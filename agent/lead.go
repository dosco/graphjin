package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

// The lead holds the conversation; the worker (Agent) answers one data
// question at a time. The lead plans, resolves follow-ups against the thread,
// and sends self-contained questions to workers, several at once. The worker
// stays stateless and small: everything conversational — history, memories,
// an agent profile — reaches the lead only.

const (
	defaultLeadMaxSteps       = 6
	defaultLeadMaxParallel    = 4
	defaultLeadMaxQuestions   = 8
	defaultLeadTimeoutSeconds = 120
	// leadResultDataBytes bounds each delegated result the lead model reads;
	// the full result still lands in the response and the grounding corpus.
	leadResultDataBytes = 8 << 10

	toolAskGraphJin = "ask_graphjin"
	toolRemember    = "remember"

	// noticeStatedChangeNotHeld marks an unattended turn whose reply proposed
	// a change the data agent did not hold.
	noticeStatedChangeNotHeld = "stated_change_not_held"

	// leadSeedMemories is how many recalled memories start every turn, so
	// standing preferences apply without the model having to ask for them.
	leadSeedMemories = 5
	leadRecallLimit  = 5
)

// SkillDoc is one of the caller's own skills: a named, reusable instruction
// the lead loads when a request matches its description.
type SkillDoc struct {
	ID          string
	Name        string
	Description string
	Content     string
}

// MemoryDoc is one saved preference or fact of the caller's.
type MemoryDoc struct {
	ID      string
	Content string
}

// MemoryUsage names a memory that shaped a turn.
type MemoryUsage struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// LeadStore gives the lead the caller's skills and memories. The service
// implements it over the graph, always as the caller, so every read and write
// goes through the same identity, tiers and policy as any GraphQL client.
type LeadStore interface {
	// Skills lists the skills the caller can see, with their content.
	Skills(ctx context.Context) ([]SkillDoc, error)
	// RecallMemories ranks the caller's visible memories against query; an
	// empty query returns the caller's standing memories, most recent first.
	RecallMemories(ctx context.Context, query string, limit int) ([]MemoryDoc, error)
	// Remember saves a memory for the caller.
	Remember(ctx context.Context, content string) (MemoryDoc, error)
}

// LeadConfig selects the lead's model and bounds its fan-out. Unset model
// fields fall back to the worker's, so a deployment with no lead section runs
// the lead on the worker's model.
type LeadConfig struct {
	// Enabled makes the lead the agent: every agent request runs it, and the
	// worker is reachable only through its questions. Conversations, memories,
	// custom skills and profiles are lead features and need it on.
	Enabled   bool   `mapstructure:"enabled" json:"enabled,omitempty" jsonschema:"title=Enable Lead Agent,description=Parsed dev and agentic configs default to agent.enabled"`
	Provider  string `mapstructure:"provider" json:"provider,omitempty" jsonschema:"title=Lead Provider,description=Defaults to agent.provider"`
	Model     string `mapstructure:"model" json:"model,omitempty" jsonschema:"title=Lead Model,description=Defaults to agent.model"`
	APIKeyEnv string `mapstructure:"api_key_env" json:"api_key_env,omitempty" jsonschema:"title=Lead API Key Environment Variable,description=Required when the lead provider differs from agent.provider"`
	BaseURL   string `mapstructure:"base_url" json:"base_url,omitempty" jsonschema:"title=Lead Provider Base URL"`
	Reasoning string `mapstructure:"reasoning" json:"reasoning,omitempty" jsonschema:"title=Lead Reasoning Effort,description=none low medium high xhigh; empty inherits agent.reasoning"`
	// MaxSteps bounds the lead's model calls in one turn.
	MaxSteps int `mapstructure:"max_steps" json:"max_steps,omitempty" jsonschema:"title=Lead Max Steps,default=6"`
	// MaxParallel bounds how many delegated questions run at once.
	MaxParallel int `mapstructure:"max_parallel" json:"max_parallel,omitempty" jsonschema:"title=Lead Max Parallel Questions,default=4"`
	// MaxQuestions bounds delegated questions in one turn.
	MaxQuestions   int `mapstructure:"max_questions" json:"max_questions,omitempty" jsonschema:"title=Lead Max Questions Per Turn,default=8"`
	TimeoutSeconds int `mapstructure:"timeout_seconds" json:"timeout_seconds,omitempty" jsonschema:"title=Lead Timeout Seconds,default=120"`
}

func (c LeadConfig) withDefaults() LeadConfig {
	if c.MaxSteps <= 0 {
		c.MaxSteps = defaultLeadMaxSteps
	}
	if c.MaxParallel <= 0 {
		c.MaxParallel = defaultLeadMaxParallel
	}
	if c.MaxQuestions <= 0 {
		c.MaxQuestions = defaultLeadMaxQuestions
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = defaultLeadTimeoutSeconds
	}
	return c
}

// ValidateLead checks the lead section against the worker it falls back to.
func (c Config) ValidateLead() error {
	l := c.Lead
	if l.MaxSteps < 0 || l.MaxParallel < 0 || l.MaxQuestions < 0 || l.TimeoutSeconds < 0 {
		return errors.New("agent.lead limits must not be negative")
	}
	if l.MaxParallel > 16 {
		return errors.New("agent.lead.max_parallel must be at most 16")
	}
	// A different provider must name its own key: inheriting the worker's
	// would send one provider's key to another.
	if l.Provider != "" && !strings.EqualFold(l.Provider, c.withDefaults().Provider) && l.APIKeyEnv == "" {
		return fmt.Errorf("agent.lead.api_key_env is required when agent.lead.provider (%s) differs from agent.provider", l.Provider)
	}
	return nil
}

// LeadModelConfig is the client config the lead's model is built from: the
// worker's, with the lead's own model selection on top.
func (c Config) LeadModelConfig() Config {
	m := c.withDefaults()
	l := c.Lead
	if l.Provider != "" && !strings.EqualFold(l.Provider, m.Provider) {
		// The worker's key file belongs to the worker's provider too.
		m.Provider, m.APIKeyEnv, m.APIKeyFile, m.BaseURL, m.Model = l.Provider, l.APIKeyEnv, "", "", ""
	}
	if l.Model != "" {
		m.Model = l.Model
	}
	if l.APIKeyEnv != "" {
		m.APIKeyEnv, m.APIKeyFile = l.APIKeyEnv, ""
	}
	if l.BaseURL != "" {
		m.BaseURL = l.BaseURL
	}
	if l.Reasoning != "" {
		m.Reasoning = l.Reasoning
	}
	return m
}

// DelegateRequest is one self-contained question the lead sends to a worker.
// It deliberately carries no history, memories or profile.
type DelegateRequest struct {
	Index    int
	Question string
	ReadOnly bool
	Observer func(ActionEvent)
}

// DelegateFunc runs one worker question as the lead's caller.
type DelegateFunc func(context.Context, DelegateRequest) (Response, error)

// Lead is the conversational agent over GraphJin workers.
type Lead struct {
	// Store supplies the caller's skills and memories; nil runs without them.
	Store LeadStore
	// FollowUps schedules check-backs in the conversation; nil offers no
	// follow_up tool.
	FollowUps FollowUpScheduler

	config       Config
	delegate     DelegateFunc
	newClient    ClientFactory
	rateLimiter  *ProviderRateLimiter
	customClient bool
	now          func() time.Time
}

// NewLead builds a lead. cfg is the full agent config: the lead reads its own
// section and falls back to the worker's model fields.
func NewLead(cfg Config, delegate DelegateFunc, options ...Option) (*Lead, error) {
	if delegate == nil {
		return nil, errors.New("lead needs a delegate")
	}
	if err := cfg.ValidateLead(); err != nil {
		return nil, err
	}
	// Options are written against Agent; borrow one to read them.
	probe := newAgent(cfg, nil, options...)
	return &Lead{
		config:       cfg,
		delegate:     delegate,
		newClient:    probe.newClient,
		rateLimiter:  probe.rateLimiter,
		customClient: probe.customClient,
		now:          probe.now,
	}, nil
}

