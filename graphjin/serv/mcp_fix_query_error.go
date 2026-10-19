package serv

import (
	"fmt"
	"regexp"
	"strings"
)

// FixQueryErrorResult is the structured tool-result envelope for fix_query_error.
type FixQueryErrorResult struct {
	Title         string   `json:"title"`
	GuideMarkdown string   `json:"guide_markdown"`
	Kind          string   `json:"kind"`
	Diagnosis     string   `json:"diagnosis"`
	RepairedQuery string   `json:"repaired_query,omitempty"`
	FollowUpTools []string `json:"follow_up_tools,omitempty"`
}

const (
	fixKindMultiFKAmbiguity    = "multi_fk_ambiguity"
	fixKindDistinctJoinShape   = "distinct_aggregate_nested_join_shape"
	fixKindPartitionFilter     = "partition_filter_required"
	fixKindUnknownRelationship = "unknown_relationship"
	fixKindTableNotFound       = "table_not_found"
	fixKindColumnNotFound      = "column_not_found"
	fixKindFieldNotOnTable     = "field_not_on_table"
	fixKindAnalyticsDirective  = "analytics_directive"
	fixKindWrongDialect        = "wrong_dialect"
	fixKindOperatorInvalid     = "operator_or_syntax_invalid"
	fixKindSyntaxParse         = "syntax_or_parse_error"
	fixKindPermission          = "permission_denied"
	fixKindMutationNotAllowed  = "mutation_not_allowed"
	fixKindVariable            = "variable_error"
	fixKindGeneric             = "generic"
)

var (
	reAmbiguousRel    = regexp.MustCompile(`ambiguous relationship\s+(\S+)\s*->\s*(\S+):\s*multiple foreign keys\s*\(([^)]+)\)`)
	reNestedShape     = regexp.MustCompile(`nested selection '([^']+)' joins through parent column '([^']+)\.([^']+)', which is not in distinct: \[([^\]]+)\]`)
	rePartitionReq    = regexp.MustCompile(`table\s+"([^"]+)"\s+requires a filter on (?:partition|temporal) column\s+"([^"]+)"`)
	reFieldNotOnTable = regexp.MustCompile(`field '([^']+)' is not a column or a function`)
	reWrongDialectArg = regexp.MustCompile(`unknown argument\s+['"` + "`" + `]?(aggregation|aggregate)['"` + "`" + `]?`)
)

// buildFixQueryErrorRepair classifies a failing query+error and returns structured repair guidance.
func buildFixQueryErrorRepair(query, errorMsg string, analyticsMode bool) FixQueryErrorResult {
	res := FixQueryErrorResult{Title: "Query error analysis", Kind: fixKindGeneric}
	errLower := strings.ToLower(errorMsg)

	switch {
	case reAmbiguousRel.MatchString(errorMsg):
		fillMultiFKArm(&res, errorMsg)
	case reNestedShape.MatchString(errorMsg):
		fillDistinctJoinShapeArm(&res, errorMsg)
	case rePartitionReq.MatchString(errorMsg):
		fillPartitionFilterArm(&res, errorMsg)
	case isAnalyticsDirectiveError(errLower):
		fillAnalyticsDirectiveArm(&res)
	case reFieldNotOnTable.MatchString(errorMsg):
		fillFieldNotOnTableArm(&res, errorMsg)
	case isWrongDialectError(errorMsg, query):
		fillWrongDialectArm(&res, errorMsg, query)
	case strings.Contains(errLower, "relationship not found"):
		fillUnknownRelArm(&res, errorMsg)
	case strings.Contains(errLower, "table") && (strings.Contains(errLower, "not found") || strings.Contains(errLower, "unknown")):
		fillTableNotFoundArm(&res)
	case strings.Contains(errLower, "column") && (strings.Contains(errLower, "not found") || strings.Contains(errLower, "unknown") || strings.Contains(errLower, "does not exist")):
		fillColumnNotFoundArm(&res)
	case strings.Contains(errLower, "operator") || strings.Contains(errLower, "invalid"):
		fillOperatorArm(&res)
	case strings.Contains(errLower, "syntax") || strings.Contains(errLower, "parse"):
		fillSyntaxArm(&res)
	case strings.Contains(errLower, "permission") || strings.Contains(errLower, "access") || strings.Contains(errLower, "denied"):
		fillPermissionArm(&res)
	case strings.Contains(errLower, "mutation") && strings.Contains(errLower, "not allowed"):
		fillMutationNotAllowedArm(&res)
	case strings.Contains(errLower, "variable") || strings.Contains(errLower, "$"):
		fillVariableArm(&res)
	default:
		fillGenericArm(&res)
	}

	res.GuideMarkdown = renderFixMarkdown(query, errorMsg, &res, analyticsMode)
	return res
}

