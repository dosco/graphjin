package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const protocolContextKey = "_graphjin_discovery"

const emptySearchKnownIDLimit = 6

// Keep complete ordinary GraphQL operations in the private action trail so
// evaluation method rules never depend on argument order within a mutation.
// Variables and headers remain redacted separately below.
const actionQueryTraceLimit = 8192

type protocolRuntime struct {
	base          GraphRuntime
	state         *discoveryState
	namespace     string
	seedLimit     int
	catalogSearch CatalogSearchFeatures
}

type discoveryState struct {
	instruction             string
	seedOK                  bool
	seedResult              any
	modelDiscoveryAction    bool
	catalogIDs              map[string]bool
	catalogKinds            map[string]bool
	savedQueriesDiscovered  map[string]bool
	savedQueriesDetailed    map[string]bool
	savedQueryGraphQL       map[string]string
	securityRuntimeEvidence bool
	watchDefinitionMutated  bool
	annotationMutated       bool
	// Per-target mutation evidence, populated only by id-detail lookups and
	// validations in THIS run (search hits never count, mirroring the
	// saved-query detail rule).
	detailKinds       map[string]bool
	tablesDetailed    map[string]bool
	tablesValidated   map[string]bool
	workflowsDetailed map[string]bool
	actions           []protocolAction
	helpTopics        []string
	catalogSearches   []map[string]any
	catalogDetails    []string
	suggestedNext     []any
	validations       []map[string]any
	executions        []map[string]any
	// A failed raw GraphQL execution must be followed by one genuinely
	// different repair execution before the actor may terminate. Identical
	// retries are rejected without spending another database execution. Failed
	// identities remain rejected for the rest of the run; the pending key only
	// tracks whether the current failure still needs one distinct repair.
	pendingFailedQueryKey string
	failedQueryKeys       map[string]bool
	// Successful executions are memoized by their normalized operation and
	// variables. Repeating one never reaches the database again. The first
	// redundant call arms a one-turn completion grace period; another already
	// seen call lets the runtime finalize from the governed cached evidence.
	successfulExecutions map[string]any
	completionLatchKey   string
	completionReady      bool
	// lastExecution retains the most recent successful execution result for
	// one narrow recovery case: an actor that ignores that result and repeats
	// the preceding catalog lookup. Ax's runtime is a persistent REPL, but the
	// next actor prompt emphasizes the latest tool result; replaying already
	// authorized data there prevents the result from being displaced by a
	// catalog-loop error.
	lastExecution any
	rawGraphQL    []map[string]any
	violations    []protocolViolation
	// catalogRequestKeys prevents an actor loop from issuing an identical
	// catalog request repeatedly instead of consuming the result it already
	// received. Only successful model calls are recorded; the seed and failed
	// attempts do not spend this budget.
	catalogRequestKeys map[string]bool
	// One empty lexical/coverage search is allowed and returned with concrete
	// recovery guidance. A second blind search is rejected before dispatch so
	// the actor must consume known ids or enumerate the catalog by kind.
	emptySearchStreak int
	// One unresolved id-detail lookup is likewise allowed with concrete known-id
	// guidance. A second unknown guess is rejected before dispatch. A detail id
	// already surfaced by this run remains eligible for lookup so following the
	// recovery guidance can resolve and reset the streak.
	emptyDetailStreak  int
	capabilities       *CapabilityProfile
	observe            func(ActionEvent)
	coverageSearchUsed bool
	// history is bounded, untrusted caller/task context. It never satisfies a
	// discovery guard, but an explicit request to repeat a prior answered turn
	// can safely recover from an actor-loop failure after this run has performed
	// its own model-driven discovery.
	history []Turn
	// groundingCorpus accumulates this run's observed evidence (instruction,
	// history, tool arguments, tool results) for the answer-grounding check.
	groundingCorpus   strings.Builder
	groundingOverflow bool
}