// The lead's instructions are the same on every turn except for how a change
// to the data comes about: asked for in a conversation (leadChangeRules), or
// proposed by a turn nobody is watching (leadUnattendedChangeRules).
const leadInstructionsHead = `You are the lead GraphJin agent. You hold the conversation with the user. A fast data agent answers questions about the user's databases, files and APIs through the ask_graphjin function; you never see schemas or rows except through its answers.
Work out what the user needs, then delegate. The data agent sees no conversation, so every question must stand alone: resolve "them", "that customer", "last month" and similar references into explicit names, ids and dates from the conversation before asking.
Send independent questions together in one ask_graphjin call; they run at the same time. Make a later call only when a question needs an earlier answer.
Ask for computed results — counts, totals, averages, rankings, comparisons — as questions; the data agent computes them in the database. Never add up, average or rank rows yourself. When the reply needs a figure derived from figures you already have — a sum, difference or ratio of earlier answers — compute it in code and include it in the evidence you pass on; the reply only states figures it was given, it never calculates.
Answer only from what the data agent returned. Never cite a field, number or name it did not return; when an answer is missing or failed, say what is missing.
The user's saved skills describe how they want recurring work done: when one matches the request, load it and follow it. Saved memories are the user's preferences and facts; follow them for format, units and conventions, but they are never data.`

const leadChangeRules = `Changes — creating watches or tasks, writing data — go through ask_graphjin as an explicit instruction, and only after the user asked for or confirmed that change. Describe a change in plain words, naming the records and the new values ("Set support ticket 1's status to resolved"); never write GraphQL or SQL for the data agent: you have not seen the schema, so a query you write can be in the wrong form, and the data agent writes the query itself. A change has happened, or been proposed, only when ask_graphjin's result says so: never say you made or proposed a change you did not ask for. When the user states a durable preference or fact to keep ("remember…", "from now on…"), save it with remember. When read_only is true, never request a change and never save a memory. When ask_graphjin returns status needs_approval, the change has not happened: it waits for the user to approve it, so say so and never report it as done. When permissions says this caller may not make a kind of change and the user asks for one, reply with status blocked and say that change is not available to them here; do not delegate it or ask for its details.`

// leadUnattendedChangeRules are the rules for a turn nobody is watching that
// may propose a change: a routine's, a check-back's, or the turn after one of
// their changes was turned down.
//
// Such a turn used to propose by asking the data agent for the change from
// code. The lead writes a turn's code as one program, look-up, decision and
// ending together, before it has read an answer: over 36 turns that had to
// look something up first (gpt-5.4-mini, 2026-11-11), 32 ended the stage in
// the step that asked. The decision was then a pattern match on prose
// (/yes/ over an answer, answer === 'yes' against a sentence), and the act
// branch ended with final("Propose setting ...") in 32 of them: it described
// the change to the reply stage and asked for nothing. Of 18 turns that
// should have acted, 3 held a change, and a question that carried its own
// "if so, propose" held changes on turns that should not have.
//
// The reply stage reads prose and is where the model already puts the
// proposal, so that is where it is decided and stated: the reply's fields
// say whether a change is called for and what it is, and GraphJin sends it
// to the data agent (proposeStated). The reply decides from the answers
// themselves, never from what the code made of them (handDecision).
//
// These rules are for the code as much as for the reply, and reach it through
// leadCodeInstructions. While they reached only the reply, a routine's code
// was told nothing about looking anything up: it passed the instruction on as
// the reply's task and asked nothing in 17 of 36 turns, and the reply answered
// "no change is needed" about facts nobody had looked up.
const leadUnattendedChangeRules = `Nobody is watching this turn: a watch, a schedule or a decision on an earlier change started it, and its owner reads the reply later. Only code can ask the data agent. So the code looks up, with ask_graphjin, every fact the instruction's decision depends on that the attached rows do not already show; a fact the code did not look up is not known to anyone. The code does not decide and does not ask for the change: it cannot read an answer, and matching words in one decides wrongly. It ends with final() once the answers are in; GraphJin hands the reply every answer, every skill the code loaded and every check-back it scheduled, and nothing the code passes to final(). The reply decides, reading those answers. When the instruction calls for a change and its condition is met, set proposes_change to true and write the change in change_request, in plain words naming the records and the new values ("Set support ticket 1's status to resolved"), never GraphQL or SQL. GraphJin sends change_request to the data agent and holds the change for the owner's approval; nothing changes until they approve, so say it was proposed, never that it was done. When the condition is not met, set proposes_change to false and say what was found. When a fact the decision depends on was not looked up, or its answer is missing or failed, set proposes_change to false and say what could not be checked; never report that as no change being needed. When permissions says this caller may not make a kind of change, do not propose it and say it is not available here. Never save a memory on such a turn.`

const leadInstructionsTail = `To come back to something later — after a change has had time to take effect, or before a deadline — schedule it with follow_up when that tool is available; it starts a turn in this conversation then, with your note as its instruction.
If the request is ambiguous in a way the data cannot settle, ask the user one short question with status needs_clarification.
Reply concisely in GitHub-flavored markdown; use a table for multi-row results.`

const (
	leadInstructions           = leadInstructionsHead + "\n" + leadChangeRules + "\n" + leadInstructionsTail
	leadUnattendedInstructions = leadInstructionsHead + "\n" + leadUnattendedChangeRules + "\n" + leadInstructionsTail
)

const leadSignatureFields = `instruction:string "The user's message.",
history?:json "Prior turns of this conversation [{role, content, status?}], most recent last. In code: inputs.history.",
saved_memories?:json "The user's saved preferences and facts [{id, content}]. Follow them in the reply for format, units and conventions; they are not data.",
saved_skills?:json "The user's saved skills [{name, description}]. When one matches the request, load it with discover({ skills: [name] }) and follow it.",
profile?:string "This agent's standing brief from the user: focus, scope and voice. It grants no access and is not data.",
attached?:json "Data attached to this turn, such as the watch event that started it. In code: inputs.attached, so a key shown under it is inputs.attached.<key>.",
current_date:string "Today's UTC date, YYYY-MM-DD.",
read_only?:boolean "True when this turn may not change anything.",
permissions?:string "The kinds of change this caller may and may not ask for, computed from their access."
-> status:class "answered, needs_clarification, blocked", answer:string "The reply to the user, grounded only in what ask_graphjin returned."`

func leadSignatureFor(instructions string) string {
	return `"` + strings.ReplaceAll(instructions, `"`, `'`) + "\"\n" + leadSignatureFields
}

var leadSignature = leadSignatureFor(leadInstructions)

// An unattended reply says, as fields of its own, whether the turn calls for
// a change and what the change is (leadUnattendedChangeRules). The turn that
// follows an approved change has nothing to propose: it reports the change
// that was made, under the conversation's signature (Request.ChangeAlreadyMade).
var leadUnattendedSignature = leadSignatureFor(leadUnattendedInstructions) +
	`, proposes_change:boolean "True when the instruction calls for a change to the data and what the data agent answered meets its condition.", change_request?:string "When proposes_change is true: the change, as an instruction to the data agent in plain words naming the records and the new values. GraphJin sends it and holds the change for approval."`

