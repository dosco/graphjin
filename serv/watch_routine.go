package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

// A routine is a watch whose delivery is an agent turn: when the watch fires,
// GraphJin asks the agent the stored prompt as the watch owner, read-only, in
// a thread, and the answer lands there like any other turn. It runs through
// the same thread run path as chat, so it gets the run lease, cancel, resume
// and the lost-run sweep.

const (
	watchRoutinePromptMaxBytes = 2048
	watchRoutineBusyWait       = 2 * time.Minute
	watchRoutineDefaultPrompt  = "This watch just fired. Explain what changed, why it matters, and what to look at next, quoting the numbers you find."
)

type watchAgentConfig struct {
	Prompt   string
	ThreadID string
	// Profile names an agent_profile: the routine answers in the owner's home
	// thread for that agent, with the agent's instructions.
	Profile string
	// Once makes the watch a one-time follow-up: it is deleted after it has
	// delivered once.
	Once bool
}

func parseWatchAgentConfig(m map[string]any) (watchAgentConfig, error) {
	src := m
	if nested := mapFromAny(m["agent"]); nested != nil {
		src = nested
	}
	cfg := watchAgentConfig{
		Prompt:   strings.TrimSpace(stringFromAny(src["prompt"])),
		ThreadID: strings.TrimSpace(stringFromAny(src["thread_id"])),
		Profile:  strings.TrimSpace(stringFromAny(src["profile"])),
		Once:     boolMapValue(src, "once"),
	}
	if cfg.Prompt == "" {
		cfg.Prompt = watchRoutineDefaultPrompt
	}
	if len(cfg.Prompt) > watchRoutinePromptMaxBytes {
		return cfg, fmt.Errorf("agent delivery prompt is longer than %d bytes", watchRoutinePromptMaxBytes)
	}
	return cfg, nil
}

// unattendedMinSteps is the step budget floor for runs nobody watches.
const unattendedMinSteps = 16

// queryCatalogTableIDs names the catalog details of the tables query reads,
// compiled as ctx's role, for a run to start with as evidence. Empty when the
// query does not compile or names nothing the caller's catalog has.
func (s *graphjinService) queryCatalogTableIDs(ctx context.Context, query string) []string {
	return s.catalogTableIDs(ctx, s.queryTables(ctx, query))
}

// queryTables is the {database, schema, table} of each table query reads.
func (s *graphjinService) queryTables(ctx context.Context, query string) [][3]string {
	if s.gj == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	exp, err := s.gj.ExplainQuery(query, nil, s.gj.RequestRole(ctx))
	if err != nil || exp == nil {
		return nil
	}
	tables := make([][3]string, 0, len(exp.Tables))
	for _, t := range exp.Tables {
		tables = append(tables, [3]string{t.Database, t.Schema, t.Table})
	}
	return tables
}

// catalogTableIDs maps tables to the ids of the table cards ctx's caller can
// see in the catalog.
func (s *graphjinService) catalogTableIDs(ctx context.Context, tables [][3]string) []string {
	if len(tables) == 0 {
		return nil
	}
	snap, err := s.catalogSnapshotForContext(ctx)
	if err != nil || snap == nil {
		return nil
	}
	return tableCardIDs(snap.Cards, tables)
}

// tableCardIDs picks the table card for each {database, schema, table}; an
// empty database or schema matches any.
func tableCardIDs(cards []core.CatalogCard, tables [][3]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, want := range tables {
		for _, card := range cards {
			if card.Kind != "table" || !strings.EqualFold(card.TableName, want[2]) ||
				(want[0] != "" && !strings.EqualFold(card.DatabaseName, want[0])) ||
				(want[1] != "" && !strings.EqualFold(card.SchemaName, want[1])) {
				continue
			}
			if !seen[card.ID] {
				seen[card.ID] = true
				out = append(out, card.ID)
			}
			break
		}
	}
	return out
}

// foldProfileIntoTask carries a saved agent's standing instructions into a
// routine's task. The data agent drops the conversation-level profile, so a
// routine that runs on it gets the instructions as part of what it is asked
// to do.
func foldProfileIntoTask(req *gjagent.Request) {
	if req.Profile == nil || strings.TrimSpace(req.Profile.Instructions) == "" {
		return
	}
	name := strings.TrimSpace(req.Profile.Name)
	if name == "" {
		name = "this agent"
	}
	req.Instruction = "Standing instructions for " + name + ": " + strings.TrimSpace(req.Profile.Instructions) + "\n\n" + req.Instruction
	req.Profile = nil
}