func isAnalyticsDirectiveError(errLower string) bool {
	return strings.Contains(errLower, "@window has been replaced") ||
		strings.Contains(errLower, "analytics directive") ||
		strings.Contains(errLower, "analytics directives") ||
		strings.Contains(errLower, "@running") ||
		strings.Contains(errLower, "@moving") ||
		strings.Contains(errLower, "@previous") ||
		strings.Contains(errLower, "@next") ||
		strings.Contains(errLower, "@first") ||
		strings.Contains(errLower, "@last") ||
		strings.Contains(errLower, "@rank") ||
		strings.Contains(errLower, "@denserank") ||
		strings.Contains(errLower, "@rownumber")
}

func fillAnalyticsDirectiveArm(res *FixQueryErrorResult) {
	res.Kind = fixKindAnalyticsDirective
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card"}
	res.Diagnosis = "Use GraphJin analytics directives on real columns for reporting metrics. Use @running or @moving for row-level aggregates, @previous/@next for period comparisons, and @rank/@denseRank/@rowNumber for ranking. Use distinct plus aggregate fields for ordinary one-row-per-group summaries."
	res.RepairedQuery = `query {
  orders {
    account_id
    month
    total
    running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc })
    previous_total: total @previous(by: "account_id", orderBy: { month: asc })
    rank_by_total: total @rank(by: "account_id", order: desc)
  }
}`
}

func fillMultiFKArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindMultiFKAmbiguity
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card"}

	m := reAmbiguousRel.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "Multiple foreign keys exist between two nested tables; the compiler cannot pick one without a hint."
		res.RepairedQuery = "query {\n  parent {\n    child @through(column: \"<fk_column>\") {\n      id\n    }\n  }\n}"
		return
	}
	from, to, candCSV := m[1], m[2], m[3]
	candidates := splitAndTrim(candCSV)
	res.Diagnosis = fmt.Sprintf("Multiple foreign keys exist between %s and %s. Pick one with @through(column: \"...\"); for composite FKs, naming any column of the composite is enough.", from, to)
	res.RepairedQuery = fmt.Sprintf(
		"query {\n  %s {\n    %s @through(column: %q) {\n      id\n    }\n  }\n}\n# candidates: %s",
		from, to, candidates[0], strings.Join(candidates, ", "))
}

func fillDistinctJoinShapeArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindDistinctJoinShape
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "validate_where_clause"}

	m := reNestedShape.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "Cannot nest a join through a column outside the distinct/group-by; root the query at the dimension table instead. See catalog query_pattern items for metric_by_dimension."
		return
	}
	child, parent, parentCol, distinctCSV := m[1], m[2], m[3], m[4]
	res.Diagnosis = fmt.Sprintf(
		"Nested selection '%s' joins through '%s.%s', which is not in distinct: [%s]. The GROUP BY collapses '%s' away, leaving the join undefined. Root at the dimension instead (see catalog query_pattern items for metric_by_dimension) — or drop the nested join.",
		child, parent, parentCol, distinctCSV, parentCol)
	res.RepairedQuery = fmt.Sprintf(
		`# Option A (preferred): root at the dimension table, nest the fact at the leaf.
# This is the "metric_by_dimension" pattern — see query_catalog(where: { kind: { eq: "query_pattern" } }).
query {
  <dimension_table> {
    id
    name
    %s {
      <metric>: sum(expr: { mul: [<col_a>, <col_b>] })
    }
  }
}
# Option B: drop the nested '%s' join and only return the per-%s metric.
query {
  %s(distinct: [%s]) {
    %s
    <metric>: sum(...)
  }
}`,
		parent, child, distinctCSV, parent, distinctCSV, distinctCSV)
}

func fillPartitionFilterArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindPartitionFilter
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "validate_where_clause"}

	m := rePartitionReq.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "This table requires a filter on its partition or temporal column; add a where clause or pass unrestricted: true."
		return
	}
	table, col := m[1], m[2]
	res.Diagnosis = fmt.Sprintf("Table %q requires a filter on column %q. Add a where clause or pass unrestricted: true (use only when you genuinely want a full scan).", table, col)
	res.RepairedQuery = fmt.Sprintf(
		`# Option A (preferred): filter the temporal column.
query {
  %s(where: { %s: { gt: "2026-01-01" } }) {
    id
  }
}
# Option B: bypass the filter (full scan).
query {
  %s(unrestricted: true) {
    id
  }
}`, table, col, table)
}

// fillFieldNotOnTableArm covers the common case where a copy-pasted pattern
// uses placeholder column names (id, name) that don't exist on the actual
// table. Almost always means the agent took a canonical pattern verbatim
// instead of substituting in the table's real PK / name columns.
func fillFieldNotOnTableArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindFieldNotOnTable
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "validate_where_clause"}

	m := reFieldNotOnTable.FindStringSubmatch(errorMsg)
	field := "<field>"
	if m != nil {
		field = m[1]
	}
	res.Diagnosis = fmt.Sprintf(
		"Field '%s' isn't a column on the queried table. Most often this means a canonical pattern (e.g. metric_by_dimension) was copied verbatim — those templates use placeholder names like <pk_column> / <name_column> that need to be substituted with this table's real columns.",
		field)
	res.RepairedQuery = fmt.Sprintf(
		`# Use query_catalog/get_catalog_card to find the table's actual primary key and name
# columns, then substitute them where the pattern said '%s'.
query {
  <table> {
    <actual_pk_column>
    <actual_name_column>
    # ... the rest of your selection
  }
}`, field)
}