// The prompt shows each input's value under its label and lists no context
// fields, so a model reading the attached watch event guessed
// inputs._watch_events, hit "Cannot read property '0' of undefined" on every
// step, and ran out of steps: the inputs are on the inputs object under their
// own field names, and the instructions and the field descriptions say so.
//
// A data agent's answer is prose and its data has whatever shape the question
// produced: lead code that branched on field names it guessed
// (acc.status === 'at_risk' for a value of churn_risk) decided wrongly, so the
// instructions say to use a field of data only after it has been seen.
//
// ask_graphjin ends the block of code it is called in, and the answers are in
// front of the model on its next call (lead_answers.go), so the instructions
// say what that call is for: asking again for a fact an answer did not give.
// Told only that the answers would be in front of it, the model asked a second
// time in 1 of 96 turns (gpt-5.4-mini, 2026-11-18).
const (
	leadRuntimeDelegation   = `Your input fields are on the inputs object under their field names: inputs.instruction, inputs.history, inputs.attached. Delegate with: const results = await ask_graphjin({ questions: ["...", "..."] }); it returns one {question, status, answer, data, errors} per question, in order; status is one of ` + leadResultStatuses + `.`
	leadRuntimeReading      = ` ask_graphjin ends the code block it is called in: nothing written after it runs, and what it returned is shown to you on your next turn, where results still holds it. So put every question you can already write into that one call and write nothing after it. On the next turn read each result's status and answer. When one is not answered, or its answer does not state the fact you asked for, ask for that fact again in a new call: on its own, in plainer words, naming what the other answers named (the account's id or name). Do that once for a fact. When the answers state what the request needs, finish with final(). The fields of data differ from question to question, so use a field of data in code only after you have seen it.`
	leadRuntimeFigures      = ` Never compute aggregates over rows in code; ask the data agent instead. When the reply needs a figure derived from answers already in hand (a sum, difference or ratio), compute it in code and pass the result in the evidence object of final() or respond(), next to the figures it came from.`
	leadRuntimeInstructions = leadRuntimeDelegation + leadRuntimeReading + leadRuntimeFigures
)

// leadCodeInstructions is what the stages that write the lead's code are told:
// how its runtime works, then the lead's own instructions for this kind of
// turn.
//
// Ax gives a signature's instructions to the stage that writes the reply and
// to no other. The stages that write code, and so decide what to ask the data
// agent and in what words, are given the runtime's usage instructions. The
// lead's instructions were in the signature alone: across 248 saved lead
// turns, conversations and routines (gpt-5.4-mini, 2026-11-11 and 12), none
// of the 450 model calls that wrote code carried them. The code that
// delegates had never been told that the data agent sees no conversation,
// that a question is plain words and not GraphQL, or how a change comes
// about.
//
// Measured with and without them in the code stages (same day, same model):
// the conversation corpus did not move (24 against 25 of 30 runs right, the
// same tokens), and a routine's code, told nothing else about its job, asked
// the data agent nothing in 17 of 36 turns that had to look something up.
func leadCodeInstructions(instructions string) string {
	return leadRuntimeInstructions + "\n\n" + instructions
}

// leadResultStatuses are the values of a delegated result's status, spelled
// out wherever the result is described: code that was never told them tested
// status === 'ok', which no result has, and so took every answer for failed.
const leadResultStatuses = StatusAnswered + ", " + StatusNeedsClarification + ", " + StatusNeedsApproval + ", " + StatusBlocked + ", " + StatusError

const leadFinalizeSignature = `"The lead ran out of steps before replying. Reply to the user's message using only the delegated results below. Report what they contain in plain language; when something needed is missing or failed, say so."
instruction:string "The user's message.",
evidence:string "The delegated questions and what each returned."
-> answer:string "The reply, grounded only in the evidence."`

// newLeadProgram builds the lead's Ax agent for one turn.
//
// Ax always runs a first stage before the one that does the work. On a turn
// with nothing attached that stage had only the message to read: it spent a
// model call handing the message on (122 lead calls against 88 over the same
// 30 conversation turns, median reply 13.6s against 8.4s) and asked the data
// agent no more and no less. Such a turn gets the hand-off without the call,
// as the data agent's plain questions do (installSeedHandoff).
//
// A turn with data attached keeps the stage. A routine's lead holds the watch
// event, and with the stage skipped it took that event for all there was: it
// asked the data agent nothing in 8 of 12 firings that had to look something
// up, against 0 of 12 with the stage. There the stage is what makes a lead
// look before it decides.
func newLeadProgram(signature string, options map[string]ax.Value, req Request) *ax.AxAgent {
	program := ax.NewAgent(signature, options)
	if len(req.Context) == 0 {
		program.Distiller.Options["cachingFunction"] = ax.AxCachingFunction(localHandoff)
	}
	return program
}