// routineServiceIdentity returns how to give a routine's agent the service
// identity, or nil to run as the watch's owner. Both must hold: the routine's
// saved agent is marked runs_as service, and the watch's owner is an admin.
// Anyone else's routine on that agent runs as themselves, so the service's
// role is never borrowed by someone who could not have granted it.
func (s *graphjinService) routineServiceIdentity(ownerCtx context.Context, def watchRuntimeDefinition, cfg watchAgentConfig) func(context.Context) context.Context {
	identity := s.conf.Agent.ServiceIdentity
	if !identity.Configured() || cfg.Profile == "" {
		return nil
	}
	profile, err := s.visibleProfileRow(ownerCtx, cfg.Profile)
	if err != nil || profile == nil || profileRunsAs(profile) != profileRunsAsService {
		return nil
	}
	if !s.identityRoleIsAdmin(ownerCtx) {
		if s.log != nil {
			s.log.Warnf("watch %s: agent %s runs as the service identity, but the watch's owner is not an admin; the routine runs as its owner", def.ID, cfg.Profile)
		}
		return nil
	}
	return func(c context.Context) context.Context {
		origin := runOriginFrom(c)
		return withRunOrigin(s.ownerContext(c, identity.UserID, identity.Role, def.AccountID), origin)
	}
}

// watchRoutineProposeNote tells a routine it may act through approvals. The
// watch's owner set the routine up, so proposing a change is doing what they
// asked; it still runs only once they approve it. A routine runs on the data
// agent, which proposes a change by writing it.
const watchRoutineProposeNote = "If what you find calls for a change to the data, make it with a mutation: GraphJin holds the write for the watch owner's approval and nothing changes until they approve. Writing the mutation is how a change is proposed; describing a change in your answer proposes nothing. Pass expect with the mutation to say what the change should achieve, so GraphJin can check later whether it worked. Then say what you proposed and why. If nothing needs changing, say so."

// followUpProposeNote is the same for a follow-up, which runs on the lead.
const followUpProposeNote = "If what you find calls for a change to the data, ask the data agent to make it, in plain words, with ask_graphjin: GraphJin holds the change for the watch owner's approval and nothing changes until they approve. That request is how a change is proposed; describing a change in your reply proposes nothing. Then say what you proposed and why. If nothing needs changing, say so."

// watchRoutineAgentConfig is the agent config an unattended run uses. Nobody
// is watching it, so with write approvals on it may propose data and API
// writes and run none itself (HeldWritesOnly); without approvals it is
// read-only, as every routine used to be.
//
// A routine is one task with its event attached, so it runs on the data agent
// itself: the worker's guards refuse an answer no tool call backs, where the
// lead could answer "I proposed the change" without asking for it. A
// follow-up continues a conversation, so it keeps the lead (lead true).
func watchRoutineAgentConfig(conf *Config, lead bool) gjagent.Config {
	agentConf := agentConfigFromService(conf)
	if !lead {
		agentConf.Lead.Enabled = false
	}
	// Nobody is waiting on an unattended run, and a change it proposes takes
	// a lookup, the write and its expectation: give it room to finish.
	if agentConf.MaxSteps < unattendedMinSteps {
		agentConf.MaxSteps = unattendedMinSteps
	}
	if configApprovalsEnabled(conf) {
		agentConf.HeldWritesOnly = true
		agentConf.WriteApproval = gjagent.WriteApprovalRequired
	} else {
		agentConf.ReadOnly = true
	}
	return agentConf
}

// watchRoutineThreadID is the thread a watch's routine answers in when none is
// named. It is fixed per owner and watch, so concurrent deliveries share it.
func watchRoutineThreadID(ownerID, watchID string) string {
	return "th_w" + hashString(ownerID + ":routine:" + watchID)[:24]
}

// watchRoutineRunID is fixed per event, so a delivery that is attempted again
// cannot start a second run.
func watchRoutineRunID(eventID string) string {
	return "run_w" + hashString("routine:" + eventID)[:24]
}