// isWrongDialectError flags unsupported aggregate arguments. The supported
// <table>_aggregate query form is compiled directly and is not an error arm.
func isWrongDialectError(errorMsg, _ string) bool {
	return reWrongDialectArg.MatchString(errorMsg)
}

func fillWrongDialectArm(res *FixQueryErrorResult, _ string, _ string) {
	res.Kind = fixKindWrongDialect
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "validate_where_clause"}

	tableHint := "<table>"
	res.Diagnosis = "Query used an unsupported `aggregation:`/`aggregate:` argument. Use GraphJin aggregate leaf fields or the supported Hasura-compatible `<table>_aggregate { aggregate { ... } }` query shape. Use query_catalog language/query-pattern items for the full grammar."

	res.RepairedQuery = fmt.Sprintf(
		`# Native GraphJin aggregate fields:
query {
  %s {
    count_<pk_column>
    sum_<numeric_col>
    avg_<numeric_col>
    revenue: sum(expr: { mul: [<col_a>, <col_b>] })
  }
}

# Hasura-compatible query aggregate syntax:
query {
  %s_aggregate {
    aggregate { count sum { <numeric_col> } }
  }
}`,
		tableHint, tableHint)
}

func fillUnknownRelArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindUnknownRelationship
	res.Diagnosis = "GraphJin has no relationship between the named tables. Confirm the join path before retrying."
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card"}
	_ = errorMsg
}

func fillTableNotFoundArm(res *FixQueryErrorResult) {
	res.Kind = fixKindTableNotFound
	res.Diagnosis = "Table name not found. Check spelling and namespace; some databases are case-sensitive."
	res.FollowUpTools = []string{"query_catalog", "get_catalog_entrypoints"}
}

func fillColumnNotFoundArm(res *FixQueryErrorResult) {
	res.Kind = fixKindColumnNotFound
	res.Diagnosis = "Column not found on the named table. If the column actually exists, this often signals an upstream relationship issue (the compiler emitted a CTE that drops the column). Check catalog table/column items; if the column is there, look for a distinct/group-by + nested-join shape mismatch."
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "fix_query_error"}
}

func fillOperatorArm(res *FixQueryErrorResult) {
	res.Kind = fixKindOperatorInvalid
	res.Diagnosis = "Invalid operator or operand shape."
	res.FollowUpTools = []string{"query_catalog", "validate_where_clause"}
}

func fillSyntaxArm(res *FixQueryErrorResult) {
	res.Kind = fixKindSyntaxParse
	res.Diagnosis = "GraphQL syntax or parse error."
	res.FollowUpTools = []string{"query_catalog"}
}

func fillPermissionArm(res *FixQueryErrorResult) {
	res.Kind = fixKindPermission
	res.Diagnosis = "Permission or access denied for the current role."
	res.FollowUpTools = []string{"audit_role_permissions", "list_saved_queries"}
}

func fillMutationNotAllowedArm(res *FixQueryErrorResult) {
	res.Kind = fixKindMutationNotAllowed
	res.Diagnosis = "Raw mutations are disabled in this deployment."
	res.FollowUpTools = []string{"list_saved_queries", "execute_saved_query"}
}

func fillVariableArm(res *FixQueryErrorResult) {
	res.Kind = fixKindVariable
	res.Diagnosis = "Variable reference is missing, mistyped, or unbound."
	res.FollowUpTools = []string{"query_catalog"}
}

func fillGenericArm(res *FixQueryErrorResult) {
	res.Kind = fixKindGeneric
	res.Diagnosis = "Unrecognized error class — fall back to schema verification and incremental query simplification."
	res.FollowUpTools = []string{"query_catalog", "get_catalog_card", "list_saved_queries"}
}

func splitAndTrim(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// renderFixMarkdown produces the human-readable markdown guide.
func renderFixMarkdown(query, errorMsg string, res *FixQueryErrorResult, analyticsMode bool) string {
	var sb strings.Builder
	sb.WriteString("# Query Error Analysis\n\n")
	if analyticsMode {
		sb.WriteString(analyticsModeBlockMarkdown())
	}

	sb.WriteString("## Error Message\n```\n")
	sb.WriteString(errorMsg)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Original Query\n```graphql\n")
	sb.WriteString(query)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Diagnosis\n")
	sb.WriteString(fmt.Sprintf("**Kind**: `%s`\n\n", res.Kind))
	sb.WriteString(res.Diagnosis)
	sb.WriteString("\n\n")

	if res.RepairedQuery != "" {
		sb.WriteString("## Repaired Query\n```graphql\n")
		sb.WriteString(res.RepairedQuery)
		sb.WriteString("\n```\n\n")
	}

	if len(res.FollowUpTools) > 0 {
		sb.WriteString("## Recommended Next Steps\n")
		for _, t := range res.FollowUpTools {
			sb.WriteString(fmt.Sprintf("- Call `%s`\n", t))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