// Run answers one conversational turn.
func (l *Lead) Run(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Instruction) == "" {
		return Response{}, ErrMissingInstruction
	}
	lc := l.config.Lead.withDefaults()
	modelCfg := l.config.LeadModelConfig()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(lc.TimeoutSeconds)*time.Second)
	defer cancel()

	client, err := l.newClient(modelCfg)
	if err != nil {
		return Response{}, err
	}
	client = wrapServiceTierAIClient(client, modelCfg.ServiceTier)
	var rateLimiter ax.AxRateLimiter
	if l.rateLimiter != nil && l.rateLimiter.Enabled() {
		if l.customClient {
			client = l.rateLimiter.WrapAIClient(client, modelCfg.Provider, modelCfg.Model)
		} else {
			rateLimiter = l.rateLimiter.Hook(ctx)
		}
	}

	run := &leadRun{lead: l, cfg: lc, observer: req.Observer, readOnly: l.config.ReadOnly, answered: map[string]leadDelegation{}, remembered: map[string]MemoryDoc{}, followUps: map[string]FollowUp{}}
	run.grounding.Add(req.Instruction, req.History, req.Memories, req.Context)
	if req.Profile != nil {
		run.grounding.Add(req.Profile.Instructions)
	}
	run.referents = newLeadReferentCheck(req.Instruction, req.History)
	run.figures.add(FigureSourceUser, req.Instruction)
	for _, turn := range req.History {
		if strings.EqualFold(turn.Role, "assistant") {
			run.figures.add(FigureSourceConversation, turn.Content)
		} else {
			run.figures.add(FigureSourceUser, turn.Content)
		}
	}
	run.figures.add(FigureSourceUser, req.Memories...)
	run.figures.addValue(FigureSourceUser, req.Context)
	if req.Profile != nil {
		run.figures.add(FigureSourceUser, req.Profile.Instructions)
	}

	tools := []ax.Tool{run.askTool(ctx)}
	if l.Store != nil && !l.config.ReadOnly && !l.config.HeldWritesOnly {
		tools = append(tools, run.rememberTool(ctx))
	}
	if l.FollowUps != nil && !l.config.ReadOnly {
		tools = append(tools, run.followUpTool(ctx))
	}
	gojaRuntime := axgoja.NewRuntime(codeBlockPolicy(lc.TimeoutSeconds))
	returned := &unprintedCalls{}
	runtime := leadNamesRuntime{CodeRuntime: figureRecordingRuntime{CodeRuntime: gojaRuntime, record: run.figures.recordCodeResult}, base: gojaRuntime, returned: returned}
	// A turn nobody is watching that may propose a change decides it in its
	// reply, so the reply stage is handed what the data agent answered.
	proposes := l.config.HeldWritesOnly && !req.ChangeAlreadyMade
	instructions, signature := leadInstructions, leadSignature
	if proposes {
		instructions, signature = leadUnattendedInstructions, leadUnattendedSignature
		runtime.record = run.turnRecord
	}
	runtime.usage = leadCodeInstructions(instructions)
	usedSkills := &skillUsageCollector{}
	usedMemories := &memoryUsageCollector{}
	options := map[string]ax.Value{
		// Plain records, which Ax registers itself: it ends a block after an
		// answer that has to be read (lead_answers.go).
		"functions":         leadFunctionRecords(tools, returned),
		"functionDiscovery": false,
		"onUsedSkills":      ax.AxAgentObserverFn(usedSkills.observe),
		"onUsedMemories":    ax.AxAgentObserverFn(usedMemories.observe),
		"max_actor_steps":   lc.MaxSteps,
	}
	input := map[string]ax.Value{
		"instruction":  strings.TrimSpace(req.Instruction),
		"history":      historyValue(req.History),
		"current_date": l.now().UTC().Format("2006-01-02"),
		"read_only":    l.config.ReadOnly,
	}
	memories := run.memorySource(ctx, req)
	if memories.enabled() {
		options["onMemoriesSearch"] = ax.AxMemoriesSearchFn(memories.search)
	}
	if skills := run.skillSource(ctx); len(skills.docs) != 0 {
		options["skillsCatalog"] = skills.catalog()
		options["onSkillsSearch"] = ax.AxSkillsSearchFn(skills.load)
		input["saved_skills"] = skills.listing()
		if proposes {
			runtime.loadSkills = skills.load
		}
	}
	// Ax decides whether a block can be ended early from the runtime it is
	// built with, so that is the runtime itself, complete, and not a
	// {language, usageInstructions} record.
	options["runtime"] = runtime
	program := newLeadProgram(signature, options, req)
	forwardOptions := modelForwardOptions(modelCfg, rateLimiter)
	forwardOptions["runtime"] = runtime
	forwardOptions["max_actor_steps"] = lc.MaxSteps
	if len(req.Context) != 0 {
		input["attached"] = normalizeValue(req.Context)
	}
	if seed := memories.seed(req.Instruction); len(seed) != 0 {
		// A signature field, not Ax's memories input: Ax hands that only to the
		// code stages, and the reply stage must follow the user's preferences.
		input["saved_memories"] = seed
	}
	if permissions := leadPermissions(l.config.ReadOnly, req.Capabilities); permissions != "" {
		input["permissions"] = permissions
	}
	if req.Profile != nil && strings.TrimSpace(req.Profile.Instructions) != "" {
		input["profile"] = strings.TrimSpace(req.Profile.Name + ": " + req.Profile.Instructions)
	}

	forward := func(program *ax.AxAgent, input map[string]ax.Value) (ax.Value, error) {
		if req.OnAnswer != nil {
			return forwardStreamingAnswer(ctx, program, client, input, forwardOptions, req.OnAnswer)
		}
		return program.Forward(ctx, client, input, forwardOptions)
	}
	out, ferr := forward(program, input)
	usage := usageSummary(program)
	var stated *leadDelegation
	if proposes && ferr == nil {
		stated = run.proposeStated(ctx, out)
	}
	resp := Response{Status: StatusAnswered}
	if ferr != nil {
		resp = forwardErrorReply(ferr)
		if resp.Status != StatusNeedsClarification && len(run.results()) != 0 {
			// Out of steps with answers in hand is a finalization failure, not
			// a knowledge one: spend one tool-less call replying from them.
			answer, extra, err := l.finalizeFrom(ctx, client, modelCfg, rateLimiter, leadFinalizeSignature, map[string]ax.Value{
				"instruction": req.Instruction,
				"evidence":    run.evidenceText(),
			})
			usage = mergedUsage(usage, extra)
			if err == nil {
				resp = Response{Status: StatusAnswered, Answer: answer}
			}
		}
	} else {
		fields := mapValue(normalizeValue(out))
		resp.Answer = strings.TrimSpace(stringValue(fields["answer"]))
		if status := strings.TrimSpace(stringValue(fields["status"])); status != "" {
			resp.Status = status
		}
	}

	if resp.Status == StatusAnswered && resp.Answer != "" {
		if terms := run.grounding.Ungrounded(resp.Answer); len(terms) != 0 {
			rewritten, extra, err := l.finalizeFrom(ctx, client, modelCfg, rateLimiter, ungroundedRewriteSignature, map[string]ax.Value{
				"instruction":      req.Instruction,
				"evidence":         run.evidenceText(),
				"draft_answer":     resp.Answer,
				"ungrounded_terms": strings.Join(terms, ", "),
			})
			usage = mergedUsage(usage, extra)
			if err == nil && len(run.grounding.Ungrounded(rewritten)) == 0 {
				resp.Answer = rewritten
			} else {
				resp.Status = StatusBlocked
				resp.Refusal = &Refusal{
					Code:    "ungrounded_answer_fields",
					Because: []string{"the reply named identifiers no delegated question returned: " + strings.Join(terms, ", ")},
				}
			}
		}
	}
	approval := run.delegatedApproval()
	if stated != nil && approval == nil {
		// The reply was written before its change was sent, and says it was
		// proposed. It was not held, so the reply says that too, in the data
		// agent's words.
		resp.Answer = strings.TrimSpace(resp.Answer + "\n\n" + stated.notHeldSentence())
		resp.Notices = append(resp.Notices, ResponseNotice{
			Kind:    noticeStatedChangeNotHeld,
			Message: "The reply proposed a change the data agent did not hold: " + truncateString(stated.question, 280),
			Count:   1,
		})
	}
	if approval != nil && resp.Status != StatusBlocked {
		// The lead's words give the context; the engine's sentence states that
		// nothing has changed yet, so it is always part of the reply.
		if sentence := approvalAnswer(approval); !strings.Contains(resp.Answer, sentence) {
			resp.Answer = strings.TrimSpace(resp.Answer + "\n\n" + sentence)
		}
	}
	resp = settleReply(resp, replyFacts{refusal: run.delegatedRefusal, failures: run.failures(), approval: approval})
	if resp.Status == StatusAnswered {
		resp.Display = run.displayHints()
	}
	resp.Skills, resp.Memories = run.shapedBy(usedSkills.snapshot(), usedMemories.snapshot())
	if resp.Status == StatusAnswered && resp.Answer != "" {
		resp.Figures = run.figures.attribute(resp.Answer)
		if unverified := resp.UnverifiedFigures(); len(unverified) != 0 {
			resp.Notices = append(resp.Notices, ResponseNotice{
				Kind:    noticeUnverifiedFigures,
				Message: "Not returned by the data agent or computed in code this turn: " + strings.Join(unverified, ", "),
				Count:   len(unverified),
			})
		}
	}
	return run.finish(resp, usage), nil
}

// leadNamesRuntime makes an input the lead's code reads under the wrong name
// resolve to the value it meant.
//
// A value reaches lead code in one of four places: an input (inputs.attached),
// a key inside an input (inputs.attached._watch_events), a variable an earlier
// step created (ticket), or evidence the first stage handed the second
// (inputs.distilledContext.ticketId). A weak model mixes them up and writes
// inputs.ticket. Read plainly, that fails with "Cannot read property
// 'subject' of undefined". Read behind optional chaining, as in
// inputs.ticket?.subject, it fails without a word: the code went on with no
// subject, and the turn concluded that a duplicate ticket needed no change, or promised a
// check-back it never scheduled. On gpt-5.4-mini that was 5 of 50 routine
// turns, most of them wrong, and every such read was in the second stage.
//
// The second stage is where the runtime lets its host set inputs, so there
// the name resolves: before a step runs, each inputs.<name> it reads that is
// not an input, and that the runtime can place, is added to inputs with the
// value it has. In the first stage the host cannot set inputs without ending
// the stage's evidence hand-off, so a step that failed there is told where
// the value is instead.
type leadNamesRuntime struct {
	ax.CodeRuntime
	// returned is what the lead's functions returned to the step that called
	// them, shown when the step printed nothing (unprinted_calls.go).
	returned *unprintedCalls
	// record is what the turn's functions returned, for a turn whose reply
	// decides a change (handDecision). Nil on any other turn.
	record func() map[string]any
	// loadSkills loads the skills a step asked for when Ax did not: a
	// discover() in the step that ends a stage is dropped (handOff). Set on
	// the turns that have a record.
	loadSkills func([]ax.Value) []ax.Value
	// base is the goja runtime Ax's registrations of the lead's functions go
	// to, and usage the instructions for the stages that write code, which Ax
	// asks the runtime it is built with for (lead_answers.go).
	base  *axgoja.Runtime
	usage string
}

func (r leadNamesRuntime) CreateSession(globals map[string]ax.Value, options map[string]ax.Value) (ax.CodeSession, error) {
	session, err := r.CodeRuntime.CreateSession(globals, options)
	if err != nil || session == nil {
		return session, err
	}
	return &leadNamesSession{CodeSession: session, inputs: mapValue(normalizeValue(globals["inputs"])), returned: r.returned, record: r.record, loadSkills: r.loadSkills}, nil
}

