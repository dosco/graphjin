package agent

import (
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

var databaseAggregateFieldPattern = regexp.MustCompile(`(?i)(?:\b(?:count|sum|avg|min|max|stddev|variance)_[a-zA-Z0-9_]+|\b(?:count|sum|avg|min|max|stddev|variance)\s*\(\s*expr\s*:)`)
var databaseAggregateKeyPattern = regexp.MustCompile(`(?i)^(?:count|sum|avg|min|max|stddev|variance)_[a-zA-Z0-9_]+$`)
var databaseOrderFieldPattern = regexp.MustCompile(`(?i)\border_by\s*:\s*\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
var databaseLimitPattern = regexp.MustCompile(`(?i)\blimit\s*:\s*(?:[1-9][0-9]*|\$[a-zA-Z_][a-zA-Z0-9_]*)`)
var wrongDialectAggregatePattern = regexp.MustCompile(`(?i)\b[a-zA-Z][a-zA-Z0-9_]*_aggregate\b`)
var aggregateFunctionCallPattern = regexp.MustCompile(`(?i)\b(?:count|sum|avg|min|max|stddev|variance)\s*\(`)
var aggregateFunctionColumnPattern = regexp.MustCompile(`(?i)\b(count|sum|avg|min|max|stddev|variance)\s*\(\s*(?:column|expr)\s*:\s*([a-zA-Z_][a-zA-Z0-9_]*)`)
var unknownGraphQLShapePattern = regexp.MustCompile(`(?i)(?:\bunknown\s+(?:graphql\s+)?(?:field|root|column|table)\b|\b(?:field|root|column|table)\b.{0,96}\b(?:not found|does not exist|not a (?:column|function))\b)`)
var unknownArgumentPattern = regexp.MustCompile(`(?i)\bunknown\s+argument\b`)
var graphQLIdentifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

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
func attachExecutionRecovery(out any, s *discoveryState, query string) any {
	if !executionFailed(out) {
		return out
	}
	aggregateSyntaxFailure, aggregateFieldHint := aggregateSyntaxRepair(out, s, query)
	if aggregateSyntaxFailure {
		out = attachWrongDialectAggregateRepair(out, aggregateFieldHint)
	}
	recovery := executionRecovery(s, aggregateSyntaxFailure, aggregateFieldHint)
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

func executionRecovery(_ *discoveryState, aggregateSyntaxFailure bool, aggregateFieldHint string) map[string]any {
	recovery := map[string]any{
		"instruction": "The live schema is authoritative; do not report it as broken or propose schema changes—follow errors[].extensions.graphjin_repair and next to re-discover real fields and retry in this run.",
		"next": catalogNext(
			toolQueryCatalog,
			"Inspect the real table and column details, re-author the query from returned fields, and retry in this run.",
		),
	}
	if aggregateSyntaxFailure {
		recovery["instruction"] = aggregateSyntaxRepairMessage(aggregateFieldHint)
		recovery["next"] = map[string]any{
			"tool":   toolGraphQLHelp,
			"args":   map[string]any{"for": "query"},
			"reason": "Open GraphJin query help, inspect the exact table detail, and retry with count_/sum_/avg_/min_/max_<column> fields on that table root.",
		}
	}
	return recovery
}

func attachWrongDialectAggregateRepair(out any, aggregateFieldHint string) any {
	repair := map[string]any{
		"kind":    "wrong_dialect_aggregate",
		"message": aggregateSyntaxRepairMessage(aggregateFieldHint),
		"next":    map[string]any{"tool": toolGraphQLHelp, "args": map[string]any{"for": "query"}},
	}
	apply := func(errors []ErrorInfo) []ErrorInfo {
		for i := range errors {
			if errors[i].Extensions == nil {
				errors[i].Extensions = map[string]any{}
			}
			errors[i].Extensions["code"] = "wrong_dialect_aggregate"
			errors[i].Extensions["graphjin_repair"] = repair
		}
		return errors
	}
	switch res := out.(type) {
	case executeResult:
		res.Errors = apply(res.Errors)
		return res
	case *executeResult:
		if res != nil {
			res.Errors = apply(res.Errors)
		}
		return res
	case map[string]any:
		for _, item := range anySlice(res["errors"]) {
			entry := mapValue(item)
			if entry == nil {
				continue
			}
			extensions := mapValue(entry["extensions"])
			if extensions == nil {
				extensions = map[string]any{}
				entry["extensions"] = extensions
			}
			extensions["code"] = "wrong_dialect_aggregate"
			extensions["graphjin_repair"] = repair
		}
		return res
	default:
		return out
	}
}

func wrongDialectAggregateQuery(query string) bool {
	return wrongDialectAggregatePattern.MatchString(query)
}

// aggregateSyntaxRepair recognizes the failure shape instead of attempting to
// enumerate every aggregate spelling a model might invent. The historical
// <table>_aggregate form remains an unconditional trigger. Other unknown
// field/root failures require aggregate intent, and unknown arguments require
// an aggregate function call so ordinary GraphQL argument mistakes retain the
// generic schema-recovery path.
func aggregateSyntaxRepair(out any, s *discoveryState, query string) (bool, string) {
	hint := aggregateFieldHint(s, query)
	if wrongDialectAggregateQuery(query) {
		return true, hint
	}
	if s == nil {
		return false, ""
	}
	aggregateIntent, _ := databaseComputationIntent(s.instruction)
	if !aggregateIntent {
		return false, ""
	}
	for _, errorInfo := range executionErrorInfo(out) {
		if unknownAggregateFieldCode(stringFromMap(errorInfo.Extensions, "code")) ||
			unknownGraphQLShapePattern.MatchString(errorInfo.Message) ||
			(aggregateFunctionCallPattern.MatchString(query) && unknownArgumentPattern.MatchString(errorInfo.Message)) {
			return true, hint
		}
	}
	return false, ""
}

func executionErrorInfo(out any) []ErrorInfo {
	switch res := out.(type) {
	case executeResult:
		return res.Errors
	case *executeResult:
		if res != nil {
			return res.Errors
		}
	case map[string]any:
		errors := anySlice(res["errors"])
		out := make([]ErrorInfo, 0, len(errors))
		for _, item := range errors {
			entry := mapValue(item)
			if entry == nil {
				continue
			}
			out = append(out, ErrorInfo{
				Message:    stringFromMap(entry, "message"),
				Extensions: mapValue(entry["extensions"]),
			})
		}
		return out
	}
	return nil
}

func unknownAggregateFieldCode(code string) bool {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", "_"))
	switch code {
	case "unknown_field", "unknown_root", "unknown_column", "unknown_table",
		"field_not_found", "root_not_found", "column_not_found", "table_not_found",
		"field_not_on_table":
		return true
	default:
		return false
	}
}

func aggregateSyntaxRepairMessage(hint string) string {
	message := "GraphJin does not use <table>_aggregate roots or function-style aggregate fields. Aggregates are fields on the table selection (for example orders { count_id sum_total })."
	if hint != "" {
		message += " For this request, retry with " + hint + " on the table selection."
	}
	return message
}

func aggregateFieldHint(s *discoveryState, query string) string {
	if match := aggregateFunctionColumnPattern.FindStringSubmatch(query); len(match) == 3 {
		return strings.ToLower(match[1]) + "_" + strings.ToLower(match[2])
	}
	if s == nil {
		return ""
	}
	operation := aggregateOperationForInstruction(s.instruction)
	if operation == "" {
		return ""
	}
	var fields []string
	for id := range s.catalogIDs {
		if field := catalogColumnField(id); field != "" && instructionMentionsField(s.instruction, field) {
			fields = appendUniqueString(fields, field)
		}
	}
	if len(fields) == 0 {
		return ""
	}
	sort.Slice(fields, func(i, j int) bool {
		if len(fields[i]) == len(fields[j]) {
			return fields[i] < fields[j]
		}
		return len(fields[i]) > len(fields[j])
	})
	return operation + "_" + fields[0]
}

func aggregateOperationForInstruction(instruction string) string {
	lower := " " + strings.ToLower(strings.TrimSpace(instruction)) + " "
	for _, candidate := range []struct {
		operation string
		phrases   []string
	}{
		{operation: "count", phrases: []string{" how many ", " count ", " count of "}},
		{operation: "avg", phrases: []string{" average", " avg ", " mean "}},
		{operation: "sum", phrases: []string{" total ", " sum "}},
		{operation: "min", phrases: []string{" minimum", " lowest", " earliest"}},
		{operation: "max", phrases: []string{" maximum", " highest", " latest"}},
	} {
		for _, phrase := range candidate.phrases {
			if strings.Contains(lower, phrase) {
				return candidate.operation
			}
		}
	}
	return ""
}

func catalogColumnField(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(strings.ToLower(id), "column:") {
		return ""
	}
	if index := strings.LastIndexByte(id, '.'); index >= 0 && index+1 < len(id) {
		field := id[index+1:]
		if graphQLIdentifierPattern.MatchString(field) {
			return strings.ToLower(field)
		}
	}
	return ""
}

func instructionMentionsField(instruction, field string) bool {
	normalize := func(value string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
	}
	haystack := " " + normalize(instruction) + " "
	needle := normalize(field)
	return needle != "" && strings.Contains(haystack, " "+needle+" ")
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
				if databaseAggregateKeyPattern.MatchString(key) || walk(item, depth+1) {
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
