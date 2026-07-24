package agent

import (
	"context"
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
	return &discoveryState{
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
	r.state.seedResult = normalizeValue(out)
	r.state.recordCatalog(args, out, true)
	return r.state.seedResult, nil
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
	if writeLikeGraphQL(query) && !r.state.securityRuntimeEvidence {
		err := fmt.Errorf("protocol violation: inspect security/runtime catalog guidance before write-capable or control-plane GraphQL")
		r.state.addViolation("security_runtime_discovery_required", err.Error(), "execute_graphql", true, map[string]any{"required": []any{"help:security", "help:runtime"}})
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
	}
	if ContainsMutationOperation(query) {
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
	r.state.finishAction(action, "execute_graphql", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_graphql", args, out)
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
	}
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
		case s.hasBlockingViolation():
			resp = blockResponse(resp)
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