// RegisterHostCallable keeps every registration Ax makes: Ax's own wrapper of
// a function is what ends a block after an answer that has to be read.
func (r leadNamesRuntime) RegisterHostCallable(name string, handler func(ax.Value) (ax.Value, error)) {
	registerAxCallable(r.base, name, handler)
}

type leadNamesSession struct {
	ax.CodeSession
	// inputs are the session's inputs: as it was created, then as the second
	// stage's patch set them.
	inputs map[string]any
	// resolved are the names this session added to inputs, read again from
	// where they live before every step that uses them.
	resolved   map[string]bool
	returned   *unprintedCalls
	record     func() map[string]any
	loadSkills func([]ax.Value) []ax.Value
}

// PatchGlobals follows the inputs Ax hands the second stage, which carry the
// first stage's evidence as inputs.distilledContext.
func (s *leadNamesSession) PatchGlobals(snapshot ax.Value, options map[string]ax.Value) ax.Value {
	if patch := mapValue(normalizeValue(snapshot)); patch["merge"] == true {
		if inputs := mapValue(mapValue(patch["bindings"])["inputs"]); inputs != nil {
			s.inputs, s.resolved = inputs, nil
		}
	}
	return s.CodeSession.PatchGlobals(snapshot, options)
}

func (s *leadNamesSession) Execute(code string, options map[string]ax.Value) ax.Value {
	if s.returned == nil {
		return s.handOff(code, s.execute(code, options), options)
	}
	s.returned.take()
	result := s.execute(code, options)
	showReturned(result, s.returned.take())
	return s.handOff(code, result, options)
}

// handOff makes the hand-off of a second-stage step that ends the stage, on a
// turn whose reply decides a change (handDecision).
//
// Before that, in either stage, it loads the skills a stage-ending step asked
// for. Ax runs a step's discover() only when the step goes on to another:
// `await discover({skills: [...]}); await final(...)`, which is how a model
// writes it, ended the stage with the skill never loaded, and a change worded
// "per the playbook" went out with a note the model made up.
func (s *leadNamesSession) handOff(code string, result ax.Value, options map[string]ax.Value) ax.Value {
	if s.record == nil {
		return result
	}
	if s.loadSkills != nil && endsStage(result) {
		if names := discoveredSkills(code); len(names) != 0 {
			s.loadSkills(names)
		}
	}
	if secondStageStep(options) {
		handDecision(result, s.record())
	}
	return result
}

func (s *leadNamesSession) execute(code string, options map[string]ax.Value) ax.Value {
	placed := s.place(code)
	if len(placed) != 0 && secondStageStep(options) && s.resolve(placed) {
		return s.CodeSession.Execute(code, options)
	}
	result := s.CodeSession.Execute(code, options)
	if payload := mapValue(result); len(placed) != 0 && payload != nil && payload["is_error"] == true {
		hints := make([]string, 0, len(placed))
		for _, name := range placed {
			hints = append(hints, name.hint())
		}
		payload["error"] = stringValue(payload["error"]) + "\n" + strings.Join(hints, " ")
	}
	return result
}

// secondStageStep reports whether a code step runs in the second (executor)
// stage: Ax reserves executorRequest in the runtime only once the first stage
// has handed over.
func secondStageStep(options map[string]ax.Value) bool {
	for _, name := range anySlice(normalizeValue(options["reservedNames"])) {
		if stringValue(name) == "executorRequest" {
			return true
		}
	}
	return false
}

// resolve adds each placed name to the session's inputs, so the step about
// to run reads the value it meant. It reports whether inputs now hold them.
func (s *leadNamesSession) resolve(placed []leadPlacedName) bool {
	next := make(map[string]any, len(s.inputs)+len(placed))
	for name, value := range s.inputs {
		next[name] = value
	}
	changed := false
	for _, name := range placed {
		if !s.resolved[name.name] || !reflect.DeepEqual(next[name.name], name.value) {
			changed = true
		}
		next[name.name] = name.value
	}
	if !changed {
		return true
	}
	// A merge patch is how the runtime's host sets inputs; it leaves the
	// session's variables alone.
	out := s.CodeSession.PatchGlobals(map[string]ax.Value{"merge": true, "bindings": map[string]ax.Value{"inputs": next}}, nil)
	if payload := mapValue(out); payload == nil || payload["is_error"] == true {
		return false
	}
	if s.resolved == nil {
		s.resolved = map[string]bool{}
	}
	for _, name := range placed {
		s.resolved[name.name] = true
	}
	s.inputs = next
	return true
}

// leadPlacedName is a name lead code read as inputs.<name> that is not an
// input, with the place its value really is.
type leadPlacedName struct {
	name  string
	where string // the expression that reads it
	what  string // what it is, for a hint
	value any
}

func (n leadPlacedName) hint() string {
	return fmt.Sprintf("inputs.%s does not exist: %s is %s, so read %s.", n.name, n.name, n.what, n.where)
}

// leadInputRead matches inputs.<name> and inputs?.<name>, the two ways lead
// code reads an input (768 and 15 of 1231 traced steps; none used brackets).
var leadInputRead = regexp.MustCompile(`\binputs\??\.([A-Za-z_$][A-Za-z0-9_$]*)`)

// leadDeclaredInputs are the inputs the lead's signature declares. One this
// turn does not carry stays absent: code that asks whether it is there must
// not be handed something else of the same name.
var leadDeclaredInputs = func() map[string]bool {
	names := map[string]bool{}
	fields, _, _ := strings.Cut(leadSignature, "->")
	for _, match := range regexp.MustCompile(`(?m)^([a-z_]+)\??:`).FindAllStringSubmatch(fields, -1) {
		names[match[1]] = true
	}
	return names
}()

// place finds each inputs.<name> the code reads that is not an input and
// says where its value is: evidence the first stage handed on, a variable an
// earlier step created, or a key inside an input, in that order. A name it
// cannot place is left out: it never guesses.
func (s *leadNamesSession) place(code string) []leadPlacedName {
	var variables map[string]any
	var placed []leadPlacedName
	seen := map[string]bool{}
	for _, match := range leadInputRead.FindAllStringSubmatch(code, -1) {
		name := match[1]
		if seen[name] || leadDeclaredInputs[name] || name == "distilledContext" || name == "executorRequest" {
			continue
		}
		seen[name] = true
		if _, isInput := s.inputs[name]; isInput && !s.resolved[name] {
			continue
		}
		if variables == nil {
			variables = mapValue(normalizeValue(s.CodeSession.Inspect(nil)))
			if variables == nil {
				variables = map[string]any{}
			}
		}
		if value, handedOn := mapValue(s.inputs["distilledContext"])[name]; handedOn && value != nil {
			placed = append(placed, leadPlacedName{name, "inputs.distilledContext." + name, "evidence the earlier stage handed on", value})
			continue
		}
		if value := variables[name]; value != nil {
			placed = append(placed, leadPlacedName{name, "it as " + name, "a variable an earlier step created", value})
			continue
		}
		inputs := make([]string, 0, len(s.inputs))
		for input := range s.inputs {
			if !s.resolved[input] {
				inputs = append(inputs, input)
			}
		}
		sort.Strings(inputs)
		for _, input := range inputs {
			if value, inside := mapValue(s.inputs[input])[name]; inside && value != nil {
				placed = append(placed, leadPlacedName{name, "inputs." + input + "." + name, "a key of the " + input + " input", value})
				break
			}
		}
	}
	sort.Slice(placed, func(i, j int) bool { return placed[i].name < placed[j].name })
	return placed
}