func (s *graphjinService) watchRoutineThread(ownerCtx context.Context, def watchRuntimeDefinition, cfg watchAgentConfig) (string, error) {
	if cfg.ThreadID != "" {
		if _, err := s.ownedThreadStoreRow(ownerCtx, cfg.ThreadID); err != nil {
			return "", fmt.Errorf("agent delivery thread %s: %w", cfg.ThreadID, err)
		}
		return cfg.ThreadID, nil
	}
	if cfg.Profile != "" {
		home, err := s.profileHomeThread(ownerCtx, cfg.Profile)
		if err == nil {
			return stringMapValue(home, "id"), nil
		}
		if !errors.Is(err, errProfileNotFound) {
			return "", err
		}
		// The agent was deleted or unshared; answer in the watch's own thread
		// rather than drop the routine.
		if s.log != nil {
			s.log.Warnf("watch %s: agent %s is gone; routine answers in the watch thread", def.ID, cfg.Profile)
		}
	}
	id := watchRoutineThreadID(def.OwnerID, def.ID)
	if row, err := s.threadStoreRow(ownerCtx, id); err != nil {
		return "", err
	} else if row != nil {
		return id, nil
	}
	_, err := s.createThread(ownerCtx, threadSpec{ID: id, Title: "Watch: " + def.Name, Channel: "watch", ChannelRef: def.ID})
	if err != nil {
		// Another delivery may have created it first.
		if row, _ := s.threadStoreRow(ownerCtx, id); row != nil {
			return id, nil
		}
		return "", err
	}
	return id, nil
}

func (s *graphjinService) deliverWatchAgent(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchAgentConfig) (map[string]any, error) {
	receipt := map[string]any{"kind": "agent", "status": "failed"}
	if !s.threadsEnabled() {
		return receipt, errors.New("agent delivery needs agent threads; enable threads in the agent config")
	}
	origin := runOrigin{Origin: approvalOriginRoutine, WatchID: def.ID, EventID: event.ID, Profile: cfg.Profile}
	runAs := s.routineServiceIdentity(s.watchOwnerContext(ctx, def), def, cfg)
	if runAs != nil {
		origin.DelegatedBy = def.OwnerID
		origin.RunAsID, origin.RunAsRole = s.conf.Agent.ServiceIdentity.UserID, s.conf.Agent.ServiceIdentity.Role
	}
	ownerCtx := withRunOrigin(s.watchOwnerContext(ctx, def), origin)
	agentCtx := ownerCtx
	if runAs != nil {
		agentCtx = runAs(ownerCtx)
	}
	threadID, err := s.watchRoutineThread(ownerCtx, def, cfg)
	if err != nil {
		return receipt, err
	}
	runID := watchRoutineRunID(event.ID)
	receipt["thread_id"], receipt["run_id"] = threadID, runID

	data := parseJSONValue(event.DataJSON)
	instruction := watchRoutineInstruction(def, cfg.Prompt, mapFromAny(data))
	if s.approvalsEnabled() {
		instruction += "\n\n" + watchRoutineProposeNote
	}
	req := gjagent.Request{
		Instruction: instruction,
		Context: map[string]any{
			"_watch_events": []any{map[string]any{"id": event.ID, "watch_id": def.ID, "data_json": data}},
			"watch":         map[string]any{"id": def.ID, "name": def.Name},
		},
		Capabilities: s.agentCapabilityProfile(agentCtx),
	}
	opts := threadRunOptions{RunID: runID, Display: watchRoutineDisplay(def, mapFromAny(data)) + "\n" + cfg.Prompt, RunAs: runAs}
	if cfg.Once {
		// A follow-up the agent scheduled: the note is the whole story.
		req.Instruction = cfg.Prompt
		if s.approvalsEnabled() {
			req.Instruction += "\n\n" + followUpProposeNote
		}
		opts.Display = "Follow-up: " + cfg.Prompt
	}
	run, err := s.prepareWatchRoutineRun(ownerCtx, threadID, &req, opts)
	if err != nil {
		return receipt, err
	}
	if !cfg.Once {
		foldProfileIntoTask(&req)
		// The watch already knows what it is about: its query's tables.
		req.PrefetchCatalogIDs = s.queryCatalogTableIDs(agentCtx, def.Query)
		// The rows it fired on were read by GraphJin and are in the task: a
		// verdict drawn from them is grounded without more discovery.
		if rows := watchRoutineRows(mapFromAny(data)); rows != nil {
			req.ObservedRows = rows
		}
	}

	runner, err := s.agentEntryRunner(watchRoutineAgentConfig(s.conf, cfg.Once))
	if err != nil {
		// The run row exists; record the failure in the thread rather than
		// leaving it for the lease sweep.
		run.execute(ownerCtx, failingAgentRunner{err: err}, req, s.watchRoutineSettle(req), nil) //nolint:errcheck
		return receipt, err
	}
	resp, err := run.execute(ownerCtx, runner, req, s.watchRoutineSettle(req), nil)
	receipt["answer_status"] = resp.Status
	if err != nil {
		return receipt, err
	}
	receipt["status"] = "delivered"
	receipt["delivered_at"] = time.Now().UTC().Format(time.RFC3339)
	s.notifyRoutineAnswer(ownerCtx, def, event, threadID, resp)
	return receipt, nil
}

