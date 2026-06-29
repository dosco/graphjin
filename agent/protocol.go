package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const protocolContextKey = "_graphjin_discovery"

type protocolRuntime struct {
	base      GraphRuntime
	state     *discoveryState
	namespace string
}

type discoveryState struct {
	instruction             string
	skillName               string
	skillRequiredEvidence   func(*discoveryState) bool
	seedOK                  bool
	seedResult              any
	modelDiscoveryAction    bool
	catalogIDs              map[string]bool
	catalogKinds            map[string]bool
	savedQueriesDiscovered  map[string]bool
	savedQueriesDetailed    map[string]bool
	securityRuntimeEvidence bool
	actions                 []protocolAction
	helpTopics              []string
	catalogSearches         []map[string]any
	catalogDetails          []string
	suggestedNext           []any
	validations             []map[string]any
	executions              []map[string]any
	rawGraphQL              []map[string]any
	violations              []protocolViolation
}

type protocolAction struct {
	Step    int            `json:"step"`
	Source  string         `json:"source"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type protocolViolation struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Tool     string         `json:"tool,omitempty"`
	Blocking bool           `json:"blocking"`
	Details  map[string]any `json:"details,omitempty"`
}

func newProtocolRuntime(base GraphRuntime, instruction, namespace string) *protocolRuntime {
	return &protocolRuntime{
		base:      base,
		namespace: namespace,
		state:     newDiscoveryState(instruction),
	}
}

func newDiscoveryState(instruction string) *discoveryState {
	return &discoveryState{
		instruction:            strings.TrimSpace(instruction),
		catalogIDs:             map[string]bool{},
		catalogKinds:           map[string]bool{},
		savedQueriesDiscovered: map[string]bool{},
		savedQueriesDetailed:   map[string]bool{},
	}
}

// setSkill records the deterministically selected skill and its required-evidence
// predicate so finalize can enforce it and surface it in evidence.
func (s *discoveryState) setSkill(sk skill) {
	s.skillName = sk.name
	s.skillRequiredEvidence = sk.requiredEvidence
}

func (r *protocolRuntime) Seed(ctx context.Context) (any, error) {
	args := map[string]any{
		"search":  r.state.instruction,
		"explain": true,
		"limit":   10,
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
		Step:   len(s.actions) + 1,
		Source: source,
		Tool:   tool,
		Args:   redactArgs(args),
		Status: "started",
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
	id := stringArg(args, "id")
	if id != "" {
		s.catalogDetails = appendUniqueString(s.catalogDetails, id)
		s.catalogIDs[id] = true
		if isSecurityRuntimeID(id) {
			s.securityRuntimeEvidence = true
		}
	}
	s.recordCatalogRows(out)
	if id != "" && s.catalogKindSeen("saved_query") {
		for _, name := range savedQueryNamesFromResult(out) {
			s.markSavedQueryDetailed(name)
		}
		if name := savedQueryNameFromID(id); name != "" {
			s.markSavedQueryDetailed(name)
		}
	}
	s.recordNext(out)
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
		case s.skillRequiredEvidence != nil && !s.skillRequiredEvidence(s):
			s.addViolation("skill_evidence_required", fmt.Sprintf("agent answered without the evidence required by the %q skill", s.skillName), "", true, map[string]any{"skill": s.skillName})
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
		"selected_skill": s.skillName,
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
		strings.Contains(lower, "gj_code")
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