// leadPermissionClasses names, in the user's words, each kind of change the
// worker gates by caller access, keyed by the guide that grants it.
var leadPermissionClasses = []struct{ skill, label string }{
	{skillDataWrite, "changing application data"},
	{skillWatchWrite, "creating or changing watches"},
	{skillTaskWrite, "creating or changing tasks"},
	{skillWorkflowExec, "running workflows"},
	{skillWorkflowWrite, "creating or changing workflows"},
	{skillCodeWrite, "changing code"},
	{skillAdminRead, "inspecting GraphJin security, runtime and configuration"},
	{skillAdminWrite, "changing GraphJin configuration"},
}

// leadPermissions tells the lead what this caller may ask to change, from the
// same allowedSkills check that gates the worker's guides. Without it the lead
// asked a non-admin which role to add to the config, then the worker refused
// once the user answered. No profile means no claim either way.
func leadPermissions(readOnly bool, profile *CapabilityProfile) string {
	if profile == nil {
		return ""
	}
	granted := map[string]bool{}
	for _, skill := range allowedSkills(readOnly, profile) {
		granted[skill.id] = true
	}
	var may, mayNot []string
	for _, class := range leadPermissionClasses {
		if granted[class.skill] {
			may = append(may, class.label)
		} else {
			mayNot = append(mayNot, class.label)
		}
	}
	var out []string
	if len(may) != 0 {
		out = append(out, "May ask for: "+strings.Join(may, "; ")+".")
	}
	if len(mayNot) != 0 {
		out = append(out, "May not ask for: "+strings.Join(mayNot, "; ")+".")
	}
	return strings.Join(out, " ")
}

// delegatedRefusal is the refusal behind a lead's block: the latest delegated
// question a guard refused, with its unblock steps. The lead holds no guards
// of its own. A block the lead's run explains itself, such as a model that
// emitted no runnable code, keeps its own code instead of borrowing one.
func (r *leadRun) delegatedRefusal(resp Response) *Refusal {
	if firstErrorCode(resp.Errors) != "" {
		return nil
	}
	delegations := r.results()
	for i := len(delegations) - 1; i >= 0; i-- {
		d := delegations[i]
		if d.err == nil && d.resp.Status == StatusBlocked && d.resp.Refusal != nil {
			refusal := *d.resp.Refusal
			return &refusal
		}
	}
	return nil
}

// delegatedApproval is the latest write a delegated question held for the
// user's approval. A reply carries one; any other held write stays pending in
// the approvals list.
func (r *leadRun) delegatedApproval() *Approval {
	delegations := r.results()
	for i := len(delegations) - 1; i >= 0; i-- {
		if d := delegations[i]; d.err == nil && d.resp.Approval != nil {
			return d.resp.Approval
		}
	}
	return nil
}

// displayHints carries the data agent's layout of each answered question, in
// the order asked, labeled with the question.
func (r *leadRun) displayHints() []DisplayHint {
	var out []DisplayHint
	for _, d := range r.results() {
		if d.err != nil || d.resp.Status != StatusAnswered {
			continue
		}
		for _, hint := range d.resp.Display {
			hint.Question = d.question
			out = append(out, hint)
		}
	}
	return out
}

// failures are the delegated questions that failed. The lead's status classes
// have no error, so it says "blocked" after one; the reply contract reports
// that as the error it was.
func (r *leadRun) failures() []ErrorInfo {
	var out []ErrorInfo
	for _, d := range r.results() {
		switch {
		case d.err != nil:
			out = append(out, ErrorInfo{Message: d.err.Error()})
		case d.resp.Status == StatusError:
			out = append(out, d.resp.Errors...)
			if len(d.resp.Errors) == 0 {
				out = append(out, ErrorInfo{Message: "the data agent failed on: " + d.question})
			}
		}
	}
	return out
}

// finalizeFrom makes one tool-less call and returns its answer field.
func (l *Lead) finalizeFrom(ctx context.Context, client ax.AIClient, cfg Config, rateLimiter ax.AxRateLimiter, signature string, input map[string]ax.Value) (string, any, error) {
	gen := ax.NewAx(signature, nil)
	out, err := gen.Forward(ctx, client, input, modelForwardOptions(cfg, rateLimiter))
	usage := usageSummary(finalizeProgram{gen: gen})
	if err != nil {
		return "", usage, err
	}
	answer := strings.TrimSpace(stringValue(mapValue(normalizeValue(out))["answer"]))
	if answer == "" {
		return "", usage, errors.New("finalize returned no answer")
	}
	return answer, usage, nil
}

// leadMemorySource answers the lead's memory recall: from the store when the
// service has one, else from the memories the request carried.
type leadMemorySource struct {
	ctx    context.Context
	run    *leadRun
	store  LeadStore
	static []MemoryDoc
}

func (r *leadRun) memorySource(ctx context.Context, req Request) leadMemorySource {
	src := leadMemorySource{ctx: ctx, run: r, store: r.lead.Store}
	for i, memory := range req.Memories {
		if memory = strings.TrimSpace(memory); memory != "" {
			src.static = append(src.static, MemoryDoc{ID: fmt.Sprintf("memory-%d", i+1), Content: memory})
		}
	}
	return src
}

func (m leadMemorySource) enabled() bool { return m.store != nil || len(m.static) != 0 }

func (m leadMemorySource) recall(query string, limit int) []MemoryDoc {
	if m.store == nil {
		if len(m.static) > limit {
			return m.static[:limit]
		}
		return m.static
	}
	docs, err := m.store.RecallMemories(m.ctx, query, limit)
	if err != nil {
		return nil
	}
	return docs
}

// seed is the turn's starting memories: those relevant to the message, then
// the caller's standing ones (an empty query) while room remains, so a
// preference applies even when the message shares no words with it.
func (m leadMemorySource) seed(instruction string) []ax.Value {
	if !m.enabled() {
		return nil
	}
	docs := m.recall(instruction, leadSeedMemories)
	if len(docs) < leadSeedMemories {
		seen := map[string]bool{}
		for _, doc := range docs {
			seen[doc.ID] = true
		}
		for _, doc := range m.recall("", leadSeedMemories) {
			if len(docs) == leadSeedMemories {
				break
			}
			if !seen[doc.ID] {
				seen[doc.ID] = true
				docs = append(docs, doc)
			}
		}
	}
	return m.values(docs, nil)
}

// search runs one recall per search string, leaving out what is loaded.
func (m leadMemorySource) search(searches []ax.Value, alreadyLoaded []ax.Value) []ax.Value {
	loaded := map[string]bool{}
	for _, item := range alreadyLoaded {
		switch v := normalizeValue(item).(type) {
		case string:
			loaded[v] = true
		case map[string]any:
			loaded[stringValue(v["id"])] = true
		}
	}
	var out []ax.Value
	for _, search := range searches {
		query := strings.TrimSpace(stringValue(normalizeValue(search)))
		if query == "" {
			continue
		}
		out = append(out, m.values(m.recall(query, leadRecallLimit), loaded)...)
	}
	return out
}

func (m leadMemorySource) values(docs []MemoryDoc, skip map[string]bool) []ax.Value {
	out := make([]ax.Value, 0, len(docs))
	for _, doc := range docs {
		if doc.ID == "" || skip[doc.ID] {
			continue
		}
		if skip != nil {
			skip[doc.ID] = true
		}
		m.run.mu.Lock()
		m.run.grounding.Add(doc.Content)
		m.run.figures.add(FigureSourceUser, doc.Content)
		m.run.loadedMemories = append(m.run.loadedMemories, MemoryUsage{ID: doc.ID, Reason: "recalled"})
		m.run.mu.Unlock()
		out = append(out, ax.Object("id", doc.ID, "content", doc.Content))
	}
	return out
}

// leadSkillSource is the turn's skill catalog. The model sees names and
// descriptions; load hands back a skill's content only for a catalog entry,
// matched by id or name, so nothing outside the caller's own skills loads.
type leadSkillSource struct {
	run  *leadRun
	docs []SkillDoc
}

