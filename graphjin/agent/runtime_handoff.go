package agent

import (
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

const (
	runtimeLastExecutionKey    = "graphjinLastExecution"
	runtimeDistilledContextKey = "graphjinDistilledContext"
	runtimeExecutorRequestKey  = "graphjinExecutorRequest"
	runtimeHistoryKey          = "graphjinHistory"
)

// graphJinCodeRuntime keeps GraphJin execution evidence available across Ax's
// distiller -> executor session patch. The current Ax Goja session reserves
// runtime input names, so PatchGlobals drops executorRequest and
// distilledContext even though Ax includes them in the patch snapshot. Copying
// only those two narrowed handoff fields to GraphJin-owned non-input bindings
// preserves the RLM contract without putting caller context or live rows in a
// prompt. The most recent authorized execution is also retained as a narrow
// recovery path when the distiller already completed the governed query.
type graphJinCodeRuntime struct {
	base                   *axgoja.Runtime
	lastExecution          func() any
	handoffFallback        func() any
	onDistilledResult      func(any)
	pendingFinal           func() string
	pendingContinuation    func() string
	completionContinuation func() string
	clarificationNudge     func() string
}

// WithHandoffFallback supplies runtime-only evidence gathered by GraphJin
// callables during distillation. Ax preserves JavaScript variables across the
// stage patch, but a weak distiller can still discard a useful catalog result
// by ending with final(request, {}). Keep the governed result on the same Goja
// session instead of asking the executor to rediscover or guess it.
func (r *graphJinCodeRuntime) WithHandoffFallback(fallback func() any) *graphJinCodeRuntime {
	r.handoffFallback = fallback
	return r
}

// WithClarificationNudge supplies the one-shot redirect for a clarification
// asked before any real discovery. Empty string means let the ask through.
func (r *graphJinCodeRuntime) WithClarificationNudge(nudge func() string) *graphJinCodeRuntime {
	r.clarificationNudge = nudge
	return r
}

func newGraphJinCodeRuntime(lastExecution func() any, onDistilledResult func(any), pendingFinal func() string, pendingContinuation func() string, completionContinuation ...func() string) *graphJinCodeRuntime {
	runtime := &graphJinCodeRuntime{
		base:                axgoja.NewRuntime(),
		lastExecution:       lastExecution,
		onDistilledResult:   onDistilledResult,
		pendingFinal:        pendingFinal,
		pendingContinuation: pendingContinuation,
	}
	if len(completionContinuation) != 0 {
		runtime.completionContinuation = completionContinuation[0]
	}
	return runtime
}

func (r *graphJinCodeRuntime) Language() string {
	return r.base.Language()
}

func (r *graphJinCodeRuntime) UsageInstructions() string {
	return r.base.UsageInstructions()
}

func (r *graphJinCodeRuntime) RegisterCallable(name string, handler axgoja.HostCallable) *graphJinCodeRuntime {
	r.base.RegisterCallable(name, handler)
	return r
}

// RegisterHostCallable preserves Ax's structural hook for llmQuery.
func (r *graphJinCodeRuntime) RegisterHostCallable(name string, handler func(ax.Value) (ax.Value, error)) {
	r.base.RegisterHostCallable(name, handler)
}

func (r *graphJinCodeRuntime) CreateSession(globals map[string]ax.Value, options map[string]ax.Value) (ax.CodeSession, error) {
	session, err := r.base.CreateSession(globals, options)
	if err != nil {
		return nil, err
	}
	inputs := mapValue(globals["inputs"])
	context := mapValue(inputs["context"])
	return &graphJinCodeSession{
		base:                   session,
		lastExecution:          r.lastExecution,
		handoffFallback:        r.handoffFallback,
		onDistilledResult:      r.onDistilledResult,
		pendingFinal:           r.pendingFinal,
		pendingContinuation:    r.pendingContinuation,
		completionContinuation: r.completionContinuation,
		clarificationNudge:     r.clarificationNudge,
		originalInstruction:    stringFromMap(inputs, "instruction"),
		seedContext:            normalizeValue(context[protocolContextKey]),
		originalHistory:        normalizeValue(inputs["history"]),
	}, nil
}

type graphJinCodeSession struct {
	base                   ax.CodeSession
	lastExecution          func() any
	handoffFallback        func() any
	onDistilledResult      func(any)
	pendingFinal           func() string
	pendingContinuation    func() string
	completionContinuation func() string
	clarificationNudge     func() string
	originalInstruction    string
	seedContext            any
	originalHistory        any
	executorRequest        any
	distilledContext       any
	explicitHandoff        bool
	executorStage          bool
	handoffReadRequired    bool
	handoffRead            bool
	historyReadRequired    bool
	historyRead            bool
}

func (s *graphJinCodeSession) Execute(code string, options map[string]ax.Value) ax.Value {
	readsHistory := referencesRuntimeHistory(code)
	readsHandoff := referencesRuntimeHandoff(code)
	usesGraphJinCallable := referencesGraphJinCallable(code)
	if s.executorStage && s.historyReadRequired && !s.historyRead && !readsHistory {
		// The condition and the safe recovery are both known in Go. Surface the
		// bounded prior turns once, mark the prerequisite satisfied, and let the
		// next actor step use the returned value. Requiring a weak model to spell a
		// magic global name merely turns a deterministic handoff into a retry loop.
		s.historyRead = true
		return map[string]any{
			"kind": "result",
			"result": map[string]any{
				"graphjin_protocol": "history_read_required",
				"message":           "This follow-up depends on prior turns. GraphJin supplied the bounded history below; use its most recent relevant content, then re-establish this run's catalog evidence.",
				"history":           normalizeValue(s.originalHistory),
				"next":              "Resolve the referenced entity from history, inspect its exact catalog detail, and execute the required query.",
			},
		}
	}
	if s.executorStage && s.handoffReadRequired && !s.handoffRead && !readsHandoff && (!usesGraphJinCallable || s.explicitHandoff) {
		return map[string]any{
			"kind": "result",
			"result": map[string]any{
				"graphjin_protocol": "runtime_handoff_read_required",
				"message":           "GraphJin handoff evidence is available only in the JavaScript runtime. Read and narrow globalThis.graphjinDistilledContext (or the relevant graphjinHistory/graphjinLastExecution binding) before calling tools or finalizing.",
				"next":              "Use the runtime-only handoff to choose the exact catalog id, then perform this run's required GraphJin discovery and execution calls.",
			},
		}
	}
	result := s.base.Execute(code, options)
	if readsHistory {
		s.historyRead = true
	}
	if readsHandoff {
		s.handoffRead = true
	}
	if usesGraphJinCallable && !s.explicitHandoff {
		// A real GraphJin call supplies fresh, same-run evidence and is still
		// governed by the protocol runtime. Do not let the context-orientation
		// guard preempt stricter query/mutation evidence checks.
		s.handoffRead = true
	}
	if !s.executorStage {
		s.captureDistillerHandoff(result)
	}
	completionType := runtimeCompletionType(result)
	if s.executorStage && (completionType == "final" || completionType == "askclarification") && s.pendingFinal != nil {
		if message := strings.TrimSpace(s.pendingFinal()); message != "" {
			protocolCode, publicMessage := pendingFinalProtocol(message)
			if s.pendingContinuation != nil {
				if continuation := strings.TrimSpace(s.pendingContinuation()); continuation != "" {
					if protocolCode == "execution_evidence_required" {
						// Recoverable write/control-plane guards already know the exact
						// catalog evidence still required. Run only that discovery now;
						// because the code has no final(), Ax advances to another executor
						// turn where the model can author from the returned detail.
						s.handoffRead = true
						return s.base.Execute(continuation, options)
					}
					if protocolCode != "saved_query_execution_required" {
						return map[string]any{
							"kind": "result",
							"result": map[string]any{
								"graphjin_protocol": protocolCode,
								"message":           publicMessage,
								"next":              publicMessage,
							},
						}
					}
					// The model already chose to terminate, so execute the narrow,
					// protocol-derived continuation in the same runtime session and
					// return its live result to the next actor step. This is limited to
					// an exact saved-query route selected from same-run discovery; the
					// model still owns interpretation and the final response.
					s.handoffRead = true
					continued := s.base.Execute(continuation, options)
					if evidence, ok := runtimeFinalEvidence(continued); ok {
						return map[string]any{
							"kind": "result",
							"result": map[string]any{
								"graphjin_protocol": "saved_query_execution_completed",
								"message":           "The required governed execution completed. Interpret continuation.execution.data and finalize the user's answer now.",
								"continuation":      evidence,
							},
						}
					}
					return continued
				}
			}
			return map[string]any{
				"kind": "result",
				"result": map[string]any{
					"graphjin_protocol": protocolCode,
					"message":           publicMessage,
					"next":              publicMessage,
					"attempt":           runtimeFinalEvidenceValue(result),
				},
			}
		}
	}
	if s.executorStage && completionType == "askclarification" && s.clarificationNudge != nil {
		if message := strings.TrimSpace(s.clarificationNudge()); message != "" {
			// A first-turn surrender: no final() in the returned code, so Ax
			// advances another executor turn with the redirect as the tool
			// result. A repeat ask returns empty from the nudge and terminates
			// normally, so a genuine clarification is delayed by one turn at
			// most. Runs after the pendingFinal machinery on purpose — a run
			// that owes a repair keeps its stricter redirect.
			return map[string]any{
				"kind": "result",
				"result": map[string]any{
					"graphjin_protocol": "clarification_premature",
					"message":           message,
					"next":              "Run the named discovery steps and answer from what they return; ask the user only if the target is still ambiguous afterwards.",
				},
			}
		}
	}
	if s.executorStage && s.completionContinuation != nil {
		if continuation := strings.TrimSpace(s.completionContinuation()); continuation != "" {
			s.handoffRead = true
			s.refreshLastExecutionBinding(options)
			return s.base.Execute(continuation, options)
		}
	}
	return result
}

func (s *graphJinCodeSession) refreshLastExecutionBinding(options map[string]ax.Value) {
	if s.lastExecution == nil {
		return
	}
	execution := s.lastExecution()
	if execution == nil {
		return
	}
	snapshot := s.base.SnapshotGlobals(options)
	snapshot = snapshotWithRuntimeBinding(snapshot, runtimeLastExecutionKey, normalizeValue(execution))
	s.base.PatchGlobals(snapshot, options)
}

func pendingFinalProtocol(message string) (string, string) {
	for _, item := range []struct{ prefix, code string }{
		{"execution_repair_required:", "execution_repair_required"},
		{"execution_evidence_required:", "execution_evidence_required"},
		{"execution_retry_required:", "execution_retry_required"},
		{"database_computation_required:", "database_computation_required"},
	} {
		if strings.HasPrefix(message, item.prefix) {
			return item.code, strings.TrimSpace(strings.TrimPrefix(message, item.prefix))
		}
	}
	return "saved_query_execution_required", message
}

func (s *graphJinCodeSession) Inspect(options map[string]ax.Value) ax.Value {
	return redactRuntimeHandoffBindings(s.base.Inspect(options))
}

func (s *graphJinCodeSession) SnapshotGlobals(options map[string]ax.Value) ax.Value {
	return redactRuntimeHandoffBindings(s.base.SnapshotGlobals(options))
}

func (s *graphJinCodeSession) PatchGlobals(snapshot ax.Value, options map[string]ax.Value) ax.Value {
	s.executorStage = true
	s.historyReadRequired = hasRuntimeHandoffValue(s.originalHistory)
	distilled := runtimeBindingFromSnapshot(snapshot, "distilledContext")
	if distilled == nil {
		distilled = s.distilledContext
	}
	if distilled != nil {
		snapshot = snapshotWithRuntimeBinding(snapshot, runtimeDistilledContextKey, normalizeValue(distilled))
	}
	request := runtimeBindingFromSnapshot(snapshot, "executorRequest")
	if request == nil {
		request = s.executorRequest
	}
	if request != nil {
		snapshot = snapshotWithRuntimeBinding(snapshot, runtimeExecutorRequestKey, normalizeValue(request))
	}
	if hasRuntimeHandoffValue(s.originalHistory) {
		snapshot = snapshotWithRuntimeBinding(snapshot, runtimeHistoryKey, s.originalHistory)
	}
	if s.lastExecution != nil {
		if execution := s.lastExecution(); execution != nil {
			snapshot = snapshotWithRuntimeBinding(snapshot, runtimeLastExecutionKey, normalizeValue(execution))
		}
	}
	s.handoffReadRequired = hasRuntimeHandoffValue(s.distilledContext)
	// The bindings must exist in the VM, while the patch result and later
	// inspections expose only a compact availability marker. They are the Go
	// runtime equivalent of Ax context fields: code-addressable, prompt-redacted
	// state.
	return redactRuntimeHandoffBindings(s.base.PatchGlobals(snapshot, options))
}

func referencesRuntimeHistory(code string) bool {
	return strings.Contains(code, runtimeHistoryKey) || strings.Contains(code, "inputs.history")
}

func referencesRuntimeHandoff(code string) bool {
	for _, name := range []string{
		"inputs.context",
		runtimeLastExecutionKey,
		runtimeDistilledContextKey,
		runtimeHistoryKey,
	} {
		if strings.Contains(code, name) {
			return true
		}
	}
	return false
}

func referencesGraphJinCallable(code string) bool {
	for _, name := range []string{
		"query_catalog", "graphql_help", "validate_where_clause",
		"execute_saved_query", "execute_graphql",
	} {
		if referencesCallableInvocation(code, name) {
			return true
		}
	}
	return false
}

// referencesCallableInvocation recognizes the bare runtime-call form even when
// a model inserts whitespace before the opening parenthesis. The boundary check
// avoids treating longer helper names as GraphJin calls.
func referencesCallableInvocation(code, name string) bool {
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], name)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || !isJavaScriptIdentifierByte(code[index-1])
		after := index + len(name)
		for after < len(code) && (code[after] == ' ' || code[after] == '\t' || code[after] == '\r' || code[after] == '\n') {
			after++
		}
		if beforeOK && after < len(code) && code[after] == '(' {
			return true
		}
		offset = index + len(name)
	}
	return false
}

func isJavaScriptIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func instructionNeedsHistory(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	for _, phrase := range []string{
		"continue", "follow up", "follow-up", "repeat", "again",
		"recent trail", "prior trail", "previous", "earlier",
		"since then", "what changed", "last time",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func runtimeCompletionType(value any) string {
	step := mapValue(value)
	if step == nil {
		return ""
	}
	if payload := mapValue(step["completion_payload"]); payload != nil {
		step = payload
	}
	return strings.ToLower(stringFromMap(step, "type"))
}

func runtimeFinalEvidence(value any) (any, bool) {
	step := mapValue(value)
	if step == nil {
		return nil, false
	}
	if payload := mapValue(step["completion_payload"]); payload != nil {
		step = payload
	}
	if !strings.EqualFold(stringFromMap(step, "type"), "final") {
		return nil, false
	}
	args := anySlice(step["args"])
	if len(args) < 2 {
		return nil, false
	}
	return normalizeValue(args[1]), true
}

func runtimeFinalEvidenceValue(value any) any {
	evidence, _ := runtimeFinalEvidence(value)
	return evidence
}

func (s *graphJinCodeSession) captureDistillerHandoff(result ax.Value) {
	step := mapValue(result)
	if step == nil {
		return
	}
	payload := mapValue(step["completion_payload"])
	if payload == nil {
		payload = step
	}
	if !strings.EqualFold(stringFromMap(payload, "type"), "final") {
		return
	}
	args := anySlice(payload["args"])
	var narrowed any
	if len(args) > 0 {
		request := normalizeValue(args[0])
		if text, ok := request.(string); ok && strings.TrimSpace(text) != "" {
			s.executorRequest = text
		} else {
			// The Ax distiller contract requires final(request, evidence), but a
			// model can occasionally pass a draft response object as the first
			// argument. Never hand that draft to the executor as its request: it
			// may contain a premature "not found" conclusion. Restore the user's
			// concrete instruction instead.
			s.executorRequest = s.originalInstruction
			if draft := mapValue(request); draft != nil {
				for _, key := range []string{"evidence", "context", "data"} {
					if value := normalizeValue(draft[key]); hasRuntimeHandoffValue(value) {
						s.distilledContext = value
						narrowed = value
						s.explicitHandoff = true
						break
					}
				}
			}
		}
	}
	if len(args) > 1 {
		if value := normalizeValue(args[1]); hasRuntimeHandoffValue(value) {
			s.distilledContext = value
			narrowed = value
			s.explicitHandoff = true
		}
	}
	if !hasRuntimeHandoffValue(s.distilledContext) && s.handoffFallback != nil {
		if value := normalizeValue(s.handoffFallback()); hasRuntimeHandoffValue(value) {
			s.distilledContext = value
			narrowed = value
			s.explicitHandoff = true
		}
	}
	if !hasRuntimeHandoffValue(s.distilledContext) && hasRuntimeHandoffValue(s.seedContext) {
		// Keep the full seed on the runtime-only channel. This is the safe
		// fallback when the distiller omitted or malformed its evidence; it is
		// never copied into a staged prompt.
		s.distilledContext = s.seedContext
	}
	if s.onDistilledResult != nil && hasRuntimeHandoffValue(narrowed) {
		s.onDistilledResult(narrowed)
	}
}

func hasRuntimeHandoffValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) != 0
	case map[string]any:
		return len(typed) != 0
	default:
		return true
	}
}