// notifyRoutineAnswer tells the owner a routine answered, and marks the watch
// event seen: the notification and the thread now carry it, so it no longer
// waits in the watch inbox. A held write already posted its own notification,
// so the answer that proposed it only links to it.
func (s *graphjinService) notifyRoutineAnswer(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, threadID string, resp gjagent.Response) {
	if !s.notificationsEnabled() {
		return
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		name = "A watch"
	}
	// The title says what happened to the data, from the engine's facts.
	severity := notificationSeverityInfo
	title := name + " fired: no change proposed"
	switch {
	case resp.Status == gjagent.StatusNeedsApproval:
		title = name + " fired: a change waits for your approval"
	case resp.Status == gjagent.StatusError || resp.Status == gjagent.StatusBlocked:
		severity, title = notificationSeverityWarning, name+" fired: the agent could not finish"
	case strings.HasPrefix(resp.Answer, "Made "):
		title = name + " fired: the agent made a change on its own"
	}
	s.notify(ctx, notification{
		OwnerID: def.OwnerID, AccountID: def.AccountID,
		Kind: notificationRoutineAnswer, Severity: severity,
		Title: title, Body: truncateUTF8(strings.TrimSpace(resp.Answer), 600),
		RefKind: "thread", RefID: threadID, Link: "/user/agent?thread=" + threadID,
		DedupeKey: "routine:" + event.ID,
	})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.internalStoreMutationRows(context.WithoutCancel(ctx), "watch_events", `where: { id: { eq: $id }, seen: { eq: false } }, update: $input`, "id",
		map[string]any{"id": event.ID, "input": map[string]any{"seen": true, "seen_at": now, "updated_at": now}}); err != nil {
		if s.log != nil {
			s.log.Warnf("watch event %s: mark seen after routine: %v", event.ID, err)
		}
		return
	}
	_ = s.bumpArtifactRevision(context.WithoutCancel(ctx), "watch_events")
}

// prepareWatchRoutineRun waits for a thread that is busy with another turn,
// such as the owner asking a follow-up, instead of dropping the routine.
func (s *graphjinService) prepareWatchRoutineRun(ctx context.Context, threadID string, req *gjagent.Request, opts threadRunOptions) (*threadRun, error) {
	deadline := time.Now().Add(watchRoutineBusyWait)
	wait := time.Second
	for {
		run, err := s.prepareThreadRunWith(ctx, threadID, req, opts)
		if !errors.Is(err, errThreadBusy) || time.Now().After(deadline) {
			return run, err
		}
		if err := sleepContext(ctx, wait); err != nil {
			return nil, err
		}
		if wait < 8*time.Second {
			wait *= 2
		}
	}
}

func (s *graphjinService) watchRoutineSettle(req gjagent.Request) func(context.Context, gjagent.Response, error) (gjagent.Response, error) {
	return func(ctx context.Context, resp gjagent.Response, err error) (gjagent.Response, error) {
		resp, err = s.settleAgentResponse(ctx, req, taskWarmStart{}, resp, err)
		return s.stateUnattendedOutcome(ctx, resp), err
	}
}

// stateUnattendedOutcome makes GraphJin, not the model, say what a run nobody
// watched did to the data. A model can write "I proposed the change" without
// asking for it; the engine knows whether a write was held, ran under an
// autonomy rule, or did not happen, and leads the answer with that.
//
// A held write already carries the engine's own sentence (approvalAnswer).
func (s *graphjinService) stateUnattendedOutcome(ctx context.Context, resp gjagent.Response) gjagent.Response {
	if resp.Status == gjagent.StatusNeedsApproval {
		return resp
	}
	outcome := unattendedNoChange(runOriginFrom(ctx))
	if changes := s.autonomousChangesInRun(ctx, runThreadFrom(ctx).runID); len(changes) != 0 {
		outcome = "Made " + strings.Join(changes, "; ") + " on its own, as configured."
	}
	switch {
	case strings.TrimSpace(resp.Answer) == "":
		if len(resp.Errors) == 0 {
			resp.Answer = outcome
		}
	case runOriginFrom(ctx).Origin == approvalOriginContinuation:
		// A follow-up leads with the decision's result; the engine's line
		// closes it.
		resp.Answer = resp.Answer + "\n\n" + outcome
	default:
		resp.Answer = outcome + "\n\n" + resp.Answer
	}
	return resp
}