// keepSkillText records a loaded skill as the code stages read it, once. The
// caller holds r.mu.
func (r *leadRun) keepSkillText(doc SkillDoc) {
	for _, kept := range r.loadedSkillText {
		if mapValue(kept)["name"] == doc.Name {
			return
		}
	}
	r.loadedSkillText = append(r.loadedSkillText, map[string]any{"name": doc.Name, "content": doc.Content})
}

func (r *leadRun) skillSource(ctx context.Context) leadSkillSource {
	src := leadSkillSource{run: r}
	if r.lead.Store == nil {
		return src
	}
	docs, err := r.lead.Store.Skills(ctx)
	if err == nil {
		src.docs = docs
	}
	return src
}

func (s leadSkillSource) catalog() []ax.Value {
	values := make([]ax.Value, 0, len(s.docs))
	for _, doc := range s.docs {
		values = append(values, ax.Object("id", doc.ID, "name", doc.Name, "description", doc.Description))
	}
	return values
}

// listing is what the model sees before loading anything.
func (s leadSkillSource) listing() []ax.Value {
	values := make([]ax.Value, 0, len(s.docs))
	for _, doc := range s.docs {
		values = append(values, ax.Object("name", doc.Name, "description", doc.Description))
	}
	return values
}

func (s leadSkillSource) load(requested []ax.Value) []ax.Value {
	var out []ax.Value
	seen := map[string]bool{}
	for _, item := range requested {
		key := strings.ToLower(strings.TrimSpace(stringValue(normalizeValue(item))))
		for _, doc := range s.docs {
			if seen[doc.ID] || (key != strings.ToLower(doc.ID) && key != strings.ToLower(doc.Name)) {
				continue
			}
			seen[doc.ID] = true
			s.run.mu.Lock()
			s.run.grounding.Add(doc.Content)
			s.run.figures.add(FigureSourceUser, doc.Content)
			s.run.loadedSkills = append(s.run.loadedSkills, SkillUsage{ID: doc.ID, Name: doc.Name, Reason: "loaded"})
			s.run.keepSkillText(doc)
			s.run.mu.Unlock()
			out = append(out, ax.Object("id", doc.ID, "name", doc.Name, "content", doc.Content))
		}
	}
	return out
}

// shapedBy lists the skills and memories behind the answer: those the model
// reported using, then any others it loaded, each once.
func (r *leadRun) shapedBy(usedSkills []SkillUsage, usedMemories []MemoryUsage) ([]SkillUsage, []MemoryUsage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	skills := append([]SkillUsage(nil), usedSkills...)
	seen := map[string]bool{}
	for _, s := range skills {
		seen[s.ID] = true
	}
	for _, s := range r.loadedSkills {
		if !seen[s.ID] {
			seen[s.ID] = true
			skills = append(skills, s)
		}
	}
	memories := append([]MemoryUsage(nil), usedMemories...)
	seenMem := map[string]bool{}
	for _, m := range memories {
		seenMem[m.ID] = true
	}
	for _, m := range r.loadedMemories {
		if !seenMem[m.ID] {
			seenMem[m.ID] = true
			memories = append(memories, m)
		}
	}
	return skills, memories
}

type memoryUsageCollector struct {
	mu    sync.Mutex
	items []MemoryUsage
}

func (c *memoryUsageCollector) observe(items []ax.Value) {
	var out []MemoryUsage
	for _, item := range items {
		switch v := normalizeValue(item).(type) {
		case string:
			out = append(out, MemoryUsage{ID: v})
		case map[string]any:
			if id := stringValue(v["id"]); id != "" {
				out = append(out, MemoryUsage{ID: id, Reason: stringValue(v["reason"])})
			}
		}
	}
	c.mu.Lock()
	c.items = out
	c.mu.Unlock()
}

func (c *memoryUsageCollector) snapshot() []MemoryUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]MemoryUsage(nil), c.items...)
}

// leadRun is one turn's delegation state. ask runs from Ax's tool loop while
// its questions run on their own goroutines, so everything shared is locked.
type leadRun struct {
	lead     *Lead
	cfg      LeadConfig
	observer func(ActionEvent)
	readOnly bool

	mu         sync.Mutex
	asked      int
	answered   map[string]leadDelegation
	remembered map[string]MemoryDoc
	followUps  map[string]FollowUp
	// loadedSkills and loadedMemories are what the lead was given this turn;
	// they shape the answer whether or not the model reports using them.
	loadedSkills   []SkillUsage
	loadedMemories []MemoryUsage
	// loadedSkillText is each loaded skill as the code stages read it. The
	// reply stage is not given loaded skills by Ax, so a turn whose reply
	// decides a change hands it these (turnRecord).
	loadedSkillText []any
	actionIndex     int
	delegations     []leadDelegation
	grounding       GroundingCorpus
	figures         figureLedger
	// referents holds back, once per turn, a follow-up question that takes
	// nothing from the conversation the user points at (lead_referents.go).
	referents      *leadReferentCheck
	referentsFired bool
}

type leadDelegation struct {
	index    int
	question string
	resp     Response
	err      error
}

func (r *leadRun) askTool(ctx context.Context) ax.Tool {
	tool := ax.Fn(toolAskGraphJin).WithHandler(func(args map[string]ax.Value) (ax.Value, error) {
		return r.ask(ctx, args)
	})
	tool.Description = "Ask the GraphJin data agent one or more self-contained questions. Questions in one call run in parallel. Returns one result per question, in order: {question, status, answer, data, errors}. status is one of " + leadResultStatuses + "."
	tool.Args = map[string]ax.Field{
		"questions": {
			Name:        "questions",
			Type:        ax.FieldType{Name: "string", IsArray: true},
			Description: "Self-contained questions or instructions for the data agent, in plain words — never GraphQL or SQL; each must make sense with no conversation.",
		},
	}
	return tool
}

// rememberTool saves one of the user's preferences or facts through the
// store, as the caller. It is not offered on read-only turns.
func (r *leadRun) rememberTool(ctx context.Context) ax.Tool {
	tool := ax.Fn(toolRemember).WithHandler(func(args map[string]ax.Value) (ax.Value, error) {
		content := strings.TrimSpace(stringValue(normalizeValue(args["content"])))
		if content == "" {
			return nil, errors.New("content must state the preference or fact to keep")
		}
		// Saving the same memory again this turn — the lead's code can run
		// again in a later stage — returns the first save instead of a copy.
		key := leadQuestionKey(content)
		r.mu.Lock()
		prior, saved := r.remembered[key]
		r.mu.Unlock()
		if saved {
			return map[string]any{"saved": true, "id": prior.ID, "already_saved": true}, nil
		}
		start := r.emit(ActionEvent{Source: "lead", Tool: toolRemember, Status: "running"})
		doc, err := r.lead.Store.Remember(ctx, content)
		finish := ActionEvent{Source: "lead", Tool: toolRemember, Status: StatusAnswered, ElapsedMS: time.Since(start).Milliseconds()}
		if err != nil {
			finish.Status, finish.Error = StatusError, err.Error()
			r.emit(finish)
			return nil, err
		}
		r.emit(finish)
		r.mu.Lock()
		r.grounding.Add(content)
		r.remembered[key] = doc
		r.mu.Unlock()
		return map[string]any{"saved": true, "id": doc.ID}, nil
	})
	tool.Description = "Save one of the user's durable preferences or facts so later conversations follow it. Use only when the user asks to remember something or states a standing preference; never save query results, secrets, credentials or one-off requests."
	tool.Args = map[string]ax.Field{
		"content": {Name: "content", Type: ax.FieldType{Name: "string"}, Description: "The preference or fact, as one short self-contained sentence."},
	}
	return tool
}

