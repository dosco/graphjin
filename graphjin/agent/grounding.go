package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Execution recovery and answer grounding close the gap between "the protocol
// ran" and "the conclusion is true". A failed execution steers the model back
// onto catalog discovery and approved saved queries inside the same run, and an
// answered response may cite field-shaped identifiers only when they appeared
// in this run's observed tool evidence (instruction, history, arguments, and
// results). Both are deterministic server-side guards, not prompt guidance.

const (
	// maxGroundingCorpusBytes bounds the evidence corpus; past it the grounding
	// check disables itself rather than false-block on missing evidence.
	maxGroundingCorpusBytes = 4 << 20
	// minGroundedTokenLength skips short incidental tokens such as ids in
	// prose; real field references (plan_status, remaining_kg) are longer.
	minGroundedTokenLength = 5
	// maxRecoverySavedQueries caps already-discovered candidates mentioned by
	// the raw-GraphQL guard so recovery cannot flood the prompt.
	maxRecoverySavedQueries = 8
	// maxUngroundedTokens caps the reported token list in the violation.
	maxUngroundedTokens = 8
)

// answerFieldTokenPatterns match identifier-shaped references: snake_case and
// camelCase. Plain prose words never match, so ordinary sentences are exempt.
var answerFieldTokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b`),
	regexp.MustCompile(`\b[a-z][a-z0-9]*(?:[A-Z][A-Za-z0-9]*)+\b`),
}

var databaseAggregateFieldPattern = regexp.MustCompile(`(?is)(?:\b[a-zA-Z][a-zA-Z0-9_]*_aggregate\b(?:\s*\([^)]*\))?\s*\{\s*(?:aggregate\s*\{\s*)?(?:count\b|(?:sum|avg|min|max|stddev|variance)\s*\{)|\b(?:count|sum|avg|min|max|stddev|variance)_[a-zA-Z0-9_]+|\b(?:count|sum|avg|min|max|stddev|variance)\s*\(\s*expr\s*:)`)
var databaseAggregateKeyPattern = regexp.MustCompile(`(?i)^(?:count|sum|avg|min|max|stddev|variance)_[a-zA-Z0-9_]+$`)
var hasuraAggregateResultKeyPattern = regexp.MustCompile(`(?i)^(?:count|sum|avg|min|max|stddev|variance)$`)
var databaseOrderFieldPattern = regexp.MustCompile(`(?i)\border_by\s*:\s*\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
var databaseLimitPattern = regexp.MustCompile(`(?i)\blimit\s*:\s*(?:[1-9][0-9]*|\$[a-zA-Z_][a-zA-Z0-9_]*)`)

// groundingVocabulary lists identifier-shaped protocol, tool, skill, and
// system-root terms an answer may always use without data evidence. Domain
// field names never belong here — they must come from run evidence.
var groundingVocabulary = strings.ToLower(strings.Join([]string{
	toolGraphQLHelp, toolQueryCatalog, toolValidateWhere, toolExecuteSavedQuery, toolExecuteGraphQL,
	systemRootCatalog, systemRootSecurity, systemRootRuntime, systemRootConfig,
	systemRootWorkflow, systemRootWorkflowExec, systemRootArtifacts,
	systemRootWatch, systemRootWatchEvent, systemRootTask, systemRootTaskEntry,
	skillDataDiscovery, skillDataAggregation, skillDataWrite, skillCodeRead, skillCodeWrite,
	skillWorkflowRead, skillWorkflowExec, skillWorkflowWrite,
	skillWatchRead, skillWatchWrite,
	skillTaskRead, skillTaskWrite, skillAdminRead, skillAdminWrite,
	StatusAnswered, StatusNeedsClarification, StatusBlocked, StatusError,
	"saved_query", "saved_queries", "saved_mutation", "mutation_pattern", "system_capability",
	"catalog_first", "read_only", "row_level_security", "graphjin_repair", "graphjin_discovery",
	"task_id", "trace_id", "watch_id", "order_by", "args_template", "variables_json",
	"delivery_json", "absence_json", "enrich_json", "snapshot_json", "detail_json",
	"data_json", "verify_json", "metadata_json", "field_not_on_table",
}, "\n"))

// addGrounding appends observed run evidence to the grounding corpus. Values
// are serialized and lowercased; once the corpus would exceed its bound the
// check degrades to disabled instead of risking false blocks.
func (s *discoveryState) addGrounding(values ...any) {
	if s.groundingOverflow {
		return
	}
	for _, value := range values {
		text := stringify(normalizeValue(value))
		if text == "" {
			continue
		}
		if s.groundingCorpus.Len()+len(text) > maxGroundingCorpusBytes {
			s.groundingOverflow = true
			return
		}
		s.groundingCorpus.WriteString(strings.ToLower(text))
		s.groundingCorpus.WriteByte('\n')
	}
}

// ungroundedAnswerTokens returns field-shaped identifiers cited in an answer
// that never appeared in this run's evidence corpus or the fixed protocol
// vocabulary. Matching is case-insensitive substring containment, so JSON
// keys, argument names, and quoted values all ground a token.
func (s *discoveryState) ungroundedAnswerTokens(answer string) []string {
	if s.groundingOverflow {
		return nil
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil
	}
	corpus := s.groundingCorpus.String()
	seen := map[string]bool{}
	var out []string
	for _, pattern := range answerFieldTokenPatterns {
		for _, token := range pattern.FindAllString(answer, -1) {
			if len(token) < minGroundedTokenLength {
				continue
			}
			key := strings.ToLower(token)
			if seen[key] {
				continue
			}
			seen[key] = true
			if strings.Contains(groundingVocabulary, key) || strings.Contains(corpus, key) {
				continue
			}
			out = append(out, token)
		}
	}
	sort.Strings(out)
	if len(out) > maxUngroundedTokens {
		out = out[:maxUngroundedTokens]
	}
	return out
}

// executionFailed reports whether an execution result carries GraphQL errors,
// across both the typed and map-shaped runtime results.
func executionFailed(out any) bool {
	switch res := out.(type) {
	case executeResult:
		return len(res.Errors) != 0
	case *executeResult:
		return res != nil && len(res.Errors) != 0
	case map[string]any:
		return len(anySlice(res["errors"])) != 0
	default:
		return false
	}
}

// attachExecutionRecovery decorates a failed execution result with
// deterministic in-run recovery guidance: the live schema is authoritative,
// repair extensions point at the fix, and approved saved queries discovered in
// this run are the preferred retry path.
//
// The guidance is also appended to each error message, not just carried in a
// sibling field. A model that has decided the run failed reads errors[].message
// and summarizes it; recovery advice it never opens does not change behavior.
func attachExecutionRecovery(out any, state *discoveryState, query string) any {
	if !executionFailed(out) {
		return out
	}
	if repair, ok := systemRootDidYouMeanRepair(out, state, query); ok {
		return attachSystemRootDidYouMean(out, repair)
	}
	recovery := executionRecovery()
	directive := recoveryDirective(recovery)
	switch res := out.(type) {
	case executeResult:
		res.Recovery = recovery
		res.Errors = appendRecoveryDirective(res.Errors, directive)
		return res
	case *executeResult:
		res.Recovery = recovery
		res.Errors = appendRecoveryDirective(res.Errors, directive)
		return res
	case map[string]any:
		res["recovery"] = recovery
		errors := anySlice(res["errors"])
		for _, item := range errors {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			entry["message"] = joinRecoveryMessage(stringValue(entry["message"]), directive)
		}
		if len(errors) != 0 {
			res["errors"] = errors
		}
		return res
	default:
		return out
	}
}

type systemRootRepair struct {
	attempted  string
	candidates []string
	example    string
}

// systemRootDidYouMeanRepair recognizes invented GraphJin-owned roots only
// after the execution reports an unknown-root failure. Application table
// failures retain the generic catalog repair. Candidate roots are restricted
// to the caller-visible fixed roots in the capability profile.
func systemRootDidYouMeanRepair(out any, state *discoveryState, query string) (systemRootRepair, bool) {
	if state == nil || state.capabilities == nil {
		return systemRootRepair{}, false
	}
	roots := append(QueryRootFields(query), MutationRootFields(query)...)
	for _, attempted := range roots {
		attempted = strings.ToLower(strings.TrimSpace(attempted))
		if attempted == "" || isFixedSystemRoot(attempted) {
			continue
		}
		watchIntent := systemWatchRootIntent(attempted)
		if !watchIntent && !strings.HasPrefix(attempted, "gj_") {
			continue
		}
		if !executionReportsUnknownRoot(out, attempted) {
			continue
		}
		candidates := visibleSystemRootSuggestions(state.capabilities, attempted, watchIntent)
		if len(candidates) == 0 {
			continue
		}
		return systemRootRepair{
			attempted:  attempted,
			candidates: candidates,
			example:    systemRootExample(candidates[0]),
		}, true
	}
	return systemRootRepair{}, false
}

func systemWatchRootIntent(root string) bool {
	root = strings.ToLower(strings.TrimSpace(root))
	return root == "watch" || root == "watches" ||
		strings.Contains(root, "watch_event") ||
		strings.Contains(root, "watch_events")
}

func isFixedSystemRoot(root string) bool {
	for _, candidate := range fixedSystemRoots {
		if strings.EqualFold(strings.TrimSpace(root), candidate) {
			return true
		}
	}
	return false
}

func visibleSystemRootSuggestions(profile *CapabilityProfile, attempted string, watchIntent bool) []string {
	visible := make([]string, 0, len(profile.AvailableSystemRoots))
	for _, root := range fixedSystemRoots {
		if profileHasSystemRoot(profile, root) {
			visible = append(visible, root)
		}
	}
	if watchIntent {
		preferred := []string{systemRootWatch, systemRootWatchEvent}
		if strings.Contains(attempted, "event") {
			preferred[0], preferred[1] = preferred[1], preferred[0]
		}
		var out []string
		for _, root := range preferred {
			if profileHasSystemRoot(profile, root) {
				out = append(out, root)
			}
		}
		return out
	}
	if !strings.HasPrefix(attempted, "gj_") || len(visible) == 0 {
		return nil
	}
	bestDistance := -1
	var out []string
	for _, root := range visible {
		distance := editDistance(attempted, root)
		if bestDistance == -1 || distance < bestDistance {
			bestDistance = distance
			out = []string{root}
			continue
		}
		if distance == bestDistance {
			out = append(out, root)
		}
	}
	return out
}

func editDistance(left, right string) int {
	if left == right {
		return 0
	}
	previous := make([]int, len(right)+1)
	for i := range previous {
		previous[i] = i
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func executionReportsUnknownRoot(out any, attempted string) bool {
	for _, message := range executionErrorMessages(out) {
		lower := strings.ToLower(message)
		if !strings.Contains(lower, attempted) {
			continue
		}
		for _, marker := range []string{
			"table not found",
			"unknown table",
			"root field not found",
			"unknown root",
			"cannot query field",
			"unknown field",
			"field not found",
			"does not exist",
		} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func executionErrorMessages(out any) []string {
	var messages []string
	switch result := out.(type) {
	case executeResult:
		for _, info := range result.Errors {
			messages = append(messages, info.Message)
		}
	case *executeResult:
		if result != nil {
			for _, info := range result.Errors {
				messages = append(messages, info.Message)
			}
		}
	case map[string]any:
		for _, item := range anySlice(result["errors"]) {
			if info := mapValue(item); info != nil {
				messages = append(messages, stringFromMap(info, "message"))
			}
		}
	}
	return messages
}

func systemRootExample(root string) string {
	switch root {
	case systemRootWatchEvent:
		return `query { gj_watch_event(where: { seen: { eq: false } }, order_by: { created_at: desc }, limit: 20) { id watch_id data_json seen created_at } }`
	case systemRootWatch:
		return `query { gj_watch(limit: 20) { id name status enabled } }`
	default:
		return fmt.Sprintf("query { %s(limit: 20) { id } }", root)
	}
}

func attachSystemRootDidYouMean(out any, suggestion systemRootRepair) any {
	next := map[string]any{
		"recommended_tool": toolExecuteGraphQL,
		"reason":           "Retry with a caller-visible canonical GraphJin system root; do not invent an alias.",
		"args":             map[string]any{"query": suggestion.example},
	}
	repair := map[string]any{
		"code":                   "system_root_did_you_mean",
		"kind":                   "system_root_did_you_mean",
		"attempted_root":         suggestion.attempted,
		"available_system_roots": suggestion.candidates,
		"example_query":          suggestion.example,
		"next":                   next,
	}
	recovery := map[string]any{
		"code":                   "system_root_did_you_mean",
		"kind":                   "system_root_did_you_mean",
		"instruction":            fmt.Sprintf("Root %q is not a GraphJin system root visible to this caller. Use one of the named roots exactly and retry the provided query in this run.", suggestion.attempted),
		"attempted_root":         suggestion.attempted,
		"available_system_roots": suggestion.candidates,
		"example_query":          suggestion.example,
		"next":                   next,
	}
	directive := fmt.Sprintf("%s root %q is unknown; use the caller-visible root %q exactly and retry the provided example in this run.", recoveryDirectivePrefix, suggestion.attempted, suggestion.candidates[0])
	decorate := func(info *ErrorInfo) {
		if info.Extensions == nil {
			info.Extensions = map[string]any{}
		}
		info.Extensions["code"] = "system_root_did_you_mean"
		info.Extensions["graphjin_repair"] = repair
		info.Message = joinRecoveryMessage(info.Message, directive)
	}

	switch result := out.(type) {
	case executeResult:
		for i := range result.Errors {
			decorate(&result.Errors[i])
		}
		result.Recovery = recovery
		return result
	case *executeResult:
		if result == nil {
			return out
		}
		for i := range result.Errors {
			decorate(&result.Errors[i])
		}
		result.Recovery = recovery
		return result
	case map[string]any:
		errors := anySlice(result["errors"])
		for _, item := range errors {
			entry := mapValue(item)
			if entry == nil {
				continue
			}
			extensions := mapValue(entry["extensions"])
			if extensions == nil {
				extensions = map[string]any{}
			}
			extensions["code"] = "system_root_did_you_mean"
			extensions["graphjin_repair"] = repair
			entry["extensions"] = extensions
			entry["message"] = joinRecoveryMessage(stringFromMap(entry, "message"), directive)
		}
		result["errors"] = errors
		result["recovery"] = recovery
		return result
	default:
		return out
	}
}

func appendRecoveryDirective(errors []ErrorInfo, directive string) []ErrorInfo {
	for i := range errors {
		errors[i].Message = joinRecoveryMessage(errors[i].Message, directive)
	}
	return errors
}

func joinRecoveryMessage(message, directive string) string {
	message = strings.TrimSpace(message)
	if directive == "" || strings.Contains(message, recoveryDirectivePrefix) {
		return message
	}
	if message == "" {
		return directive
	}
	return message + " " + directive
}

const recoveryDirectivePrefix = "GraphJin recovery:"

// recommendedSavedQueryNames returns saved queries already discovered in this
// run. Recovery never performs hidden catalog calls or injects extra cards.
func recommendedSavedQueryNames(s *discoveryState) []string {
	return sortedBoolKeys(s.savedQueriesDiscovered)
}

// approvedSavedQuerySuffix names the governed shortcuts discovered for this
// run, for appending to a protocol rejection message. Dynamic authoring stays
// the primary path; the names are an option, not a required route.
func approvedSavedQuerySuffix(s *discoveryState) string {
	names := recommendedSavedQueryNames(s)
	if len(names) > maxRecoverySavedQueries {
		names = names[:maxRecoverySavedQueries]
	}
	if len(names) == 0 {
		return ""
	}
	return " Author the query from the returned column names, or use an approved saved query as a governed shortcut when one matches (" +
		strings.Join(names, ", ") + "): query_catalog({id:\"saved_query:<name>\"}) then execute_saved_query({name:\"<name>\"})."
}

// recoveryDirective is the single sentence appended to execution errors; the
// structured recovery.next field carries the actionable tool pointer.
func recoveryDirective(map[string]any) string {
	return recoveryDirectivePrefix + " the live schema is authoritative; do not report it as broken or propose schema changes—follow recovery.next to re-discover real fields and retry in this run."
}

func executionRecovery() map[string]any {
	return map[string]any{
		"instruction": "The live schema is authoritative; do not report it as broken or propose schema changes—follow errors[].extensions.graphjin_repair and next to re-discover real fields and retry in this run.",
		"next": catalogNext(
			toolQueryCatalog,
			"Inspect the real table and column details, re-author the query from returned fields, and retry in this run.",
		),
	}
}

func (s *discoveryState) pendingDatabaseComputation() string {
	needsAggregate, needsAggregateOrder := databaseComputationIntent(s.instruction)
	if !needsAggregate {
		return ""
	}
	allowsRankedRows := databaseOrderedRowIntent(s.instruction)
	for _, execution := range s.executions {
		if execution["has_data"] != true || executionErrorCount(execution["error_count"]) != 0 {
			continue
		}
		if strings.EqualFold(stringFromMap(execution, "tool"), toolExecuteSavedQuery) {
			query := s.savedQueryGraphQLFor(stringFromMap(execution, "name"))
			hasAggregate := databaseAggregateFieldPattern.MatchString(query) || execution["database_aggregate"] == true
			if hasAggregate && (!needsAggregateOrder || strings.Contains(strings.ToLower(query), "order_by")) {
				return ""
			}
			if allowsRankedRows && queryUsesDatabaseRanking(query, s.instruction) {
				return ""
			}
			continue
		}
		query := stringFromMap(execution, "query")
		if databaseAggregateFieldPattern.MatchString(query) &&
			(!needsAggregateOrder || strings.Contains(strings.ToLower(query), "order_by")) {
			return ""
		}
		if allowsRankedRows && queryUsesDatabaseRanking(query, s.instruction) {
			return ""
		}
	}
	if allowsRankedRows {
		return "database_computation_required: this request asks for an extreme or ranking, but the run only has row-list or failed evidence. Execute one distinct database-side query that applies order_by on the requested ranked column together with a limit, then answer from that result."
	}
	requirement := "a successful database-side aggregate field such as count_/sum_/avg_/min_/max_<column>"
	if needsAggregateOrder {
		requirement += " with aggregate order_by for the requested ranking"
	}
	return "database_computation_required: this request asks for a count, total, average, extreme, or ranking, but the run only has row-list or failed evidence. Execute " + requirement + " and answer from that result; do not calculate from fetched rows."
}

// databaseOrderedRowIntent recognizes extrema where the user wants the record
// as well as its value. A database-side order_by plus limit computes that
// result without client-side aggregation. Explicit count/total/average intent
// remains aggregate-only, including grouped rankings such as "most total
// revenue".
func databaseOrderedRowIntent(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	if lower == "" || databaseStrictAggregateIntent(lower) {
		return false
	}
	for _, phrase := range []string{"top ", "rank", "ranking", "minimum", "maximum", "highest", "lowest", "latest", "earliest", "extreme"} {
		if strings.Contains(" "+lower+" ", phrase) {
			return true
		}
	}
	choosesRecord := strings.Contains(lower, "which ") || strings.Contains(lower, "who ")
	return choosesRecord && (strings.Contains(" "+lower+" ", " most ") || strings.Contains(" "+lower+" ", " least "))
}

func databaseStrictAggregateIntent(instruction string) bool {
	lower := " " + strings.ToLower(strings.TrimSpace(instruction)) + " "
	for _, phrase := range []string{" how many ", " count ", " count of ", " total ", " sum ", " average", " avg ", " mean "} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func queryUsesDatabaseRanking(query, instruction string) bool {
	if !databaseLimitPattern.MatchString(query) {
		return false
	}
	match := databaseOrderFieldPattern.FindStringSubmatch(query)
	if len(match) != 2 {
		return false
	}
	rankedColumn := strings.Join(strings.FieldsFunc(strings.ToLower(match[1]), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
	return rankedColumn != "" && strings.Contains(strings.ToLower(instruction), rankedColumn)
}

func resultContainsAggregateField(value any) bool {
	var walk func(any, int) bool
	walk = func(current any, depth int) bool {
		if depth > 16 {
			return false
		}
		switch typed := normalizeValue(current).(type) {
		case map[string]any:
			for key, item := range typed {
				if databaseAggregateKeyPattern.MatchString(key) ||
					(strings.EqualFold(key, "aggregate") && hasuraAggregateResult(item)) ||
					walk(item, depth+1) {
					return true
				}
			}
		case []any:
			for _, item := range typed {
				if walk(item, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(value, 0)
}

func hasuraAggregateResult(value any) bool {
	result := mapValue(value)
	for key := range result {
		if hasuraAggregateResultKeyPattern.MatchString(key) {
			return true
		}
	}
	return false
}

func executionErrorCount(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func databaseComputationIntent(instruction string) (aggregate, aggregateOrder bool) {
	lower := strings.Join(strings.FieldsFunc(strings.ToLower(strings.TrimSpace(instruction)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
	if lower == "" {
		return false, false
	}
	padded := " " + lower + " "
	for _, phrase := range []string{"how many", "count", "count of", "total", "sum", "average", "avg", "mean", "minimum", "maximum", "highest", "lowest", "latest", "earliest", "extreme"} {
		if strings.Contains(padded, " "+phrase+" ") {
			aggregate = true
			break
		}
	}
	for _, phrase := range []string{"top", "rank", "ranking"} {
		if strings.Contains(padded, " "+phrase+" ") {
			aggregate = true
			aggregateOrder = true
			break
		}
	}
	choosesGroup := strings.Contains(padded, " which ") || strings.Contains(padded, " who ")
	if choosesGroup {
		for _, phrase := range []string{"most", "least", "highest", "lowest"} {
			if strings.Contains(padded, " "+phrase+" ") {
				aggregate = true
				aggregateOrder = true
				break
			}
		}
	}
	return aggregate, aggregateOrder
}