func unattendedNoChange(origin runOrigin) string {
	if origin.Origin == approvalOriginContinuation {
		return "No further change was proposed."
	}
	return "No change was proposed."
}

// autonomousChangesInRun lists the writes an autonomy rule ran in this run.
func (s *graphjinService) autonomousChangesInRun(ctx context.Context, runID string) []string {
	if runID == "" || !s.approvalsEnabled() {
		return nil
	}
	rows, err := s.internalStoreRows(context.WithoutCancel(ctx), "approvals",
		`where: { run_id: { eq: $run_id }, status: { eq: "executed" }, decided_by: { like: "autonomy:%" } }, limit: 20`,
		"summary_json", map[string]any{"run_id": runID})
	if err != nil {
		return nil
	}
	var out []string
	for _, row := range rows {
		if summary := approvalSummaryText(row); summary != "" {
			out = append(out, "a change ("+summary+")")
		}
	}
	return out
}

type failingAgentRunner struct{ err error }

func (r failingAgentRunner) Run(context.Context, gjagent.Request) (gjagent.Response, error) {
	return gjagent.Response{}, r.err
}

// watchRoutineInstruction is the prompt plus a plain-language brief of the
// event. It names no field paths or watch ids: the agent repeats what it is
// given, and an answer citing a field the run never observed is rejected, so a
// brief like "orders.0.count_id is 3" made routines exhaust their steps. The
// full event rides in the run context (_watch_events).
//
// A data event's rows are the routine's subject, so they are part of the task
// (watchRoutineEventRows): the data agent reads its context only from code,
// and a routine that cannot see the rows it fired on goes looking for them.
// Their keys are columns the run context carries, so citing them is grounded.
func watchRoutineInstruction(def watchRuntimeDefinition, prompt string, data map[string]any) string {
	out := prompt + "\n\n" + watchRoutineBrief(data)
	if rows := watchRoutineEventRows(data); rows != "" {
		out += " The rows it returned: " + rows
	}
	return out
}

const watchRoutineEventRowsMaxBytes = 3000

