package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		m.Provider, m.APIKeyEnv, m.BaseURL, m.Model = l.Provider, l.APIKeyEnv, "", ""
	}
	if l.Model != "" {
		m.Model = l.Model
	}
	if l.APIKeyEnv != "" {
		m.APIKeyEnv = l.APIKeyEnv
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

const leadInstructions = `You are the lead GraphJin agent. You hold the conversation with the user. A fast data agent answers questions about the user's databases, files and APIs through the ask_graphjin function; you never see schemas or rows except through its answers.
Work out what the user needs, then delegate. The data agent sees no conversation, so every question must stand alone: resolve "them", "that customer", "last month" and similar references into explicit names, ids and dates from the conversation before asking.
Send independent questions together in one ask_graphjin call; they run at the same time. Make a later call only when a question needs an earlier answer.
Ask for computed results — counts, totals, averages, rankings, comparisons — as questions; the data agent computes them in the database. Never add up, average or rank rows yourself.
Answer only from what the data agent returned. Never cite a field, number or name it did not return; when an answer is missing or failed, say what is missing.
The user's saved skills describe how they want recurring work done: when one matches the request, load it and follow it. Saved memories are the user's preferences and facts; follow them for format, units and conventions, but they are never data.
Changes — creating watches or tasks, writing data — go through ask_graphjin as an explicit instruction, and only after the user asked for or confirmed that change. When the user states a durable preference or fact to keep ("remember…", "from now on…"), save it with remember. When read_only is true, never request a change and never save a memory.
If the request is ambiguous in a way the data cannot settle, ask the user one short question with status needs_clarification.
Reply concisely in GitHub-flavored markdown; use a table for multi-row results.`

var leadSignature = `"` + strings.ReplaceAll(leadInstructions, `"`, `'`) + `"
instruction:string "The user's message.",
history?:json "Prior turns of this conversation [{role, content, status?}], most recent last.",
saved_memories?:json "The user's saved preferences and facts [{id, content}]. Follow them in the reply for format, units and conventions; they are not data.",
saved_skills?:json "The user's saved skills [{name, description}]. When one matches the request, load it with discover({ skills: [name] }) and follow it.",
profile?:string "This agent's standing brief from the user: focus, scope and voice. It grants no access and is not data.",
attached?:json "Data attached to this turn, such as the watch event that started it.",
current_date:string "Today's UTC date, YYYY-MM-DD.",
read_only?:boolean "True when this turn may not change anything."
-> status:class "answered, needs_clarification, blocked", answer:string "The reply to the user, grounded only in what ask_graphjin returned."`

const leadRuntimeInstructions = `Delegate with: const results = await ask_graphjin({ questions: ["...", "..."] }); it returns one {question, status, answer, data, errors} per question, in order. Log what you need to read with console.log. Never compute aggregates in code; ask the data agent instead.`

const leadFinalizeSignature = `"The lead ran out of steps before replying. Reply to the user's message using only the delegated results below. Report what they contain in plain language; when something needed is missing or failed, say so."
instruction:string "The user's message.",
evidence:string "The delegated questions and what each returned."
-> answer:string "The reply, grounded only in the evidence."`

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

	run := &leadRun{lead: l, cfg: lc, observer: req.Observer, readOnly: l.config.ReadOnly, answered: map[string]leadDelegation{}, remembered: map[string]MemoryDoc{}}
	run.grounding.Add(req.Instruction, req.History, req.Memories, req.Context)
	if req.Profile != nil {
		run.grounding.Add(req.Profile.Instructions)
	}
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
	if l.Store != nil && !l.config.ReadOnly {
		tools = append(tools, run.rememberTool(ctx))
	}
	gojaRuntime := axgoja.NewRuntime()
	runtime := figureRecordingRuntime{CodeRuntime: gojaRuntime, record: run.figures.recordCodeResult}
	for _, tool := range tools {
		t := tool
		gojaRuntime.RegisterCallable(t.Name, func(params ax.Value) (ax.Value, error) {
			return t.Handler(asMap(params))
		})
	}
	usedSkills := &skillUsageCollector{}
	usedMemories := &memoryUsageCollector{}
	options := map[string]ax.Value{
		"functions":         toolArray(tools),
		"functionDiscovery": false,
		"onUsedSkills":      ax.AxAgentObserverFn(usedSkills.observe),
		"onUsedMemories":    ax.AxAgentObserverFn(usedMemories.observe),
		"runtime":           map[string]ax.Value{"language": "JavaScript", "usageInstructions": leadRuntimeInstructions},
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
	}
	program := ax.NewAgent(leadSignature, options)
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
	if req.Profile != nil && strings.TrimSpace(req.Profile.Instructions) != "" {
		input["profile"] = strings.TrimSpace(req.Profile.Name + ": " + req.Profile.Instructions)
	}

	out, ferr := program.Forward(ctx, client, input, forwardOptions)
	usage := usageSummary(program)
	resp := Response{Status: StatusAnswered}
	if ferr != nil {
		if len(run.results()) == 0 {
			resp = Response{Status: StatusError, Errors: []ErrorInfo{{Message: ferr.Error()}}}
		} else {
			// Out of steps with answers in hand is a finalization failure, not
			// a knowledge one: spend one tool-less call replying from them.
			answer, extra, err := l.finalizeFrom(ctx, client, modelCfg, rateLimiter, leadFinalizeSignature, map[string]ax.Value{
				"instruction": req.Instruction,
				"evidence":    run.evidenceText(),
			})
			usage = mergedUsage(usage, extra)
			if err != nil {
				resp = Response{Status: StatusError, Errors: []ErrorInfo{{Message: ferr.Error()}}}
			} else {
				resp.Answer = answer
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
	// loadedSkills and loadedMemories are what the lead was given this turn;
	// they shape the answer whether or not the model reports using them.
	loadedSkills   []SkillUsage
	loadedMemories []MemoryUsage
	actionIndex    int
	delegations    []leadDelegation
	grounding      GroundingCorpus
	figures        figureLedger
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
	tool.Description = "Ask the GraphJin data agent one or more self-contained questions. Questions in one call run in parallel. Returns one result per question, in order: {question, status, answer, data, errors}."
	tool.Args = map[string]ax.Field{
		"questions": {
			Name:        "questions",
			Type:        ax.FieldType{Name: "string", IsArray: true},
			Description: "Self-contained questions or instructions for the data agent; each must make sense with no conversation.",
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
			results[i] = r.runOne(ctx, first+i, question)
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

func (r *leadRun) runOne(ctx context.Context, index int, question string) leadDelegation {
	start := r.emit(ActionEvent{Source: "lead", Tool: toolAskGraphJin, Status: "running", Delegate: index, Question: question})
	resp, err := r.lead.delegate(ctx, DelegateRequest{
		Index:    index,
		Question: question,
		ReadOnly: r.readOnly,
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
