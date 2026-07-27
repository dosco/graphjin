package agent

import (
	"regexp"
	"sort"
	"strings"
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
	// maxRecoverySavedQueries caps the candidate list attached to a failed
	// execution result.
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

// groundingVocabulary lists identifier-shaped protocol, tool, skill, and
// system-root terms an answer may always use without data evidence. Domain
// field names never belong here — they must come from run evidence.
var groundingVocabulary = strings.ToLower(strings.Join([]string{
	toolGraphQLHelp, toolQueryCatalog, toolValidateWhere, toolExecuteSavedQuery, toolExecuteGraphQL,
	systemRootCatalog, systemRootSecurity, systemRootRuntime, systemRootConfig,
	systemRootWorkflow, systemRootWorkflowExec, systemRootArtifacts,
	systemRootWatch, systemRootWatchEvent, systemRootTask, systemRootTaskEntry,
	skillDataDiscovery, skillDataWrite, skillCodeRead, skillCodeWrite,
	skillWorkflowRead, skillWorkflowExec, skillWorkflowWrite,
	skillWatchRead, skillWatchWrite, skillWatchFlow, skillWatchDelivery,
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
func attachExecutionRecovery(out any, s *discoveryState) any {
	if !executionFailed(out) {
		return out
	}
	recovery := executionRecovery(s)
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

// recommendedSavedQueryNames returns the saved queries worth naming in
// guidance: the operation-filtered supplement set when present, else every
// saved query discovered this run.
func recommendedSavedQueryNames(s *discoveryState) []string {
	if len(s.savedQuerySupplementCards) != 0 {
		var names []string
		for _, card := range s.savedQuerySupplementCards {
			name := stringFromMap(card, "name")
			if name == "" {
				name = savedQueryNameFromID(stringFromMap(card, "id"))
			}
			if name = canonicalSavedQueryName(name); name != "" {
				names = appendUniqueString(names, name)
			}
		}
		if len(names) != 0 {
			sort.Strings(names)
			return names
		}
	}
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

// recoveryDirective renders the imperative one-liner appended to failed
// execution errors. Re-authoring dynamically from real column names is the
// primary recovery; a matching approved saved query is a shortcut.
func recoveryDirective(recovery map[string]any) string {
	directive := recoveryDirectivePrefix + " this is a query-authoring mistake, not a data or schema problem." +
		" The live schema is authoritative — do not report it as broken, do not propose schema changes, and do not stop at blocked." +
		" Re-discover the real field names with query_catalog, re-author the query from the returned columns, and retry in this run."
	names, _ := recovery["approved_saved_queries"].([]string)
	if len(names) == 0 {
		return directive
	}
	return directive +
		" A matching approved saved query is a governed shortcut (" + strings.Join(names, ", ") + "):" +
		" query_catalog({id:\"saved_query:<name>\"}), then execute_saved_query({name:\"<name>\"})."
}

func executionRecovery(s *discoveryState) map[string]any {
	recovery := map[string]any{
		"instruction": "This query did not match the live schema. The schema is authoritative: never advise schema or data changes and do not stop at blocked. Recover in this run — follow errors[].extensions.graphjin_repair, re-discover the real table and field names with query_catalog, re-author the query from the returned columns, and retry.",
		"next":        []string{toolQueryCatalog},
	}
	names := recommendedSavedQueryNames(s)
	if len(names) > maxRecoverySavedQueries {
		names = names[:maxRecoverySavedQueries]
	}
	if len(names) != 0 {
		recovery["approved_saved_queries"] = names
		recovery["saved_query_usage"] = `query_catalog({id:"saved_query:<name>"}) then execute_saved_query({name:"<name>"})`
		recovery["instruction"] = recovery["instruction"].(string) +
			" A saved query in approved_saved_queries that matches the request is a governed shortcut: inspect it with query_catalog({id:\"saved_query:<name>\"}), then execute_saved_query."
	}
	return recovery
}