// watchRoutineEventRows is a data event's rows as compact JSON, capped in
// size; empty when watchRoutineRows has none.
func watchRoutineEventRows(data map[string]any) string {
	rows := watchRoutineRows(data)
	if rows == nil {
		return ""
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	if len(raw) > watchRoutineEventRowsMaxBytes {
		return truncateUTF8(string(raw), watchRoutineEventRowsMaxBytes) + " (truncated; the full event is in the run context)"
	}
	return string(raw)
}

// watchRoutineRows is a data event's rows, without the pagination cursors;
// nil for metric, schedule and absence events, whose brief already says what
// happened.
func watchRoutineRows(data map[string]any) map[string]any {
	switch stringFromAny(data["kind"]) {
	case "metric", "schedule", "absence":
		return nil
	}
	rows := map[string]any{}
	for key, value := range data {
		if strings.HasSuffix(key, "_cursor") || key == "kind" {
			continue
		}
		rows[key] = value
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

var watchRoutineOpWords = map[string]string{
	"gt": "above", "gte": "at or above", "lt": "below", "lte": "at or below", "eq": "at", "ne": "other than",
}

func watchRoutineBrief(data map[string]any) string {
	switch stringFromAny(data["kind"]) {
	case "metric":
		m := mapFromAny(data["metric"])
		op := stringFromAny(m["op"])
		rule := ""
		switch op {
		case "changed":
			rule = "it fires when the number changes"
		case "change_pct":
			rule = fmt.Sprintf("it fires when the number moves by %v%% or more", m["threshold"])
		default:
			rule = fmt.Sprintf("it fires when the number is %s %v", watchRoutineOpWords[op], m["threshold"])
		}
		if crossings, ok := data["crossings"].([]any); ok {
			parts := make([]string, 0, len(crossings))
			for _, c := range crossings {
				cm := mapFromAny(c)
				parts = append(parts, fmt.Sprintf("%v (%v)", cm["key"], cm["value"]))
			}
			return fmt.Sprintf("This was triggered by a watch firing for %s; %s for a group.", strings.Join(parts, ", "), rule)
		}
		line := fmt.Sprintf("This was triggered by a watch firing: the number it watches is now %v", data["value"])
		if prev, ok := data["previous"]; ok {
			line += fmt.Sprintf(", it was %v", prev)
		}
		return line + "; " + rule + "."
	case "schedule":
		return "This was triggered by a scheduled check."
	case "absence":
		return "This was triggered because a watch saw no new data in its window."
	}
	return "This was triggered by a watch firing."
}

// watchRoutineDisplay is the one line the thread shows for the event.
func watchRoutineDisplay(def watchRuntimeDefinition, data map[string]any) string {
	switch stringFromAny(data["kind"]) {
	case "metric":
		m := mapFromAny(data["metric"])
		if crossings, ok := data["crossings"].([]any); ok {
			keys := make([]string, 0, len(crossings))
			for _, c := range crossings {
				keys = append(keys, fmt.Sprint(mapFromAny(c)["key"]))
			}
			return fmt.Sprintf("Watch %q fired for %s (%s %v).", def.Name, strings.Join(keys, ", "), stringFromAny(m["op"]), m["threshold"])
		}
		line := fmt.Sprintf("Watch %q fired: %s is %v", def.Name, stringFromAny(m["path"]), data["value"])
		if prev, ok := data["previous"]; ok {
			line += fmt.Sprintf(" (was %v)", prev)
		}
		if t, ok := m["threshold"]; ok {
			line += fmt.Sprintf(", %s %v", stringFromAny(m["op"]), t)
		}
		return line + "."
	case "schedule":
		return fmt.Sprintf("Scheduled check %q (%s).", def.Name, stringFromAny(data["scheduled_for_local"]))
	case "absence":
		return fmt.Sprintf("Watch %q saw no data in its window.", def.Name)
	}
	return fmt.Sprintf("Watch %q fired.", def.Name)
}

// watchRoutineThreadProjection names the thread an agent-delivery watch
// answers in, so clients can link to it before the first firing.
func watchRoutineThreadProjection(row map[string]any) string {
	cfg, _, err := parseWatchDeliveryConfig(jsonMapString(row, "delivery_json"))
	if err != nil || cfg.Kind != "agent" {
		return ""
	}
	if cfg.Agent.ThreadID != "" {
		return cfg.Agent.ThreadID
	}
	if cfg.Agent.Profile != "" {
		return profileHomeThreadID(stringMapValue(row, "owner_id"), cfg.Agent.Profile)
	}
	return watchRoutineThreadID(stringMapValue(row, "owner_id"), stringMapValue(row, "id"))
}

// A thread run carries its thread's agent on the context, so a routine watch
// created from an agent's chat answers back in that agent's home thread.

type runThreadProfileKey struct{}

func withRunThreadProfile(ctx context.Context, profile string) context.Context {
	if profile = strings.TrimSpace(profile); profile == "" {
		return ctx
	}
	return context.WithValue(ctx, runThreadProfileKey{}, profile)
}

func runThreadProfile(ctx context.Context) string {
	profile, _ := ctx.Value(runThreadProfileKey{}).(string)
	return profile
}

// resolveWatchRoutineProfile fills an agent delivery's profile from the run's
// thread when the delivery names neither a thread nor an agent, and checks
// that a named agent is one the owner can see.
func (s *graphjinService) resolveWatchRoutineProfile(ctx context.Context, deliveryJSON string) (string, error) {
	cfg, enabled, err := parseWatchDeliveryConfig(deliveryJSON)
	if err != nil || !enabled || cfg.Kind != "agent" || cfg.Agent.ThreadID != "" {
		return deliveryJSON, err
	}
	profile := cfg.Agent.Profile
	if profile == "" {
		if profile = runThreadProfile(ctx); profile == "" {
			return deliveryJSON, nil
		}
		m := mapFromAny(parseJSONValue(deliveryJSON))
		if nested := mapFromAny(m["agent"]); nested != nil {
			nested["profile"] = profile
		} else {
			m["profile"] = profile
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return "", err
		}
		deliveryJSON = string(raw)
	}
	row, err := s.visibleProfileRow(ctx, profile)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", fmt.Errorf("agent delivery profile %s not found", profile)
	}
	return deliveryJSON, nil
}
