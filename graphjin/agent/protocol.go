package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const protocolContextKey = "_graphjin_discovery"

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
	securityRuntimeEvidence bool
	watchDefinitionMutated  bool
	annotationMutated       bool
	// Per-target mutation evidence, populated only by id-detail lookups and
	// validations in THIS run (search hits never count, mirroring the
	// saved-query detail rule).
	detailKinds        map[string]bool
	tablesDetailed     map[string]bool
	tablesValidated    map[string]bool
	workflowsDetailed  map[string]bool
	actions            []protocolAction
	helpTopics         []string
	catalogSearches    []map[string]any
	catalogDetails     []string
	suggestedNext      []any
	validations        []map[string]any
	executions         []map[string]any
	rawGraphQL         []map[string]any
	violations         []protocolViolation
	capabilities       *CapabilityProfile
	observe            func(ActionEvent)
	coverageSearchUsed bool
	// groundingCorpus accumulates this run's observed evidence (instruction,
	// history, tool arguments, tool results) for the answer-grounding check.
	groundingCorpus   strings.Builder
	groundingOverflow bool
	// savedQuerySupplement* cache the one approved-saved-query lookup performed
	// on the agent's behalf when discovery did not surface the approved path.
	savedQuerySupplementDone  bool
	savedQuerySupplementCards []map[string]any
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
	return &protocolRuntime{
		base:          base,
		namespace:     namespace,
		seedLimit:     seedLimit,
		state:         state,
		catalogSearch: catalogSearch,
	}
}