func runtimeBindingFromSnapshot(snapshot ax.Value, key string) any {
	original := mapValue(snapshot)
	if original == nil {
		return nil
	}
	for _, field := range []string{"bindings", "globals"} {
		if bindings := mapValue(original[field]); bindings != nil {
			if value, ok := bindings[key]; ok {
				return value
			}
		}
	}
	return original[key]
}

func (s *graphJinCodeSession) Close() ax.Value {
	return s.base.Close()
}

func snapshotWithRuntimeBinding(snapshot ax.Value, key string, value any) ax.Value {
	original := mapValue(snapshot)
	if original == nil {
		return snapshot
	}
	out := cloneAnyMap(original)
	wrote := false
	for _, field := range []string{"bindings", "globals"} {
		bindings := mapValue(original[field])
		if bindings == nil {
			continue
		}
		next := cloneAnyMap(bindings)
		next[key] = value
		out[field] = next
		wrote = true
	}
	if !wrote {
		out[key] = value
	}
	return out
}

func redactRuntimeHandoffBindings(snapshot ax.Value) ax.Value {
	original := mapValue(snapshot)
	if original == nil {
		return snapshot
	}
	out := cloneAnyMap(original)
	hidden := []string{
		runtimeLastExecutionKey,
		runtimeDistilledContextKey,
		runtimeExecutorRequestKey,
		runtimeHistoryKey,
	}
	for _, key := range hidden {
		if _, ok := out[key]; ok {
			out[key] = runtimeHandoffRedaction(key)
		}
	}
	for _, field := range []string{"bindings", "globals"} {
		bindings := mapValue(original[field])
		if bindings == nil {
			continue
		}
		next := cloneAnyMap(bindings)
		for _, key := range hidden {
			if _, ok := next[key]; ok {
				next[key] = runtimeHandoffRedaction(key)
			}
		}
		out[field] = next
	}
	return out
}

func runtimeHandoffRedaction(key string) string {
	return "[runtime-only GraphJin handoff; read globalThis." + key + " in JavaScript]"
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}