func (r *leadRun) ask(ctx context.Context, args map[string]ax.Value) (ax.Value, error) {
	var questions []string
	for _, q := range anySlice(normalizeValue(args["questions"])) {
		if s := strings.TrimSpace(stringValue(q)); s != "" {
			questions = append(questions, s)
		}
	}
	if len(questions) == 0 {
		return nil, errors.New("questions must list at least one question")
	}

	// A question already answered this turn is answered from that result: the
	// lead's code can run again in a later stage, and re-asking would run the
	// worker (and repeat any write) a second time.
	out := make([]any, len(questions))
	var fresh []string
	var freshAt []int
	r.mu.Lock()
	for i, question := range questions {
		if prior, ok := r.answered[leadQuestionKey(question)]; ok {
			view := prior.modelView()
			view["already_asked"] = true
			out[i] = view
			continue
		}
		fresh = append(fresh, question)
		freshAt = append(freshAt, i)
	}
	if !r.referentsFired {
		for _, question := range fresh {
			if refs, carried := r.referents.unresolved(question); len(refs) != 0 {
				r.referentsFired = true
				r.mu.Unlock()
				return nil, r.referents.refusal(question, refs, carried)
			}
		}
	}
	remaining := r.cfg.MaxQuestions - r.asked
	if len(fresh) != 0 && remaining <= 0 {
		r.mu.Unlock()
		return nil, fmt.Errorf("this turn's %d questions are used up; reply from the results you have", r.cfg.MaxQuestions)
	}
	dropped := 0
	if len(fresh) > remaining {
		dropped = len(fresh) - remaining
		fresh, freshAt = fresh[:remaining], freshAt[:remaining]
	}
	first := r.asked + 1
	r.asked += len(fresh)
	r.mu.Unlock()

	results := make([]leadDelegation, len(fresh))
	sem := make(chan struct{}, r.cfg.MaxParallel)
	var wg sync.WaitGroup
	for i, question := range fresh {
		wg.Add(1)
		go func(i int, question string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = leadDelegation{index: first + i, question: question, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			results[i] = r.runOne(ctx, first+i, question, r.codeOnlyReads())
		}(i, question)
	}
	wg.Wait()

	r.mu.Lock()
	r.delegations = append(r.delegations, results...)
	for i, d := range results {
		if d.err == nil {
			r.answered[leadQuestionKey(d.question)] = d
		}
		out[freshAt[i]] = d.modelView()
	}
	r.mu.Unlock()

	kept := out[:0]
	for _, view := range out {
		if view != nil {
			kept = append(kept, view)
		}
	}
	if dropped != 0 {
		kept = append(kept, map[string]any{"note": fmt.Sprintf("%d questions were not asked: this turn allows %d", dropped, r.cfg.MaxQuestions)})
	}
	return kept, nil
}

// leadQuestionKey matches a question asked again with different spacing or case.
func leadQuestionKey(question string) string {
	return strings.ToLower(strings.Join(strings.Fields(question), " "))
}

// codeOnlyReads says whether a question the turn's code asks now may only
// read. A reply carries one change: once a question held a write for the
// user's approval, the turn's later questions only read.
func (r *leadRun) codeOnlyReads() bool {
	return r.readOnly || r.delegatedApproval() != nil
}

// runOne asks the data agent one question.
func (r *leadRun) runOne(ctx context.Context, index int, question string, readOnly bool) leadDelegation {
	start := r.emit(ActionEvent{Source: "lead", Tool: toolAskGraphJin, Status: "running", Delegate: index, Question: question})
	resp, err := r.lead.delegate(ctx, DelegateRequest{
		Index:    index,
		Question: question,
		ReadOnly: readOnly,
		Observer: func(event ActionEvent) {
			event.Delegate = index
			r.forward(event)
		},
	})
	d := leadDelegation{index: index, question: question, resp: resp, err: err}
	r.mu.Lock()
	r.grounding.Add(question, resp.Answer, resp.Data, resp.Errors)
	r.mu.Unlock()
	r.figures.add(FigureSourceDataAgent, resp.Answer)
	finish := ActionEvent{
		Source: "lead", Tool: toolAskGraphJin, Status: d.status(), Delegate: index, Question: question,
		ElapsedMS: time.Since(start).Milliseconds(),
		Summary:   map[string]any{"status": d.status(), "answer": truncateString(resp.Answer, 280)},
	}
	if err != nil {
		finish.Error = err.Error()
	}
	r.emit(finish)
	return d
}

// emit sends a lead event and returns when it was sent.
func (r *leadRun) emit(event ActionEvent) time.Time {
	r.mu.Lock()
	r.actionIndex++
	event.Index = r.actionIndex
	r.mu.Unlock()
	r.forward(event)
	return time.Now()
}

func (r *leadRun) forward(event ActionEvent) {
	if r.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	r.observer(event)
}

func (r *leadRun) results() []leadDelegation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]leadDelegation(nil), r.delegations...)
}

func (d leadDelegation) status() string {
	switch {
	case d.err != nil:
		return StatusError
	case d.resp.Status == "":
		return StatusAnswered
	default:
		return d.resp.Status
	}
}

// modelView is what the lead model reads back for one question.
func (d leadDelegation) modelView() map[string]any {
	view := map[string]any{"question": d.question, "status": d.status()}
	if d.err != nil {
		view["errors"] = []string{d.err.Error()}
		return view
	}
	if d.resp.Answer != "" {
		view["answer"] = d.resp.Answer
	}
	if d.resp.Data != nil {
		view["data"] = boundedJSON(d.resp.Data, leadResultDataBytes)
	}
	if len(d.resp.Errors) != 0 {
		msgs := make([]string, 0, len(d.resp.Errors))
		for _, e := range d.resp.Errors {
			msgs = append(msgs, e.Message)
		}
		view["errors"] = msgs
	}
	if d.resp.Refusal != nil {
		view["refusal"] = d.resp.Refusal.Code
	}
	return view
}

// boundedJSON keeps a value as-is when it is small and otherwise hands the
// model a truncated JSON preview it can still read.
func boundedJSON(value any, limit int) any {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) <= limit {
		return value
	}
	return map[string]any{"truncated": true, "preview": string(raw[:limit])}
}

func (r *leadRun) evidenceText() string {
	var b strings.Builder
	for _, d := range r.results() {
		raw, _ := json.Marshal(d.modelView())
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// finish shapes the turn as an ordinary Response, so thread storage and the
// console handle lead and worker answers alike.
func (r *leadRun) finish(resp Response, usage any) Response {
	delegations := r.results()
	data := make([]any, 0, len(delegations))
	evidence := make([]any, 0, len(delegations))
	var actions []any
	seenNotice := map[string]bool{}
	for _, d := range delegations {
		item := map[string]any{"question": d.question, "status": d.status()}
		if d.resp.Answer != "" {
			item["answer"] = d.resp.Answer
		}
		if d.resp.Data != nil {
			item["data"] = d.resp.Data
		}
		if d.err != nil {
			item["error"] = d.err.Error()
		}
		data = append(data, item)
		evidence = append(evidence, map[string]any{
			"delegate": d.index, "question": d.question, "trace_id": d.resp.TraceID, "evidence": d.resp.Evidence,
		})
		for _, action := range anySlice(normalizeValue(d.resp.Actions)) {
			if m, ok := action.(map[string]any); ok {
				tagged := cloneAnyMap(m)
				tagged["delegate"] = d.index
				actions = append(actions, tagged)
			}
		}
		for _, notice := range d.resp.Notices {
			key := notice.Kind + "\x00" + notice.Message
			if !seenNotice[key] {
				seenNotice[key] = true
				resp.Notices = append(resp.Notices, notice)
			}
		}
		usage = mergedUsage(usage, usageTotalsMap(SummarizeUsage(d.resp.Usage)))
	}
	if len(delegations) != 0 {
		resp.Data = map[string]any{"delegations": data}
		resp.Evidence = map[string]any{"delegations": evidence}
	}
	if len(actions) != 0 {
		resp.Actions = actions
	}
	resp.Usage = usage
	return resp
}

func usageTotalsMap(t UsageTotals) map[string]any {
	return map[string]any{
		"llm_calls": t.LLMCalls, "prompt_tokens": t.PromptTokens,
		"completion_tokens": t.CompletionTokens, "total_tokens": t.TotalTokens,
	}
}