type protocolAction struct {
	Step    int            `json:"step"`
	Source  string         `json:"source"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
	Error   string         `json:"error,omitempty"`

	startedAt time.Time
}

type protocolViolation struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Tool     string         `json:"tool,omitempty"`
	Blocking bool           `json:"blocking"`
	Details  map[string]any `json:"details,omitempty"`
}

func newProtocolRuntime(base GraphRuntime, instruction, namespace string, seedLimit int, profile *CapabilityProfile, observe func(ActionEvent), catalogSearch CatalogSearchFeatures) *protocolRuntime {
	if seedLimit <= 0 {
		seedLimit = defaultSeedLimit
	}
	state := newDiscoveryState(instruction)
	state.capabilities = profile
	state.observe = observe
	state.addCapabilityIntentViolation()
	return &protocolRuntime{
		base:          base,
		namespace:     namespace,
		seedLimit:     seedLimit,
		state:         state,
		catalogSearch: catalogSearch,
	}
}

// addCapabilityIntentViolation turns an explicit server-side root denial into
// a deterministic policy result. The model may still inspect visible, redacted
// catalog guidance, but it cannot turn a configuration-write request into an
// answered result merely by describing an out-of-band edit.
func (s *discoveryState) addCapabilityIntentViolation() {
	if s == nil || !profileExplicitlyBlocksRoot(s.capabilities, systemRootConfig) || !configWriteIntent(s.instruction) {
		return
	}
	s.addViolation(
		"access_blocked",
		"the caller capability profile does not permit gj_config changes; an authorized administrator must perform this configuration update",
		toolExecuteGraphQL,
		true,
		map[string]any{"root": systemRootConfig},
	)
}

func profileExplicitlyBlocksRoot(profile *CapabilityProfile, root string) bool {
	if profile == nil {
		return false
	}
	for _, blocked := range profile.BlockedSystemRoots {
		if strings.EqualFold(strings.TrimSpace(blocked), strings.TrimSpace(root)) {
			return true
		}
	}
	return false
}

func configWriteIntent(instruction string) bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(instruction), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		words[word] = true
	}
	if !words["gj_config"] && !(words["graphjin"] && words["config"]) {
		return false
	}
	for _, verb := range []string{"add", "change", "configure", "delete", "disable", "edit", "enable", "modify", "remove", "set", "update"} {
		if words[verb] {
			return true
		}
	}
	return false
}

func newDiscoveryState(instruction string) *discoveryState {
	state := &discoveryState{
		instruction:            strings.TrimSpace(instruction),
		catalogIDs:             map[string]bool{},
		catalogKinds:           map[string]bool{},
		savedQueriesDiscovered: map[string]bool{},
		savedQueriesDetailed:   map[string]bool{},
		savedQueryGraphQL:      map[string]string{},
		detailKinds:            map[string]bool{},
		tablesDetailed:         map[string]bool{},
		tablesValidated:        map[string]bool{},
		workflowsDetailed:      map[string]bool{},
		catalogRequestKeys:     map[string]bool{},
		failedQueryKeys:        map[string]bool{},
		successfulExecutions:   map[string]any{},
	}
	state.addGrounding(state.instruction)
	return state
}

func (r *protocolRuntime) Seed(ctx context.Context) (any, error) {
	args := map[string]any{
		"search":  r.state.instruction,
		"explain": true,
		"limit":   r.seedLimit,
	}
	r.addNamespace(args)
	action := r.state.startAction("seed", "query_catalog", args)
	out, err := r.base.QueryCatalog(ctx, args)
	r.state.finishAction(action, "query_catalog", args, out, err)
	if err != nil {
		r.state.addViolation("catalog_seed_failed", "initial catalog discovery failed; the agent cannot safely answer without gj_catalog", "query_catalog", true, map[string]any{"error": err.Error()})
		return nil, err
	}
	r.state.seedOK = true
	r.state.recordCatalog(args, out, true)
	r.state.seedResult = normalizeValue(out)
	return r.state.seedResult, nil
}

func (r *protocolRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	action := r.state.startAction("model", "graphql_help", args)
	out, err := r.base.GraphQLHelp(ctx, args)
	r.state.finishAction(action, "graphql_help", args, out, err)
	if err == nil {
		r.state.clearCompletionLatch()
		r.state.modelDiscoveryAction = true
		topic := stringArg(args, "for")
		if topic == "" {
			topic = "discovery"
		}
		r.state.helpTopics = appendUniqueString(r.state.helpTopics, topic)
		r.state.recordCatalogRows(out, true)
		r.state.recordNext(out)
	}
	return out, err
}

func (r *protocolRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if _, hasCoverage := args["searches"]; hasCoverage {
		searches, err := r.validateCoverageSearches(args)
		if err != nil {
			action := r.state.startAction("model", "query_catalog", args)
			r.state.finishAction(action, "query_catalog", args, nil, err)
			return nil, err
		}
		args["searches"] = searches
		args["explain"] = true
		r.state.coverageSearchUsed = true
	}
	r.addNamespace(args)
	searchRequest := isCatalogSearchRequest(args)
	detailIDs := detailIDsFromArgs(args)
	requestKey := stringify(normalizeValue(args))
	if searchRequest && r.state.emptySearchStreak > 0 {
		r.state.emptySearchStreak++
		out := r.state.emptySearchExhaustedResult()
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, out, nil)
		return out, nil
	}
	if len(detailIDs) != 0 && r.state.emptyDetailStreak > 0 && !r.state.hasKnownCatalogID(detailIDs) {
		r.state.emptyDetailStreak++
		out := r.state.emptyDetailExhaustedResult(detailIDs)
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, out, nil)
		return out, nil
	}
	if requestKey != "" && r.state.catalogRequestKeys[requestKey] {
		message := "duplicate query_catalog call rejected: this exact request already returned catalog evidence; reuse the prior result and follow its next guidance instead of searching again"
		if r.state.lastExecution != nil {
			r.state.recordRepeatedCall("query_catalog:" + requestKey)
			out := map[string]any{
				"cards": []any{},
				"recovery": map[string]any{
					"reason":    "catalog evidence and live execution data were already returned in this run",
					"execution": r.state.lastExecution,
					"next":      "call final now using recovery.execution.result.data; do not call query_catalog again",
				},
			}
			action := r.state.startAction("model", "query_catalog", args)
			r.state.finishAction(action, "query_catalog", args, out, nil)
			return out, nil
		}
		err := fmt.Errorf("%s", message)
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, nil, err)
		return nil, err
	}
	action := r.state.startAction("model", "query_catalog", args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err == nil {
		r.state.clearCompletionLatch()
		if requestKey != "" {
			r.state.catalogRequestKeys[requestKey] = true
		}
		r.state.modelDiscoveryAction = true
		r.state.recordCatalog(args, out, false)
		if len(catalogCards(out)) != 0 {
			r.state.emptySearchStreak = 0
			if len(detailIDs) == 0 {
				r.state.emptyDetailStreak = 0
			}
		} else if searchRequest {
			r.state.emptySearchStreak = 1
			out = attachEmptySearchRecovery(out, r.state.emptySearchNext(
				"No catalog cards matched. Drop search filters and inspect a known id, or enumerate tables with query_catalog({kind:\"table\"}) instead of trying another blind search.",
			))
		}
		if len(detailIDs) != 0 {
			returned := returnedCatalogDetailIDs(detailIDs, out)
			if len(returned) != 0 {
				r.state.emptyDetailStreak = 0
			} else {
				r.state.emptyDetailStreak = 1
				out = attachEmptyDetailRecovery(out, detailIDs, r.state.emptyDetailNext(
					"The requested catalog id was not returned. Inspect a known id or enumerate tables by kind before trying another detail lookup.",
				))
			}
		}
	}
	r.state.finishAction(action, "query_catalog", args, out, err)
	return out, err
}

func isCatalogSearchRequest(args map[string]any) bool {
	if len(detailIDsFromArgs(args)) != 0 {
		return false
	}
	_, hasSearch := args["search"]
	_, hasSearches := args["searches"]
	return hasSearch || hasSearches
}

func attachEmptySearchRecovery(out any, next map[string]any) any {
	result := cloneAnyMap(mapValue(out))
	if result == nil {
		result = map[string]any{"cards": []any{}}
	}
	result["recovery"] = map[string]any{
		"kind":        "empty_search",
		"instruction": "The search returned no catalog cards. Broaden once by dropping filters, enumerate tables by kind, or inspect one of the catalog ids already discovered in this run.",
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	return result
}

func attachEmptyDetailRecovery(out any, missedIDs []string, next map[string]any) any {
	result := cloneAnyMap(mapValue(out))
	if len(catalogCards(result)) == 0 {
		result["cards"] = []any{}
	}
	recovery := map[string]any{
		"kind":        "empty_detail",
		"instruction": "The requested catalog detail was not found. Use a known catalog id from this run or enumerate tables by kind instead of guessing another id.",
		"missed_ids":  append([]string(nil), missedIDs...),
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	if len(missedIDs) == 1 {
		recovery["missed_id"] = missedIDs[0]
	}
	result["recovery"] = recovery
	return result
}

func (s *discoveryState) emptySearchExhaustedResult() executeResult {
	next := s.emptySearchNext("A prior catalog search already returned no cards. Enumerate tables by kind or inspect a known id before searching again.")
	return executeResult{
		Errors: []ErrorInfo{{
			Message: "consecutive empty catalog search rejected; stop varying blind search text and use the supplied catalog ids or enumerate tables by kind",
			Extensions: map[string]any{
				"code":      "empty_search_exhausted",
				"retryable": true,
				"graphjin_repair": map[string]any{
					"kind":      "empty_search_exhausted",
					"known_ids": next["known_ids"],
					"next":      next,
				},
			},
		}},
		Recovery: map[string]any{
			"kind":        "empty_search_exhausted",
			"instruction": "Do not issue another lexical search. Enumerate the catalog by kind or inspect a known id.",
			"known_ids":   next["known_ids"],
			"next":        next,
		},
	}
}

func (s *discoveryState) emptyDetailExhaustedResult(missedIDs []string) executeResult {
	next := s.emptyDetailNext("A prior catalog detail lookup returned no matching card. Inspect a known id or enumerate tables by kind before trying another detail lookup.")
	repair := map[string]any{
		"kind":       "empty_detail_exhausted",
		"missed_ids": append([]string(nil), missedIDs...),
		"known_ids":  next["known_ids"],
		"next":       next,
	}
	if len(missedIDs) == 1 {
		repair["missed_id"] = missedIDs[0]
	}
	recovery := map[string]any{
		"kind":        "empty_detail_exhausted",
		"instruction": "Do not guess another catalog id. Inspect one of the known ids or enumerate tables by kind.",
		"missed_ids":  repair["missed_ids"],
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	if len(missedIDs) == 1 {
		recovery["missed_id"] = missedIDs[0]
	}
	return executeResult{
		Errors: []ErrorInfo{{
			Message: "consecutive empty catalog detail lookup rejected; stop guessing ids and use a known catalog id or enumerate tables by kind",
			Extensions: map[string]any{
				"code":            "empty_detail_exhausted",
				"retryable":       true,
				"graphjin_repair": repair,
			},
		}},
		Recovery: recovery,
	}
}

func (s *discoveryState) emptySearchNext(reason string) map[string]any {
	knownIDs := s.knownCatalogIDs(emptySearchKnownIDLimit)
	return map[string]any{
		"recommended_tool": toolQueryCatalog,
		"args":             map[string]any{"kind": "table", "limit": 20},
		"reason":           reason,
		"known_ids":        knownIDs,
	}
}

func (s *discoveryState) emptyDetailNext(reason string) map[string]any {
	return s.emptySearchNext(reason)
}

func (s *discoveryState) hasKnownCatalogID(ids []string) bool {
	if s == nil {
		return false
	}
	for _, id := range ids {
		if s.catalogIDs[id] {
			return true
		}
		for knownID := range s.catalogIDs {
			if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(knownID)) {
				return true
			}
		}
	}
	return false
}

func (s *discoveryState) knownCatalogIDs(limit int) []string {
	if s == nil || limit <= 0 {
		return nil
	}
	ids := make([]string, 0, limit)
	for _, id := range s.catalogDetails {
		if id != "" {
			ids = appendUniqueString(ids, id)
			if len(ids) == limit {
				return ids
			}
		}
	}
	for _, id := range sortedBoolKeys(s.catalogIDs) {
		ids = appendUniqueString(ids, id)
		if len(ids) == limit {
			break
		}
	}
	return ids
}

func (r *protocolRuntime) validateCoverageSearches(args map[string]any) ([]string, error) {
	if !r.catalogSearch.enabled() {
		return nil, fmt.Errorf("adaptive catalog coverage is unavailable; use query_catalog with one lexical search")
	}
	if r.state.coverageSearchUsed {
		return nil, fmt.Errorf("adaptive catalog coverage was already used in this agent run; inspect returned card ids or use one focused detail lookup instead")
	}
	for _, name := range []string{"search", "id", "ids", "order_by"} {
		if value, exists := args[name]; exists && value != nil {
			return nil, fmt.Errorf("query_catalog searches is mutually exclusive with %s; remove %s and retry the single coverage batch", name, name)
		}
	}
	raw := anySlice(normalizeValue(args["searches"]))
	if len(raw) < 2 || len(raw) > MaxCatalogCoverageSearches {
		return nil, fmt.Errorf("query_catalog searches requires two or three unique phrases")
	}
	searches := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		phrase, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("query_catalog searches phrases must be strings")
		}
		phrase = strings.Join(strings.Fields(phrase), " ")
		if phrase == "" {
			return nil, fmt.Errorf("query_catalog searches phrases cannot be empty")
		}
		if !utf8.ValidString(phrase) || len([]byte(phrase)) > MaxCatalogCoverageSearchBytes {
			return nil, fmt.Errorf("query_catalog searches phrases must be valid UTF-8 and at most %d bytes", MaxCatalogCoverageSearchBytes)
		}
		key := strings.ToLower(phrase)
		if seen[key] {
			return nil, fmt.Errorf("query_catalog searches phrases must be unique")
		}
		seen[key] = true
		searches = append(searches, phrase)
	}
	return searches, nil
}

func (r *protocolRuntime) ValidateWhereClause(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	action := r.state.startAction("model", "validate_where_clause", args)
	out, err := r.base.ValidateWhereClause(ctx, args)
	r.state.finishAction(action, "validate_where_clause", args, out, err)
	if err == nil {
		r.state.recordValidation(args, out)
	}
	return out, err
}

func (r *protocolRuntime) ExecuteSavedQuery(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	name := stringArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("query name is required")
	}
	if !r.state.savedQueryDetailed(name) {
		err := fmt.Errorf("protocol violation: inspect query_catalog(id: %q) before execute_saved_query", "saved_query:"+name)
		r.state.addViolation("saved_query_detail_required", err.Error(), "execute_saved_query", true, map[string]any{"name": name})
		action := r.state.startAction("model", "execute_saved_query", args)
		r.state.finishAction(action, "execute_saved_query", args, nil, err)
		return nil, err
	}
	executionKey := savedQueryExecutionKey(args)
	if cached, ok := r.state.cachedExecution(executionKey); ok {
		if out, rejected := pendingCachedExecutionRejection(r.state, ""); rejected {
			action := r.state.startAction("model", "execute_saved_query", args)
			r.state.finishAction(action, "execute_saved_query", args, out, nil)
			return out, nil
		}
		out := cachedExecutionResult(r.state, cached)
		r.state.selectCachedExecution("execute_saved_query", args, out)
		r.state.recordRepeatedCall(executionKey)
		action := r.state.startAction("model", "execute_saved_query", args)
		r.state.finishAction(action, "execute_saved_query", args, out, nil)
		return out, nil
	}
	r.state.clearCompletionLatch()
	action := r.state.startAction("model", "execute_saved_query", args)
	out, err := r.base.ExecuteSavedQuery(ctx, args)
	r.state.finishAction(action, "execute_saved_query", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_saved_query", args, out)
		r.state.cacheSuccessfulExecution(executionKey, out)
		if !executionFailed(out) {
			r.state.emptySearchStreak = 0
			r.state.emptyDetailStreak = 0
		}
		// A rejected out-of-order attempt is recoverable inside the same actor
		// run. Once the model has inspected the exact detail and the governed
		// execution succeeds, retain the attempt in the action/evidence trail but
		// do not let its now-satisfied protocol violation poison the final answer.
		r.state.resolveSavedQueryDetailViolation(name)
	}
	return out, err
}

func (r *protocolRuntime) ExecuteGraphQL(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	query := stringArg(args, "query")
	if !r.state.hasCatalogEvidence() {
		err := fmt.Errorf("protocol violation: inspect catalog evidence before execute_graphql")
		r.state.addViolation("raw_graphql_catalog_required", err.Error(), "execute_graphql", true, nil)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
	}
	// The seed and broad catalog lists are ranked hint sets, not schema proof. A
	// model that authors raw GraphQL from either can still guess the target or
	// fields. Require an exact same-run detail lookup, which is also the evidence
	// surfaced as catalog_detail_ids in the protocol response. Fail here while
	// the run can recover and name the governed path.
	if !r.state.hasCatalogDetailEvidence() {
		err := fmt.Errorf("protocol violation: the seed and broad catalog results are not discovery detail; inspect the relevant catalog item with query_catalog({id:\"...\"}) before authoring raw GraphQL.%s", approvedSavedQuerySuffix(r.state))
		r.state.addViolation("raw_graphql_discovery_required", err.Error(), "execute_graphql", true, map[string]any{
			"approved_saved_queries": sortedBoolKeys(r.state.savedQueriesDiscovered),
		})
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
	}
	if writeLikeGraphQL(query) && !r.state.securityRuntimeEvidence {
		err := fmt.Errorf("protocol violation: inspect security/runtime catalog guidance before write-capable or control-plane GraphQL")
		details := map[string]any{"required": []any{"help:security", "help:runtime"}}
		r.state.addViolation("security_runtime_discovery_required", err.Error(), "execute_graphql", true, details)
		next := catalogRepairNext(
			map[string]any{"ids": []any{"help:security", "help:runtime"}},
			"Inspect the exact security and runtime guidance in a discovery-only actor step, then re-author the write from returned evidence.",
		)
		out := recoverableProtocolFailure("security_runtime_discovery_required", err.Error(), "write_prerequisite_detail_required", next, details)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if ContainsMutationOperation(query) {
		annotationFields := annotationMutationInputFields(query, args)
		annotationDefinition := isAnnotationDefinitionMutation(query, annotationFields)
		if annotationFields["tier"] && (r.state.annotationMutated || annotationDefinition) {
			err := fmt.Errorf("protocol violation: an annotation cannot be inserted or edited and have its tier flipped in the same agent run; present the draft as data and wait for user confirmation")
			r.state.addViolation("annotation_tier_confirmation_required", err.Error(), "execute_graphql", true, map[string]any{"root": systemRootArtifacts})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		if isWatchActionReviewMutation(query) && r.state.watchDefinitionMutated {
			err := fmt.Errorf("protocol violation: an autonomous watch action cannot be created or changed and approved in the same agent run; explain the proposed action and wait for user confirmation")
			r.state.addViolation("watch_action_confirmation_required", err.Error(), "execute_graphql", true, map[string]any{"root": systemRootWatch})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		roots := MutationRootFields(query)
		if missing := r.state.missingMutationEvidence(roots); len(missing) != 0 {
			err := fmt.Errorf("protocol violation: gather mutation-shape evidence for %s before executing a mutation: inspect the target table's catalog detail with query_catalog({id:\"table:...\"}), validate_where_clause the target, inspect a mutation_pattern detail row, or use an approved saved mutation", strings.Join(missing, ", "))
			details := map[string]any{"tables": missing}
			r.state.addViolation("mutation_evidence_required", err.Error(), "execute_graphql", true, details)
			next := r.state.mutationEvidenceNext(missing)
			out := recoverableProtocolFailure("mutation_evidence_required", err.Error(), "mutation_shape_detail_required", next, details)
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, out, nil)
			return out, nil
		}
		if requiresWorkflowDetail(roots) && !r.state.hasWorkflowDetailEvidence() {
			err := fmt.Errorf("protocol violation: inspect the workflow detail by id before executing it through %s", systemRootWorkflowExec)
			details := map[string]any{"root": systemRootWorkflowExec}
			r.state.addViolation("workflow_detail_required", err.Error(), "execute_graphql", true, details)
			next := r.state.workflowEvidenceNext()
			out := recoverableProtocolFailure("workflow_detail_required", err.Error(), "workflow_detail_required", next, details)
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, out, nil)
			return out, nil
		}
	}
	queryKey := executionQueryKey(args)
	if r.state.failedQueryKeys[queryKey] {
		out := attachExecutionRecovery(executeResult{Errors: []ErrorInfo{{
			Message: "identical failed GraphQL query retry rejected; re-author the query from live GraphJin catalog/help evidence before executing again",
			Extensions: map[string]any{
				"code":      "duplicate_failed_query",
				"retryable": true,
				"graphjin_repair": map[string]any{
					"kind": "distinct_query_required",
					"next": catalogNext(toolQueryCatalog, "Inspect the real table/column detail or graphql_help({for:\"query\"}), then execute a distinct repaired query."),
				},
			},
		}}}, r.state, "")
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if cached, ok := r.state.cachedExecution(queryKey); ok {
		if out, rejected := pendingCachedExecutionRejection(r.state, query); rejected {
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, out, nil)
			return out, nil
		}
		out := cachedExecutionResult(r.state, cached)
		r.state.selectCachedExecution("execute_graphql", args, out)
		r.state.recordRepeatedCall(queryKey)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	r.state.clearCompletionLatch()
	wasRepairPending := r.state.pendingFailedQueryKey != ""
	action := r.state.startAction("model", "execute_graphql", args)
	out, err := r.base.ExecuteGraphQL(ctx, args)
	if err == nil && executionFailed(out) {
		out = attachExecutionRecovery(out, r.state, query)
		r.state.failedQueryKeys[queryKey] = true
		if wasRepairPending {
			r.state.pendingFailedQueryKey = ""
		} else {
			r.state.pendingFailedQueryKey = queryKey
		}
	} else if err == nil && wasRepairPending {
		r.state.pendingFailedQueryKey = ""
	}
	r.state.finishAction(action, "execute_graphql", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_graphql", args, out)
		r.state.cacheSuccessfulExecution(queryKey, out)
		if !executionFailed(out) {
			r.state.emptySearchStreak = 0
			r.state.emptyDetailStreak = 0
			r.state.resolveSuccessfulExecutionViolations()
		}
		if isWatchDefinitionMutation(query) {
			r.state.watchDefinitionMutated = true
		}
		if isAnnotationDefinitionMutation(query, annotationMutationInputFields(query, args)) {
			r.state.annotationMutated = true
		}
	}
	r.state.rawGraphQL = append(r.state.rawGraphQL, map[string]any{
		"operation":  graphQLOperationKind(query),
		"write_like": writeLikeGraphQL(query),
	})
	return out, err
}

func recoverableProtocolFailure(code, message, kind string, next, details map[string]any) executeResult {
	repair := map[string]any{
		"code": code,
		"kind": kind,
		"next": next,
	}
	if len(details) != 0 {
		repair["details"] = details
	}
	directive := recoveryDirectivePrefix + " follow recovery.next in a discovery-only actor step, then re-author and retry the operation in this run."
	return executeResult{
		Errors: []ErrorInfo{{
			Message: joinRecoveryMessage(message, directive),
			Extensions: map[string]any{
				"code":            code,
				"retryable":       true,
				"graphjin_repair": repair,
			},
		}},
		Recovery: map[string]any{
			"code":        code,
			"kind":        kind,
			"instruction": "Gather the named evidence in a separate actor step so its returned schema and policy details are visible before GraphQL is authored.",
			"next":        next,
		},
	}
}

func catalogRepairNext(args map[string]any, reason string) map[string]any {
	next := catalogNext(toolQueryCatalog, reason)
	next["args"] = args
	return next
}

func (r *protocolRuntime) addNamespace(args map[string]any) {
	if r.namespace == "" || args == nil {
		return
	}
	if _, ok := args["namespace"]; !ok {
		args["namespace"] = r.namespace
	}
}

func (s *discoveryState) startAction(source, tool string, args map[string]any) int {
	action := protocolAction{
		Step:      len(s.actions) + 1,
		Source:    source,
		Tool:      tool,
		Args:      redactArgs(args),
		Status:    "started",
		startedAt: time.Now(),
	}
	s.actions = append(s.actions, action)
	return len(s.actions) - 1
}

func (s *discoveryState) finishAction(index int, tool string, args map[string]any, out any, err error) {
	if index < 0 || index >= len(s.actions) {
		return
	}
	s.actions[index].Status = "ok"
	if err != nil {
		s.actions[index].Status = "error"
		s.actions[index].Error = err.Error()
		s.addGrounding(err.Error())
	}
	s.addGrounding(args, out)
	s.actions[index].Summary = resultSummary(tool, args, out)
	s.emitAction(s.actions[index])
}

// emitAction forwards a completed action to the request observer (progress
// streaming). Observer failures never affect the run.
func (s *discoveryState) emitAction(action protocolAction) {
	if s.observe == nil {
		return
	}
	event := ActionEvent{
		Index:     action.Step,
		Source:    action.Source,
		Tool:      action.Tool,
		Args:      action.Args,
		Status:    action.Status,
		Summary:   action.Summary,
		Error:     action.Error,
		ElapsedMS: time.Since(action.startedAt).Milliseconds(),
	}
	func() {
		defer func() { _ = recover() }()
		s.observe(event)
	}()
}

func (s *discoveryState) recordCatalog(args map[string]any, out any, seed bool) {
	search := stringArg(args, "search")
	if search != "" {
		s.catalogSearches = append(s.catalogSearches, map[string]any{
			"search": search,
			"kind":   stringArg(args, "kind"),
			"where":  normalizeValue(args["where"]),
			"seed":   seed,
		})
	}
	if searches := stringSliceArg(args, "searches"); len(searches) != 0 {
		s.catalogSearches = append(s.catalogSearches, map[string]any{
			"searches": searches,
			"kind":     stringArg(args, "kind"),
			"where":    normalizeValue(args["where"]),
			"coverage": true,
			"seed":     seed,
		})
	}
	ids := returnedCatalogDetailIDs(detailIDsFromArgs(args), out)
	for _, id := range ids {
		s.catalogDetails = appendUniqueString(s.catalogDetails, id)
		s.catalogIDs[id] = true
		if isSecurityRuntimeID(id) {
			s.securityRuntimeEvidence = true
		}
	}
	// Seed search is deliberately broad. Its result set can contain one
	// unrelated saved query among many table, column, help, and workflow cards;
	// that is context, not an unambiguous governed route selected by the model.
	// Only model-driven discovery may arm implicit saved-query execution.
	s.recordCatalogRows(out, !seed)
	if len(ids) != 0 {
		s.recordDetailEvidence(ids, out)
		if s.catalogKindSeen("saved_query") {
			for _, name := range savedQueryNamesFromResult(out) {
				s.markSavedQueryDetailed(name)
			}
			for _, id := range ids {
				if name := savedQueryNameFromID(id); name != "" {
					s.markSavedQueryDetailed(name)
				}
			}
		}
	}
	s.recordNext(out)
}

// returnedCatalogDetailIDs prevents a guessed or stale id from becoming
// protocol evidence merely because the catalog request itself succeeded. Only
// ids actually present in returned detail cards can satisfy detail-before-use
// guards for raw GraphQL, saved queries, workflows, or mutations.
func returnedCatalogDetailIDs(requested []string, out any) []string {
	if len(requested) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(requested))
	for _, id := range requested {
		wanted[id] = true
	}
	ids := make([]string, 0, len(requested))
	for _, card := range catalogCards(out) {
		if id := stringFromMap(card, "id"); wanted[id] {
			ids = appendUniqueString(ids, id)
		}
	}
	return ids
}

// detailIDsFromArgs collects the requested detail ids from the single `id`
// argument and the batched `ids` argument.
func detailIDsFromArgs(args map[string]any) []string {
	var out []string
	if id := stringArg(args, "id"); id != "" {
		out = appendUniqueString(out, id)
	}
	for _, id := range stringSliceArg(args, "ids") {
		out = appendUniqueString(out, id)
	}
	return out
}

// recordDetailEvidence marks per-target evidence established by an id-detail
// lookup: catalog kinds seen in detail, table cards (by table name and by the
// table:<...> id suffix), and workflow cards. Search results never reach here.
func (s *discoveryState) recordDetailEvidence(ids []string, out any) {
	for _, card := range catalogCards(out) {
		kind := strings.ToLower(stringFromMap(card, "kind"))
		if kind == "" {
			continue
		}
		s.detailKinds[kind] = true
		switch kind {
		case "table":
			if table := strings.ToLower(stringFromMap(card, "table_name")); table != "" {
				s.tablesDetailed[table] = true
			}
		case "workflow":
			name := stringFromMap(card, "name")
			if name == "" {
				name = strings.TrimPrefix(stringFromMap(card, "id"), "workflow:")
			}
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				s.workflowsDetailed[name] = true
			}
		case "saved_query":
			name := savedQueryNameFromID(stringFromMap(card, "id"))
			if name == "" {
				name = stringFromMap(card, "name")
			}
			if query := strings.TrimSpace(stringFromMap(card, "graphql_query")); name != "" && query != "" {
				for _, candidate := range savedQueryNameCandidates(name) {
					s.savedQueryGraphQL[candidate] = query
				}
			}
		}
	}
	for _, id := range ids {
		if table := tableNameFromCatalogID(id); table != "" {
			s.tablesDetailed[table] = true
		}
		if strings.HasPrefix(strings.ToLower(id), "workflow:") {
			if name := strings.ToLower(strings.TrimSpace(id[len("workflow:"):])); name != "" {
				s.workflowsDetailed[name] = true
			}
		}
	}
}

// tableNameFromCatalogID extracts the bare table name from a table:<...> catalog
// id such as table:db:public.products (returns "products").
func tableNameFromCatalogID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if !strings.HasPrefix(id, "table:") {
		return ""
	}
	name := id[len("table:"):]
	if idx := strings.LastIndexAny(name, ".:"); idx >= 0 && idx+1 < len(name) {
		name = name[idx+1:]
	}
	return strings.TrimSpace(name)
}

func (s *discoveryState) recordCatalogRows(out any, discoverSavedQueries bool) {
	for _, card := range catalogCards(out) {
		id := stringFromMap(card, "id")
		kind := stringFromMap(card, "kind")
		name := stringFromMap(card, "name")
		if name == "" {
			name = savedQueryNameFromID(id)
		}
		if id != "" {
			s.catalogIDs[id] = true
			if isSecurityRuntimeID(id) {
				s.securityRuntimeEvidence = true
			}
		}
		if kind != "" {
			s.catalogKinds[kind] = true
		}
		if kind == "saved_query" && discoverSavedQueries {
			if name == "" {
				name = strings.TrimPrefix(id, "saved_query:")
			}
			if name != "" {
				s.markSavedQueryDiscovered(name)
			}
		}
		if kind == "system_capability" && strings.Contains(strings.ToLower(id+" "+name+" "+stringFromMap(card, "title")), "gj_security") {
			s.securityRuntimeEvidence = true
		}
		if kind == "system_capability" && strings.Contains(strings.ToLower(id+" "+name+" "+stringFromMap(card, "title")), "gj_runtime") {
			s.securityRuntimeEvidence = true
		}
	}
}

func (s *discoveryState) recordNext(out any) {
	if m := mapValue(out); m != nil {
		if next := m["next"]; next != nil {
			s.suggestedNext = append(s.suggestedNext, normalizeValue(next))
		}
	}
}

func (s *discoveryState) recordValidation(args map[string]any, out any) {
	m := mapValue(out)
	if table := strings.ToLower(stringArg(args, "table")); table != "" {
		// The validation attempt itself is mutation-shape evidence: the model
		// learned the table's columns and operators even when valid=false.
		s.tablesValidated[table] = true
	}
	item := map[string]any{
		"table":    stringArg(args, "table"),
		"database": stringArg(args, "database"),
	}
	if m != nil {
		item["valid"] = m["valid"]
		item["validated_by"] = m["validated_by"]
		if errs, ok := m["errors"].([]any); ok {
			item["error_count"] = len(errs)
		}
		if errs, ok := m["compiler_errors"].([]any); ok {
			item["compiler_error_count"] = len(errs)
		}
	}
	s.validations = append(s.validations, item)
}

func (s *discoveryState) recordExecution(tool string, args map[string]any, out any) {
	item := resultSummary(tool, args, out)
	item["tool"] = tool
	if tool == "execute_saved_query" {
		item["name"] = stringArg(args, "name")
	}
	if tool == "execute_graphql" {
		item["query"] = stringArg(args, "query")
	}
	s.executions = append(s.executions, item)
	if item["has_data"] == true {
		s.lastExecution = map[string]any{
			"tool":   tool,
			"args":   redactArgs(args),
			"result": normalizeValue(out),
		}
	}
}

func executionQueryKey(args map[string]any) string {
	return "execute_graphql:" + stringify(normalizeValue(map[string]any{
		"query":     normalizeGraphQLIdentity(stringArg(args, "query")),
		"variables": args["variables"],
		"namespace": strings.TrimSpace(stringArg(args, "namespace")),
	}))
}

func savedQueryExecutionKey(args map[string]any) string {
	return "execute_saved_query:" + stringify(normalizeValue(map[string]any{
		"name":      strings.ToLower(strings.TrimSpace(stringArg(args, "name"))),
		"variables": args["variables"],
		"namespace": strings.TrimSpace(stringArg(args, "namespace")),
	}))
}

// normalizeGraphQLIdentity is deliberately conservative: whitespace and
// comments outside string values are insignificant in GraphQL, while quoted
// contents must remain byte-for-byte distinct.
func normalizeGraphQLIdentity(query string) string {
	var out strings.Builder
	inString := false
	inBlockString := false
	escaped := false
	pendingSpace := false
	for i := 0; i < len(query); i++ {
		if !inString && i+2 < len(query) && query[i:i+3] == `"""` {
			if pendingSpace && out.Len() != 0 {
				out.WriteByte(' ')
			}
			pendingSpace = false
			inString = true
			inBlockString = true
			out.WriteString(`"""`)
			i += 2
			continue
		}
		if inBlockString && i+2 < len(query) && query[i:i+3] == `"""` {
			out.WriteString(`"""`)
			i += 2
			inString = false
			inBlockString = false
			continue
		}
		ch := query[i]
		if inString {
			out.WriteByte(ch)
			if !inBlockString && ch == '\\' && !escaped {
				escaped = true
				continue
			}
			if !inBlockString && ch == '"' && !escaped {
				inString = false
			}
			escaped = false
			continue
		}
		if ch == '#' {
			for i+1 < len(query) && query[i+1] != '\n' && query[i+1] != '\r' {
				i++
			}
			pendingSpace = true
			continue
		}
		if ch == '"' {
			if pendingSpace && out.Len() != 0 {
				out.WriteByte(' ')
			}
			pendingSpace = false
			inString = true
			out.WriteByte(ch)
			continue
		}
		if unicode.IsSpace(rune(ch)) || ch == ',' {
			pendingSpace = true
			continue
		}
		if pendingSpace && out.Len() != 0 {
			out.WriteByte(' ')
		}
		pendingSpace = false
		out.WriteByte(ch)
	}
	return strings.TrimSpace(out.String())
}

func (s *discoveryState) cachedExecution(key string) (any, bool) {
	if s == nil || key == "" {
		return nil, false
	}
	value, ok := s.successfulExecutions[key]
	return normalizeValue(value), ok
}

func (s *discoveryState) cacheSuccessfulExecution(key string, out any) {
	if s == nil || key == "" || executionFailed(out) || resultSummary("execute_graphql", nil, out)["has_data"] != true {
		return
	}
	s.successfulExecutions[key] = normalizeValue(out)
}

func pendingCachedExecutionRejection(state *discoveryState, query string) (any, bool) {
	if state == nil {
		return nil, false
	}
	requirement := strings.TrimSpace(state.pendingRequiredFinalization())
	if requirement == "" {
		return nil, false
	}
	code, message := pendingFinalProtocol(requirement)
	out := attachExecutionRecovery(executeResult{Errors: []ErrorInfo{{
		Message: "identical execution repeated while " + code + " is unmet; cached rows withheld because they cannot answer this request",
		Extensions: map[string]any{
			"code":      code,
			"retryable": true,
			"graphjin_repair": map[string]any{
				"kind": "distinct_aggregate_required",
				"next": message,
			},
		},
	}}}, state, query)
	return out, true
}

// selectCachedExecution makes the governed evidence selected by the current
// actor step the completion source. Without this, query A -> query B -> cached
// query A leaves the completion binding pointed at query B even though the
// model just reselected A.
func (s *discoveryState) selectCachedExecution(tool string, args map[string]any, out any) {
	if s == nil || executionFailed(out) || resultSummary(tool, args, out)["has_data"] != true {
		return
	}
	s.emptySearchStreak = 0
	s.emptyDetailStreak = 0
	s.lastExecution = map[string]any{
		"tool":   tool,
		"args":   redactArgs(args),
		"result": normalizeValue(out),
	}
}

func cachedExecutionResult(state *discoveryState, cached any) any {
	out := mapValue(cached)
	if out == nil {
		out = map[string]any{"data": normalizeValue(cached)}
	} else {
		out = cloneAnyMap(out)
	}
	out["cached"] = true
	if state != nil {
		if requirement := strings.TrimSpace(state.pendingRequiredFinalization()); requirement != "" {
			code, publicMessage := pendingFinalProtocol(requirement)
			next := "The identical successful execution was not run again. A pending " + code + " requirement still blocks finalization."
			if publicMessage != "" {
				next += " " + publicMessage
			}
			out["recovery"] = map[string]any{
				"code": code,
				"kind": code,
				"next": next,
			}
			return out
		}
	}
	out["recovery"] = map[string]any{
		"code": "completion_required",
		"kind": "completion_required",
		"next": "The identical successful execution was not run again. Call final now using this cached result.data.",
	}
	return out
}

func (s *discoveryState) answerReadyForCompletion() bool {
	return s != nil && s.seedOK && s.modelDiscoveryAction && !s.hasBlockingViolation() &&
		s.lastExecution != nil && s.pendingRequiredFinalization() == ""
}

func (s *discoveryState) recordRepeatedCall(key string) {
	if !s.answerReadyForCompletion() {
		return
	}
	if s.completionLatchKey == "" {
		s.completionLatchKey = key
		return
	}
	s.completionReady = true
}

func (s *discoveryState) clearCompletionLatch() {
	if s == nil {
		return
	}
	s.completionLatchKey = ""
	s.completionReady = false
}

func (s *discoveryState) completionContinuation() string {
	if s == nil || !s.completionReady || !s.answerReadyForCompletion() {
		return ""
	}
	s.completionReady = false
	return `await final("GraphJin duplicate execution recovery completed.", {execution: globalThis.` + runtimeLastExecutionKey + `});`
}

func (s *discoveryState) pendingRequiredFinalization() string {
	if s.pendingFailedQueryKey != "" {
		return "execution_repair_required: the first GraphQL execution failed. Read errors[].extensions.graphjin_repair and result.recovery, then execute one distinct repaired query before finalizing; an identical retry is rejected."
	}
	if message := s.pendingRecoverableExecution(); message != "" {
		return message
	}
	if message := s.pendingRequiredSavedQueryExecution(); message != "" {
		return message
	}
	return s.pendingDatabaseComputation()
}

func (s *discoveryState) pendingRequiredFinalizationContinuation() string {
	if s.pendingFailedQueryKey != "" || s.pendingDatabaseComputation() != "" {
		return ""
	}
	if continuation := s.pendingRecoverableExecutionContinuation(); continuation != "" {
		return continuation
	}
	return s.pendingRequiredSavedQueryContinuation()
}

func (s *discoveryState) pendingRecoverableExecution() string {
	violation, ok := s.primaryRecoverableExecutionViolation()
	if !ok {
		return ""
	}
	if s.recoverableExecutionEvidenceMissing(violation) {
		return "execution_evidence_required: the attempted GraphQL operation is missing required same-run evidence. Follow errors[].extensions.graphjin_repair.next in a discovery-only actor step; after the result is visible, re-author and execute the operation before finalizing."
	}
	return "execution_retry_required: the required evidence is now present, but the rejected GraphQL operation has not been retried successfully. Re-author it from the returned detail and execute it before finalizing."
}

func (s *discoveryState) pendingRecoverableExecutionContinuation() string {
	violation, ok := s.primaryRecoverableExecutionViolation()
	if !ok || !s.recoverableExecutionEvidenceMissing(violation) {
		return ""
	}
	var next map[string]any
	switch violation.Code {
	case "security_runtime_discovery_required":
		next = catalogRepairNext(map[string]any{"ids": []any{"help:security", "help:runtime"}}, "Inspect write prerequisites.")
	case "mutation_evidence_required":
		next = s.mutationEvidenceNext(stringListFromDetails(violation.Details, "tables"))
	case "workflow_detail_required":
		next = s.workflowEvidenceNext()
	default:
		return ""
	}
	args := mapValue(next["args"])
	if stringFromMap(next, "recommended_tool") != toolQueryCatalog || len(args) == 0 {
		return ""
	}
	return `const evidence = await query_catalog(` + stringify(normalizeValue(args)) + `); console.log(evidence);`
}

func (s *discoveryState) primaryRecoverableExecutionViolation() (protocolViolation, bool) {
	if s == nil {
		return protocolViolation{}, false
	}
	var fallback protocolViolation
	hasFallback := false
	for _, code := range []string{"security_runtime_discovery_required", "mutation_evidence_required", "workflow_detail_required"} {
		for _, violation := range s.violations {
			if violation.Blocking && violation.Code == code {
				if s.recoverableExecutionEvidenceMissing(violation) {
					return violation, true
				}
				if !hasFallback {
					fallback = violation
					hasFallback = true
				}
			}
		}
	}
	return fallback, hasFallback
}

func (s *discoveryState) recoverableExecutionEvidenceMissing(violation protocolViolation) bool {
	switch violation.Code {
	case "security_runtime_discovery_required":
		return !s.securityRuntimeEvidence
	case "mutation_evidence_required":
		return len(s.missingMutationEvidence(stringListFromDetails(violation.Details, "tables"))) != 0
	case "workflow_detail_required":
		return !s.hasWorkflowDetailEvidence()
	default:
		return false
	}
}

// pendingRequiredSavedQueryExecution guards explicit, ordered user requests
// such as "then execute_saved_query({name:...})" from a premature executor
// final. It also covers natural live-data requests when discovery produced one
// unambiguous saved-query route. Explanatory/catalog-only requests and
// multi-candidate routes never turn into an implicit execution.
func (s *discoveryState) pendingRequiredSavedQueryExecution() string {
	name, explicit := s.requiredSavedQueryExecution()
	if name == "" || s.hasSuccessfulSavedQueryExecution(name) {
		return ""
	}
	requirement := "the live-data request has one unambiguous discovered saved-query route"
	if explicit {
		requirement = "the user explicitly required this saved-query execution"
	}
	if !s.savedQueryDetailed(name) {
		return fmt.Sprintf("%s. Continue by running this exact JavaScript now: const detail = await query_catalog({id:%q}); const execution = await execute_saved_query({name:%q}); await final({status:\"answered\", answer:\"Answer the user's request only from execution.data.\", data:execution.data, evidence:{saved_query_detail:detail.cards}});", requirement, "saved_query:"+name, name)
	}
	return fmt.Sprintf("%s. The detail is already present; continue by running this exact JavaScript now: const execution = await execute_saved_query({name:%q}); await final({status:\"answered\", answer:\"Answer the user's request only from execution.data.\", data:execution.data});", requirement, name)
}

func (s *discoveryState) pendingRequiredSavedQueryContinuation() string {
	name, _ := s.requiredSavedQueryExecution()
	if name == "" || s.hasSuccessfulSavedQueryExecution(name) {
		return ""
	}
	result := `await final("GraphJin saved-query continuation completed.", {detail, execution});`
	if !s.savedQueryDetailed(name) {
		return fmt.Sprintf(`const detail = await query_catalog({id:%q}); const execution = await execute_saved_query({name:%q}); %s`, "saved_query:"+name, name, result)
	}
	return fmt.Sprintf(`const detail = {cards:[]}; const execution = await execute_saved_query({name:%q}); %s`, name, result)
}

func (s *discoveryState) requiredSavedQueryExecution() (name string, explicit bool) {
	if s == nil {
		return "", false
	}
	name = explicitlyRequiredSavedQueryName(s.instruction)
	explicit = name != ""
	if name == "" {
		name = s.unambiguousSavedQueryForLiveData()
	}
	return name, explicit
}

func (s *discoveryState) unambiguousSavedQueryForLiveData() string {
	if !liveDataIntent(s.instruction) || len(s.savedQueriesDiscovered) != 1 {
		return ""
	}
	for name := range s.savedQueriesDiscovered {
		return name
	}
	return ""
}

func liveDataIntent(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	if lower == "" {
		return false
	}
	for _, phrase := range []string{
		"discovery only", "do not execute", "don't execute", "without executing",
		"inventory the approved saved queries", "inventory approved saved queries",
		"saved queries and workflows", "list saved queries", "list the saved queries",
		"explain the saved query", "explain saved queries",
	} {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	for _, verb := range []string{
		"analyze", "check", "compare", "decide", "determine", "find", "identify",
		"prioritize", "review", "run", "show", "summarize", "triage", "what",
	} {
		if containsWord(lower, verb) {
			return true
		}
	}
	return false
}

func containsWord(value, word string) bool {
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == word {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasSuccessfulSavedQueryExecution(name string) bool {
	for _, execution := range s.executions {
		if strings.EqualFold(stringFromMap(execution, "name"), strings.TrimSpace(name)) && execution["has_data"] == true {
			return true
		}
	}
	return false
}

func explicitlyRequiredSavedQueryName(instruction string) string {
	lower := strings.ToLower(instruction)
	for _, phrase := range []string{"do not execute", "don't execute", "without executing", "discovery only"} {
		if strings.Contains(lower, phrase) {
			return ""
		}
	}
	const marker = "execute_saved_query"
	for searchFrom := 0; searchFrom < len(lower); {
		relative := strings.Index(lower[searchFrom:], marker)
		if relative < 0 {
			return ""
		}
		start := searchFrom + relative
		windowStart := start - 96
		if windowStart < 0 {
			windowStart = 0
		}
		lead := lower[windowStart:start]
		required := strings.Contains(lead, "only after") || strings.Contains(lead, "then") || strings.Contains(lead, "await") || strings.Contains(lead, "must")
		open := start + len(marker)
		for open < len(instruction) && unicode.IsSpace(rune(instruction[open])) {
			open++
		}
		if required && open < len(instruction) && instruction[open] == '(' {
			close := strings.IndexByte(instruction[open+1:], ')')
			if close >= 0 {
				if name := savedQueryNameArgument(instruction[open+1 : open+1+close]); name != "" {
					return name
				}
			}
		}
		searchFrom = start + len(marker)
	}
	return ""
}

func savedQueryNameArgument(arguments string) string {
	lower := strings.ToLower(arguments)
	nameAt := strings.Index(lower, "name")
	if nameAt < 0 {
		return ""
	}
	colon := strings.IndexByte(arguments[nameAt+len("name"):], ':')
	if colon < 0 {
		return ""
	}
	value := strings.TrimSpace(arguments[nameAt+len("name")+colon+1:])
	if len(value) < 2 || (value[0] != '\'' && value[0] != '"') {
		return ""
	}
	quote := value[0]
	end := strings.IndexByte(value[1:], quote)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(value[1 : end+1])
}

func (s *discoveryState) hasCatalogEvidence() bool {
	return s.seedOK || len(s.catalogIDs) != 0 || len(s.catalogSearches) != 0
}

// hasCatalogDetailEvidence is deliberately stricter than modelDiscoveryAction:
// broad kind/table/search listings help the model choose a route, but only an
// explicit id lookup records the exact catalog object used to author raw
// GraphQL and makes that provenance visible in the final protocol evidence.
func (s *discoveryState) hasCatalogDetailEvidence() bool {
	return s != nil && len(s.catalogDetails) != 0
}

// missingMutationEvidence returns the mutation root fields that lack
// mutation-shape evidence in this run. A target table is covered when its table
// card detail was inspected by id, it was passed through validate_where_clause,
// or a mutation_pattern detail was inspected AND the table surfaced in any
// catalog result. Watch roots require help:watches or their exact capability
// detail; other gj_* roots are governed by their dedicated gates. An
// unparseable mutation (no roots) demands generic shape evidence.
func (s *discoveryState) missingMutationEvidence(roots []string) []string {
	if len(roots) == 0 {
		if s.detailKinds["mutation_pattern"] || len(s.tablesDetailed) != 0 || len(s.tablesValidated) != 0 {
			return nil
		}
		return []string{"mutation target"}
	}
	var missing []string
	for _, root := range roots {
		root = strings.ToLower(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		if root == systemRootWatch || root == systemRootWatchEvent {
			if !s.hasWatchMutationEvidence(root) {
				missing = appendUniqueString(missing, root)
			}
			continue
		}
		if strings.HasPrefix(root, "gj_") {
			continue
		}
		if s.tablesDetailed[root] || s.tablesValidated[root] {
			continue
		}
		if s.detailKinds["mutation_pattern"] && s.tableSeenInCatalog(root) {
			continue
		}
		missing = appendUniqueString(missing, root)
	}
	return missing
}

func (s *discoveryState) hasWatchMutationEvidence(root string) bool {
	if s == nil {
		return false
	}
	root = strings.ToLower(strings.TrimSpace(root))
	for _, id := range s.catalogDetails {
		lower := strings.ToLower(strings.TrimSpace(id))
		if lower == "help:watches" || lower == root || strings.HasSuffix(lower, ":"+root) || strings.Contains(lower, ":"+root+".") {
			return true
		}
	}
	return false
}

// tableSeenInCatalog reports whether any catalog result in this run surfaced a
// table:<...> card whose id suffix matches the table name.
func (s *discoveryState) tableSeenInCatalog(table string) bool {
	for id := range s.catalogIDs {
		if tableNameFromCatalogID(id) == table {
			return true
		}
	}
	return false
}

func requiresWorkflowDetail(roots []string) bool {
	for _, root := range roots {
		if strings.EqualFold(strings.TrimSpace(root), systemRootWorkflowExec) {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasWorkflowDetailEvidence() bool {
	return s != nil && (len(s.workflowsDetailed) != 0 || len(s.savedQueriesDetailed) != 0)
}

func (s *discoveryState) mutationEvidenceNext(tables []string) map[string]any {
	ids := make([]any, 0, len(tables))
	for _, table := range tables {
		if table == systemRootWatch || table == systemRootWatchEvent {
			ids = append(ids, "help:watches")
			continue
		}
		id := s.catalogIDForTable(table)
		if id == "" {
			return catalogRepairNext(
				map[string]any{"kind": "table", "limit": 20},
				"Enumerate visible tables, select the exact mutation target, then inspect that table card by id in a discovery-only actor step.",
			)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return catalogRepairNext(
			map[string]any{"kind": "mutation_pattern", "limit": 20},
			"Inspect a mutation pattern or the exact target table detail before retrying the write.",
		)
	}
	return catalogRepairNext(
		map[string]any{"ids": ids},
		"Inspect the exact target table detail and author the mutation only from its returned column and mutation-shape evidence.",
	)
}

func (s *discoveryState) catalogIDForTable(table string) string {
	table = strings.ToLower(strings.TrimSpace(table))
	best := ""
	for id := range s.catalogIDs {
		if tableNameFromCatalogID(id) == table && (best == "" || id < best) {
			best = id
		}
	}
	return best
}

func (s *discoveryState) workflowEvidenceNext() map[string]any {
	workflowIDs := make([]string, 0)
	for id := range s.catalogIDs {
		if strings.HasPrefix(strings.ToLower(id), "workflow:") {
			workflowIDs = append(workflowIDs, id)
		}
	}
	if len(workflowIDs) != 0 {
		sort.Strings(workflowIDs)
		ids := make([]any, len(workflowIDs))
		for i := range workflowIDs {
			ids[i] = workflowIDs[i]
		}
		return catalogRepairNext(
			map[string]any{"ids": ids},
			"Inspect the discovered workflow detail by id before retrying workflow execution.",
		)
	}
	return catalogRepairNext(
		map[string]any{"kind": "workflow", "limit": 20},
		"Enumerate visible workflows, choose the exact workflow, and inspect its detail row before retrying execution.",
	)
}

func (s *discoveryState) catalogKindSeen(kind string) bool {
	return s.catalogKinds[kind]
}

func (s *discoveryState) addViolation(code, message, tool string, blocking bool, details map[string]any) {
	s.violations = append(s.violations, protocolViolation{
		Code:     code,
		Message:  message,
		Tool:     tool,
		Blocking: blocking,
		Details:  details,
	})
}

func (s *discoveryState) resolveSavedQueryDetailViolation(name string) {
	name = strings.TrimSpace(name)
	for i := range s.violations {
		violation := &s.violations[i]
		if violation.Code != "saved_query_detail_required" ||
			!strings.EqualFold(strings.TrimSpace(stringFromMap(violation.Details, "name")), name) {
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

func (s *discoveryState) resolveSuccessfulExecutionViolations() {
	for i := range s.violations {
		violation := &s.violations[i]
		switch violation.Code {
		case "raw_graphql_catalog_required", "raw_graphql_discovery_required", "security_runtime_discovery_required", "mutation_evidence_required", "workflow_detail_required":
		default:
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

func (s *discoveryState) finalize(resp Response) Response {
	if resp.Status == "" {
		resp.Status = StatusAnswered
	}
	if s.hasPolicyFinalBlockingViolation() {
		resp = blockResponse(resp)
	}
	if resp.Status == StatusError && s.hasBlockingViolation() {
		resp = blockResponse(resp)
	}
	resp = s.recoverResolvedTerminalRefusalResponse(resp)
	resp = s.recoverCompletionLatchResponse(resp)
	resp = s.recoverLostExecutionResponse(resp)
	resp = s.recoverLostHistoryResponse(resp)
	if resp.Status == StatusAnswered {
		switch {
		case !s.seedOK:
			s.addViolation("catalog_seed_required", "agent answered without successful initial catalog discovery", "", true, nil)
			resp = blockResponse(resp)
		case !s.modelDiscoveryAction:
			s.addViolation("model_discovery_required", "agent answered without a model-driven catalog/help/detail discovery action", "", true, nil)
			resp = blockResponse(resp)
		default:
			if tokens := s.ungroundedAnswerTokens(resp.Answer); len(tokens) != 0 {
				s.addViolation("ungrounded_answer_fields",
					fmt.Sprintf("answer cites field-like identifiers absent from this run's tool evidence: %s; answer only from fields observed in execution results and state derived values in plain language", strings.Join(tokens, ", ")),
					"", true, map[string]any{"tokens": tokens})
			}
			if s.hasBlockingViolation() {
				resp = blockResponse(resp)
			}
		}
	}
	resp.Actions = s.actionValues()
	resp.Evidence = s.mergeEvidence(resp.Evidence)
	if resp.Next == nil {
		resp.Next = s.nextValue()
	}
	if resp.Status == StatusBlocked {
		for _, violation := range s.violations {
			if violation.Blocking {
				resp.Errors = appendProtocolError(resp.Errors, violation)
			}
		}
		if resp.Refusal == nil {
			resp.Refusal = s.buildRefusal(resp)
		}
	} else {
		resp.Refusal = nil
	}
	return resp
}

// recoverResolvedTerminalRefusalResponse removes a stale model refusal only
// when this same run subsequently satisfied that exact protocol requirement
// and its final tool action was a successful data-bearing execution. Policy
// refusals and any still-blocking requirement remain untouched.
func (s *discoveryState) recoverResolvedTerminalRefusalResponse(resp Response) Response {
	if s == nil || resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.PolicyFinal ||
		strings.TrimSpace(resp.Answer) == "" || s.hasBlockingViolation() ||
		s.pendingRequiredFinalization() != "" || !s.terminalExecutionSucceeded() ||
		!s.hasResolvedViolation(resp.Refusal.Code) {
		return resp
	}
	for _, item := range resp.Errors {
		code := stringFromMap(item.Extensions, "code")
		if code != "" && !s.hasResolvedViolation(code) {
			return resp
		}
	}
	resp.Status = StatusAnswered
	resp.Refusal = nil
	resp.Errors = nil
	resp.Next = nil
	return resp
}

func (s *discoveryState) terminalExecutionSucceeded() bool {
	if len(s.actions) == 0 {
		return false
	}
	action := s.actions[len(s.actions)-1]
	return action.Status == "ok" &&
		(action.Tool == toolExecuteGraphQL || action.Tool == toolExecuteSavedQuery) &&
		action.Summary["has_data"] == true && executionErrorCount(action.Summary["error_count"]) == 0
}

func (s *discoveryState) hasResolvedViolation(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	for _, violation := range s.violations {
		if violation.Code == code && !violation.Blocking && mapValue(violation.Details)["resolved"] == true {
			return true
		}
	}
	return false
}

// recoverCompletionLatchResponse is the hard-stop safety net for a model that
// used its final actor step to repeat an already-successful execution. The
// cached result has already passed the same discovery, safety, repair, and
// database-computation guards required by the runtime auto-final path.
func (s *discoveryState) recoverCompletionLatchResponse(resp Response) Response {
	if s == nil || resp.Status != StatusError || s.completionLatchKey == "" ||
		!onlyActorLoopError(resp.Errors) || !s.answerReadyForCompletion() {
		return resp
	}
	data, ok := s.lastExecutionData()
	if !ok {
		return resp
	}
	resp.Status = StatusAnswered
	resp.Answer = "The governed GraphJin execution completed successfully. Returned data:\n\n" + executionDataExcerpt(data, 8*1024)
	resp.Data = data
	resp.Errors = nil
	resp.Refusal = nil
	resp.Next = nil
	return resp
}

// recoverLostHistoryResponse handles a narrow RLM terminal failure: the user
// explicitly asks to repeat a prior answered turn, this run re-established
// model-driven catalog evidence, but the actor loops before forwarding the
// already-loaded trail to the responder. Prior turns remain context rather
// than protocol evidence; this only returns their content verbatim and never
// authorizes a query or side effect.
func (s *discoveryState) recoverLostHistoryResponse(resp Response) Response {
	if s == nil || resp.Status != StatusError || !s.seedOK || !s.modelDiscoveryAction ||
		s.hasBlockingViolation() || resp.Refusal != nil || !explicitHistoryRepeatRequest(s.instruction) ||
		!onlyActorLoopError(resp.Errors) {
		return resp
	}
	for i := len(s.history) - 1; i >= 0; i-- {
		turn := s.history[i]
		if !strings.EqualFold(strings.TrimSpace(turn.Role), "assistant") || strings.TrimSpace(turn.Content) == "" {
			continue
		}
		if status := strings.TrimSpace(turn.Status); status != "" && status != StatusAnswered {
			continue
		}
		resp.Status = StatusAnswered
		resp.Answer = strings.TrimSpace(turn.Content)
		resp.Errors = nil
		resp.Refusal = nil
		resp.Next = nil
		return resp
	}
	return resp
}

func explicitHistoryRepeatRequest(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	repeat := strings.Contains(lower, "repeat") || strings.Contains(lower, "restate") || strings.Contains(lower, "echo")
	if !repeat {
		return false
	}
	for _, phrase := range []string{"prior", "previous", "earlier", "recent trail", "task trail", "history", "last turn", "prior agent run"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func onlyActorLoopError(errors []ErrorInfo) bool {
	if len(errors) != 1 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(stringFromMap(errors[0].Extensions, "code")), "agent_actor_steps_exhausted") {
		return true
	}
	return strings.Contains(strings.ToLower(errors[0].Message), "actor loop exceeded")
}

// recoverLostExecutionResponse repairs one internally contradictory model
// response: this run completed a governed execution with data, yet the final
// actor claims that no result was provided. The protocol only takes over when
// every discovery/policy guard is satisfied and the response carries no
// structured refusal or error. This keeps real empty/error/access outcomes
// under the normal refusal path.
func (s *discoveryState) recoverLostExecutionResponse(resp Response) Response {
	if s == nil || !s.seedOK || !s.modelDiscoveryAction || s.hasBlockingViolation() ||
		len(resp.Errors) != 0 || resp.Refusal != nil || !lostExecutionEvidenceClaim(resp.Answer) {
		return resp
	}
	switch resp.Status {
	case StatusBlocked, StatusError, StatusNeedsClarification:
	default:
		return resp
	}
	data, ok := s.lastExecutionData()
	if !ok {
		return resp
	}
	resp.Status = StatusAnswered
	resp.Answer = "The governed GraphJin execution completed successfully. Returned data:\n\n" + executionDataExcerpt(data, 8*1024)
	resp.Data = data
	resp.Errors = nil
	resp.Refusal = nil
	resp.Next = nil
	return resp
}

func (s *discoveryState) lastExecutionData() (any, bool) {
	execution := mapValue(s.lastExecution)
	result := mapValue(execution["result"])
	if result == nil {
		return nil, false
	}
	data, ok := result["data"]
	return normalizeValue(data), ok && data != nil
}

func lostExecutionEvidenceClaim(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return false
	}
	plain := strings.Join(strings.FieldsFunc(answer, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
	for _, phrase := range []string{
		"no result data",
		"no query result",
		"no execution result",
		"did not receive the result",
		"result was not provided",
		"results were not provided",
		"data was not provided",
		"unable to access the result",
		"cannot access the result",
	} {
		if strings.Contains(plain, phrase) {
			return true
		}
	}
	return strings.Contains(answer, "didn't receive the result")
}

func executionDataExcerpt(data any, maxBytes int) string {
	value := stringify(normalizeValue(data))
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

func blockResponse(resp Response) Response {
	resp.Status = StatusBlocked
	if strings.TrimSpace(resp.Answer) == "" {
		resp.Answer = "I could not answer because the required GraphJin discovery protocol was not completed."
	}
	return resp
}

func (s *discoveryState) hasBlockingViolation() bool {
	for _, violation := range s.violations {
		if violation.Blocking {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasPolicyFinalBlockingViolation() bool {
	for _, violation := range s.violations {
		if violation.Blocking && policyFinalViolation(violation.Code) {
			return true
		}
	}
	return false
}

func (s *discoveryState) actionValues() any {
	out := make([]any, len(s.actions))
	for i, action := range s.actions {
		out[i] = action
	}
	return out
}

func (s *discoveryState) mergeEvidence(model any) any {
	protocol := map[string]any{
		"seed": map[string]any{
			"ok":          s.seedOK,
			"instruction": s.instruction,
			"result":      s.seedResult,
		},
		"help_topics":              s.helpTopics,
		"catalog_searches":         s.catalogSearches,
		"catalog_detail_ids":       s.catalogDetails,
		"catalog_ids":              sortedBoolKeys(s.catalogIDs),
		"catalog_kinds":            sortedBoolKeys(s.catalogKinds),
		"saved_queries_discovered": sortedBoolKeys(s.savedQueriesDiscovered),
		"saved_queries_detailed":   sortedBoolKeys(s.savedQueriesDetailed),
		"tables_detailed":          sortedBoolKeys(s.tablesDetailed),
		"tables_validated":         sortedBoolKeys(s.tablesValidated),
		"workflows_detailed":       sortedBoolKeys(s.workflowsDetailed),
		"validations":              s.validations,
		"executions":               s.executions,
		"raw_graphql":              s.rawGraphQL,
		"suggested_next":           s.suggestedNext,
		"violations":               s.violations,
	}
	if model == nil {
		return protocol
	}
	return map[string]any{
		"protocol": protocol,
		"model":    model,
	}
}

func (s *discoveryState) nextValue() any {
	if len(s.suggestedNext) == 0 {
		return nil
	}
	return s.suggestedNext[len(s.suggestedNext)-1]
}

func appendProtocolError(errors []ErrorInfo, violation protocolViolation) []ErrorInfo {
	for _, err := range errors {
		if err.Message == violation.Message {
			return errors
		}
	}
	return append(errors, ErrorInfo{
		Message: violation.Message,
		Extensions: map[string]any{
			"code":     violation.Code,
			"tool":     violation.Tool,
			"details":  violation.Details,
			"protocol": "graphjin_discovery",
		},
	})
}

func redactArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range args {
		switch key {
		case "variables", "headers", "context":
			out[key] = "[redacted]"
		case "query":
			out[key] = truncateString(fmt.Sprint(value), actionQueryTraceLimit)
		default:
			out[key] = normalizeValue(value)
		}
	}
	return out
}

func resultSummary(tool string, args map[string]any, out any) map[string]any {
	summary := map[string]any{}
	addStructuredResultSummary(summary, out)
	if tool == "query_catalog" || tool == "graphql_help" {
		cards := catalogCards(out)
		summary["card_count"] = len(cards)
		ids := make([]string, 0, len(cards))
		kinds := map[string]bool{}
		for _, card := range cards {
			if id := stringFromMap(card, "id"); id != "" {
				ids = append(ids, id)
			}
			if kind := stringFromMap(card, "kind"); kind != "" {
				kinds[kind] = true
			}
		}
		if len(ids) != 0 {
			summary["catalog_ids"] = ids
		}
		if len(kinds) != 0 {
			summary["catalog_kinds"] = sortedBoolKeys(kinds)
		}
		if result := mapValue(out); result != nil {
			if retrieval := mapValue(result["retrieval"]); retrieval != nil {
				for _, key := range []string{"mode", "exact_match", "semantic_candidate_count", "visible_table_endpoints", "relationship_path_count", "lexical_fallback"} {
					if value, exists := retrieval[key]; exists {
						summary[key] = value
					}
				}
			}
			if coverage := anySlice(result["coverage"]); len(coverage) != 0 {
				summary["coverage_phrase_count"] = len(coverage)
			}
		}
		return summary
	}
	if tool == "execute_saved_query" || tool == "execute_graphql" {
		m := mapValue(out)
		if m != nil {
			if m["cached"] == true {
				summary["cached"] = true
			}
			if data, ok := m["data"]; ok && data != nil {
				summary["has_data"] = true
				summary["data_shape"] = dataShape(data)
				summary["database_aggregate"] = resultContainsAggregateField(data)
			}
			if trunc := mapValue(m["truncation"]); trunc != nil {
				summary["truncated"] = trunc["roots"]
			}
		}
		return summary
	}
	if tool == "validate_where_clause" {
		m := mapValue(out)
		if m != nil {
			summary["valid"] = m["valid"]
			summary["table"] = m["table"]
			summary["validated_by"] = m["validated_by"]
		}
		return summary
	}
	return summary
}

func addStructuredResultSummary(summary map[string]any, out any) {
	m := mapValue(out)
	if m == nil {
		return
	}
	errorCodes := map[string]bool{}
	recoveryCodes := map[string]bool{}
	if errs := anySlice(m["errors"]); len(errs) != 0 {
		summary["error_count"] = len(errs)
		for _, item := range errs {
			extensions := mapValue(mapValue(item)["extensions"])
			if code := stringFromMap(extensions, "code"); code != "" {
				errorCodes[code] = true
			}
			if repair := mapValue(extensions["graphjin_repair"]); repair != nil {
				if code := stringFromMap(repair, "code"); code != "" {
					recoveryCodes[code] = true
				}
				if kind := stringFromMap(repair, "kind"); kind != "" {
					recoveryCodes[kind] = true
				}
			}
		}
	}
	if recovery := mapValue(m["recovery"]); recovery != nil {
		if code := stringFromMap(recovery, "code"); code != "" {
			recoveryCodes[code] = true
		}
		if kind := stringFromMap(recovery, "kind"); kind != "" {
			recoveryCodes[kind] = true
		}
		if next := mapValue(recovery["next"]); next != nil {
			tool := stringFromMap(next, "tool")
			if tool == "" {
				tool = stringFromMap(next, "recommended_tool")
			}
			if tool != "" {
				summary["recovery_tool"] = tool
			}
		}
	}
	if len(errorCodes) != 0 {
		summary["error_codes"] = sortedBoolKeys(errorCodes)
	}
	if len(recoveryCodes) != 0 {
		summary["recovery_codes"] = sortedBoolKeys(recoveryCodes)
	}
}

func catalogCards(out any) []map[string]any {
	m := mapValue(out)
	if m == nil {
		return nil
	}
	items := anySlice(m["cards"])
	if len(items) == 0 {
		items = anySlice(m["catalog_rows"])
	}
	cards := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if card := mapValue(item); card != nil {
			cards = append(cards, card)
		}
	}
	return cards
}

func savedQueryNamesFromResult(out any) []string {
	var names []string
	for _, card := range catalogCards(out) {
		if stringFromMap(card, "kind") != "saved_query" {
			continue
		}
		name := stringFromMap(card, "name")
		if name == "" {
			name = savedQueryNameFromID(stringFromMap(card, "id"))
		}
		if name != "" {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

func mapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	if m, ok := normalizeValue(value).(map[string]any); ok {
		return m
	}
	return nil
}

func catalogFacetDigest(value any) map[string]any {
	result := mapValue(value)
	if result == nil {
		return nil
	}
	return mapValue(result["facets"])
}

func anySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	default:
		if out, ok := normalizeValue(value).([]any); ok {
			return out
		}
		return nil
	}
}

func stringFromMap(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func canonicalSavedQueryName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(name, "saved_query:"))
}

func (s *discoveryState) markSavedQueryDiscovered(name string) {
	for _, candidate := range savedQueryNameCandidates(name) {
		s.savedQueriesDiscovered[candidate] = true
	}
}

func (s *discoveryState) markSavedQueryDetailed(name string) {
	for _, candidate := range savedQueryNameCandidates(name) {
		s.savedQueriesDetailed[candidate] = true
	}
}

func (s *discoveryState) savedQueryDetailed(name string) bool {
	for _, candidate := range savedQueryNameCandidates(name) {
		if s.savedQueriesDetailed[candidate] {
			return true
		}
	}
	return false
}

func (s *discoveryState) savedQueryGraphQLFor(name string) string {
	for _, candidate := range savedQueryNameCandidates(name) {
		if query := strings.TrimSpace(s.savedQueryGraphQL[candidate]); query != "" {
			return query
		}
	}
	return ""
}

func savedQueryNameCandidates(name string) []string {
	name = canonicalSavedQueryName(name)
	if name == "" {
		return nil
	}
	out := []string{name}
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx+1 < len(name) {
		out = append(out, name[idx+1:])
	}
	return out
}

func savedQueryNameFromID(id string) string {
	if strings.HasPrefix(id, "saved_query:") {
		return strings.TrimSpace(strings.TrimPrefix(id, "saved_query:"))
	}
	return ""
}

func isSecurityRuntimeID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return id == "help:security" || id == "help:runtime" || strings.Contains(id, "gj_security") || strings.Contains(id, "gj_runtime")
}

func writeLikeGraphQL(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) ||
		strings.Contains(lower, "gj_config") ||
		strings.Contains(lower, "gj_workflow") ||
		strings.Contains(lower, "gj_code") ||
		strings.Contains(lower, "gj_watch")
}

func isWatchActionReviewMutation(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) &&
		strings.Contains(lower, "gj_watch") &&
		strings.Contains(lower, "action_review_json")
}

func isWatchDefinitionMutation(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) &&
		strings.Contains(lower, "gj_watch") &&
		!strings.Contains(lower, "flow_review_json") &&
		!strings.Contains(lower, "action_review_json")
}

func isAnnotationDefinitionMutation(query string, fields map[string]bool) bool {
	lower := strings.ToLower(query)
	if !ContainsMutationOperation(query) || !strings.Contains(lower, "gj_artifacts") {
		return false
	}
	if fields["target_ref"] || fields["task_id"] || fields["metadata_json"] {
		return true
	}
	if fields["content"] {
		return fields["kind"] || fields["_annotation_id"] || strings.Contains(lower, "annotation:")
	}
	return false
}

// annotationMutationInputFields inspects only gj_artifacts insert/update input
// objects. Selection-set fields and string contents do not count, and a
// variable-backed input contributes the keys of the referenced variable.
func annotationMutationInputFields(query string, args map[string]any) map[string]bool {
	fields := make(map[string]bool)
	if !ContainsMutationOperation(query) {
		return fields
	}
	clean := graphQLStructure(query)
	if containsAnnotationID(args["variables"]) {
		fields["_annotation_id"] = true
	}
	for start := 0; start < len(clean); {
		index := strings.Index(strings.ToLower(clean[start:]), "gj_artifacts")
		if index < 0 {
			break
		}
		index += start
		endName := index + len("gj_artifacts")
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (endName < len(clean) && isGraphQLNameContinue(clean[endName])) {
			start = endName
			continue
		}
		open := skipGraphQLSpace(clean, endName)
		if open >= len(clean) || clean[open] != '(' {
			start = endName
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '(', ')')
		if close < 0 {
			break
		}
		collectAnnotationArgumentFields(clean[open+1:close], args, fields)
		start = close + 1
	}
	return fields
}

func containsAnnotationID(value any) bool {
	switch typed := normalizeValue(value).(type) {
	case string:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "annotation:")
	case map[string]any:
		for _, item := range typed {
			if containsAnnotationID(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsAnnotationID(item) {
				return true
			}
		}
	}
	return false
}

func collectAnnotationArgumentFields(arguments string, args map[string]any, fields map[string]bool) {
	for index := 0; index < len(arguments); {
		index = skipGraphQLSpace(arguments, index)
		if index >= len(arguments) {
			return
		}
		if !isGraphQLNameStart(arguments[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(arguments) && isGraphQLNameContinue(arguments[index]) {
			index++
		}
		argument := strings.ToLower(arguments[start:index])
		colon := skipGraphQLSpace(arguments, index)
		if colon >= len(arguments) || arguments[colon] != ':' || (argument != "insert" && argument != "update") {
			index = colon
			continue
		}
		value := skipGraphQLSpace(arguments, colon+1)
		if value < len(arguments) && arguments[value] == '{' {
			close := matchingGraphQLDelimiter(arguments, value, '{', '}')
			if close < 0 {
				return
			}
			collectGraphQLObjectKeys(arguments[value+1:close], fields)
			index = close + 1
			continue
		}
		if value < len(arguments) && arguments[value] == '$' {
			nameStart := value + 1
			nameEnd := nameStart
			for nameEnd < len(arguments) && isGraphQLNameContinue(arguments[nameEnd]) {
				nameEnd++
			}
			collectVariableObjectKeys(args, arguments[nameStart:nameEnd], fields)
			index = nameEnd
			continue
		}
		index = value + 1
	}
}

func collectGraphQLObjectKeys(object string, fields map[string]bool) {
	for index := 0; index < len(object); {
		if !isGraphQLNameStart(object[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(object) && isGraphQLNameContinue(object[index]) {
			index++
		}
		if colon := skipGraphQLSpace(object, index); colon < len(object) && object[colon] == ':' {
			fields[strings.ToLower(object[start:index])] = true
		}
	}
}

func collectVariableObjectKeys(args map[string]any, name string, fields map[string]bool) {
	variables, _ := normalizeValue(args["variables"]).(map[string]any)
	input, _ := variables[name].(map[string]any)
	for key := range input {
		fields[strings.ToLower(strings.TrimSpace(key))] = true
	}
}

func graphQLStructure(query string) string {
	data := []byte(query)
	for index := 0; index < len(data); {
		switch data[index] {
		case '#':
			for index < len(data) && data[index] != '\n' {
				data[index] = ' '
				index++
			}
		case '"':
			end := skipGraphQLString(query, index)
			for position := index; position < end; position++ {
				data[position] = ' '
			}
			index = end
		default:
			index++
		}
	}
	return string(data)
}

func skipGraphQLSpace(value string, index int) int {
	for index < len(value) {
		switch value[index] {
		case ' ', '\t', '\n', '\r', ',':
			index++
		default:
			return index
		}
	}
	return index
}

func matchingGraphQLDelimiter(value string, start int, open, close byte) int {
	depth := 0
	for index := start; index < len(value); index++ {
		switch value[index] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func graphQLOperationKind(query string) string {
	if ContainsMutationOperation(query) {
		return "mutation"
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	if strings.HasPrefix(lower, "subscription") {
		return "subscription"
	}
	return "query"
}

func dataShape(value any) any {
	switch v := normalizeValue(value).(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return map[string]any{"type": "object", "keys": keys}
	case []any:
		return map[string]any{"type": "array", "length": len(v)}
	default:
		return map[string]any{"type": fmt.Sprintf("%T", value)}
	}
}

func truncateString(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