func newDiscoveryState(instruction string) *discoveryState {
	state := &discoveryState{
		instruction:            strings.TrimSpace(instruction),
		catalogIDs:             map[string]bool{},
		catalogKinds:           map[string]bool{},
		savedQueriesDiscovered: map[string]bool{},
		savedQueriesDetailed:   map[string]bool{},
		detailKinds:            map[string]bool{},
		tablesDetailed:         map[string]bool{},
		tablesValidated:        map[string]bool{},
		workflowsDetailed:      map[string]bool{},
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
	// A business-language instruction ranks tables, columns, and relationships
	// above saved queries, so the approved path is often absent from the seed
	// entirely. An agent cannot prefer a saved query it was never shown: it
	// authors raw GraphQL, guesses field names, and reports the schema as
	// broken. Surface the approved inventory before the model's first step.
	// The seed's own search is untouched — this is a separate, lexical
	// saved-query lookup, never a coverage expansion — and detail inspection
	// is still required before any saved query can execute.
	out = r.appendSavedQuerySupplement(ctx, out)
	r.state.seedResult = normalizeValue(out)
	return r.state.seedResult, nil
}

// appendSavedQuerySupplement merges visible approved saved-query cards into a
// seed result that surfaced none. Failures are non-fatal: the seed stands on
// its own and the model can still discover saved queries itself.
func (r *protocolRuntime) appendSavedQuerySupplement(ctx context.Context, seed any) any {
	if r.state.catalogKindSeen("saved_query") {
		return seed
	}
	cards := r.lookupSavedQueryCards(ctx)
	if len(cards) == 0 {
		return seed
	}
	result := mapValue(seed)
	if result == nil {
		return seed
	}
	merged := make(map[string]any, len(result)+1)
	for key, value := range result {
		merged[key] = value
	}
	existing := anySlice(merged["cards"])
	supplement := make([]any, 0, len(cards))
	for _, card := range cards {
		supplement = append(supplement, card)
	}
	merged["cards"] = append(append([]any{}, existing...), supplement...)
	merged["count"] = len(existing) + len(supplement)
	merged["approved_saved_queries"] = map[string]any{
		"names": sortedBoolKeys(r.state.savedQueriesDiscovered),
		"usage": "Governed, pre-approved queries for this data — a shortcut when one matches the request: query_catalog({id:\"saved_query:<name>\"}), then execute_saved_query({name:\"<name>\"}), then answer from result.data. For anything they do not cover, author GraphQL dynamically from inspected catalog detail: real column names only, then validate_where_clause before filtering.",
	}
	return merged
}

// lookupSavedQueryCards asks the catalog for approved saved queries relevant to
// the instruction, falling back to the visible inventory when the ranked search
// returns nothing. It runs at most once per run, only after a failed execution,
// so the single-seed contract and the semantic coverage budget are untouched.
// Results are recorded as evidence but never count as the model's own discovery
// action or as saved-query detail evidence.
func (r *protocolRuntime) lookupSavedQueryCards(ctx context.Context) []map[string]any {
	if r.state.savedQuerySupplementDone {
		return r.state.savedQuerySupplementCards
	}
	r.state.savedQuerySupplementDone = true
	attempts := []map[string]any{
		{"kind": "saved_query", "search": r.state.instruction, "limit": maxRecoverySavedQueries},
		{"kind": "saved_query", "limit": maxRecoverySavedQueries},
	}
	for _, args := range attempts {
		r.addNamespace(args)
		action := r.state.startAction("recovery", "query_catalog", args)
		out, err := r.base.QueryCatalog(ctx, args)
		r.state.finishAction(action, "query_catalog", args, out, err)
		if err != nil {
			continue
		}
		r.state.recordCatalog(args, out, false)
		cards := catalogCards(out)
		saved := make([]map[string]any, 0, len(cards))
		for _, card := range cards {
			if !strings.EqualFold(stringFromMap(card, "kind"), "saved_query") {
				continue
			}
			// Only executable read queries belong in the recommended set:
			// execute_saved_query cannot run subscriptions (watch-registered
			// entries), and unprompted mutations are never a recommendation.
			if op := savedQueryCardOperation(card); op != "" && op != "query" {
				continue
			}
			saved = append(saved, card)
		}
		if len(saved) != 0 {
			r.state.savedQuerySupplementCards = saved
			return saved
		}
	}
	return nil
}

// savedQueryCardOperation extracts the saved query's operation kind from its
// catalog card safety metadata, tolerating both object and JSON-string shapes.
// An empty result means the card did not declare an operation.
func savedQueryCardOperation(card map[string]any) string {
	safety := mapValue(card["safety_json"])
	if safety == nil {
		var parsed any
		if err := json.Unmarshal([]byte(stringFromMap(card, "safety_json")), &parsed); err == nil {
			safety = mapValue(parsed)
		}
	}
	if safety == nil {
		return ""
	}
	return strings.ToLower(stringFromMap(safety, "operation"))
}

func (r *protocolRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	action := r.state.startAction("model", "graphql_help", args)
	out, err := r.base.GraphQLHelp(ctx, args)
	r.state.finishAction(action, "graphql_help", args, out, err)
	if err == nil {
		r.state.modelDiscoveryAction = true
		topic := stringArg(args, "for")
		if topic == "" {
			topic = "discovery"
		}
		r.state.helpTopics = appendUniqueString(r.state.helpTopics, topic)
		r.state.recordCatalogRows(out)
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
	action := r.state.startAction("model", "query_catalog", args)
	out, err := r.base.QueryCatalog(ctx, args)
	r.state.finishAction(action, "query_catalog", args, out, err)
	if err == nil {
		r.state.modelDiscoveryAction = true
		r.state.recordCatalog(args, out, false)
	}
	return out, err
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
	action := r.state.startAction("model", "execute_saved_query", args)
	out, err := r.base.ExecuteSavedQuery(ctx, args)
	r.state.finishAction(action, "execute_saved_query", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_saved_query", args, out)
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
	// The seed is a ranked hint list, not discovery. A model that authors raw
	// GraphQL straight off the seed guesses field names and misses the approved
	// saved query entirely — and finalize would reject that answer anyway. Fail
	// here instead, while the run can still recover, and name the governed path.
	if !r.state.modelDiscoveryAction {
		r.lookupSavedQueryCards(ctx)
		err := fmt.Errorf("protocol violation: the seed is a ranked hint list, not discovery; inspect catalog detail with query_catalog({id:\"...\"}) before authoring raw GraphQL.%s", approvedSavedQuerySuffix(r.state))
		r.state.addViolation("raw_graphql_discovery_required", err.Error(), "execute_graphql", true, map[string]any{
			"approved_saved_queries": sortedBoolKeys(r.state.savedQueriesDiscovered),
		})
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
	}
	if writeLikeGraphQL(query) && !r.state.securityRuntimeEvidence {
		err := fmt.Errorf("protocol violation: inspect security/runtime catalog guidance before write-capable or control-plane GraphQL")
		r.state.addViolation("security_runtime_discovery_required", err.Error(), "execute_graphql", true, map[string]any{"required": []any{"help:security", "help:runtime"}})
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
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
			r.state.addViolation("mutation_evidence_required", err.Error(), "execute_graphql", true, map[string]any{"tables": missing})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		if requiresWorkflowDetail(roots) && !r.state.hasWorkflowDetailEvidence() {
			err := fmt.Errorf("protocol violation: inspect the workflow detail by id before executing it through %s", systemRootWorkflowExec)
			r.state.addViolation("workflow_detail_required", err.Error(), "execute_graphql", true, map[string]any{"root": systemRootWorkflowExec})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
	}
	action := r.state.startAction("model", "execute_graphql", args)
	out, err := r.base.ExecuteGraphQL(ctx, args)
	if err == nil && executionFailed(out) {
		// The model authored a query the live schema rejected. Perform the
		// approved-path discovery it skipped so recovery guidance names real
		// saved queries instead of merely asking it to look again.
		r.lookupSavedQueryCards(ctx)
		out = attachExecutionRecovery(out, r.state)
	}
	r.state.finishAction(action, "execute_graphql", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_graphql", args, out)
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
	ids := detailIDsFromArgs(args)
	for _, id := range ids {
		s.catalogDetails = appendUniqueString(s.catalogDetails, id)
		s.catalogIDs[id] = true
		if isSecurityRuntimeID(id) {
			s.securityRuntimeEvidence = true
		}
	}
	s.recordCatalogRows(out)
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

func (s *discoveryState) recordCatalogRows(out any) {
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
		if kind == "saved_query" {
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
	if tool == "execute_saved_query" {
		item["name"] = stringArg(args, "name")
	}
	s.executions = append(s.executions, item)
}

func (s *discoveryState) hasCatalogEvidence() bool {
	return s.seedOK || len(s.catalogIDs) != 0 || len(s.catalogSearches) != 0
}

// missingMutationEvidence returns the mutation root fields that lack
// mutation-shape evidence in this run. A target table is covered when its table
// card detail was inspected by id, it was passed through validate_where_clause,
// or a mutation_pattern detail was inspected AND the table surfaced in any
// catalog result. gj_* system roots are excluded — they are governed by the
// security/runtime gate. An unparseable mutation (no roots) demands generic
// shape evidence.
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
		if root == "" || strings.HasPrefix(root, "gj_") {
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

func (s *discoveryState) finalize(resp Response) Response {
	if resp.Status == "" {
		resp.Status = StatusAnswered
	}
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
			out[key] = truncateString(fmt.Sprint(value), 240)
		default:
			out[key] = normalizeValue(value)
		}
	}
	return out
}

func resultSummary(tool string, args map[string]any, out any) map[string]any {
	summary := map[string]any{}
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
			if data, ok := m["data"]; ok && data != nil {
				summary["has_data"] = true
				summary["data_shape"] = dataShape(data)
			}
			if errs, ok := m["errors"].([]any); ok {
				summary["error_count"] = len(errs)
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
