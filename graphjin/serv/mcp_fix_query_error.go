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
	case reFieldNotOnTable.MatchString(errorMsg):
		fillFieldNotOnTableArm(&res, errorMsg)
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

func fillMultiFKArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindMultiFKAmbiguity
	res.FollowUpTools = []string{"describe_table", "get_table_sample"}

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
	// get_query_syntax leads — most agents call it before authoring and
	// it now carries the same QueryPatterns content. get_workflow_guide
	// is listed too for the workflow-using minority.
	res.FollowUpTools = []string{"get_query_syntax", "get_workflow_guide", "get_table_sample", "describe_table"}

	m := reNestedShape.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "Cannot nest a join through a column outside the distinct/group-by; root the query at the dimension table instead. See get_query_syntax → patterns → metric_by_dimension."
		return
	}
	child, parent, parentCol, distinctCSV := m[1], m[2], m[3], m[4]
	res.Diagnosis = fmt.Sprintf(
		"Nested selection '%s' joins through '%s.%s', which is not in distinct: [%s]. The GROUP BY collapses '%s' away, leaving the join undefined. Root at the dimension instead (see get_query_syntax → patterns → metric_by_dimension) — or drop the nested join.",
		child, parent, parentCol, distinctCSV, parentCol)
	res.RepairedQuery = fmt.Sprintf(
		`# Option A (preferred): root at the dimension table, nest the fact at the leaf.
# This is the "metric_by_dimension" pattern — see get_query_syntax.patterns.
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
	res.FollowUpTools = []string{"get_table_sample", "describe_table"}

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
	res.FollowUpTools = []string{"describe_table", "get_table_sample", "get_query_syntax"}

	m := reFieldNotOnTable.FindStringSubmatch(errorMsg)
	field := "<field>"
	if m != nil {
		field = m[1]
	}
	res.Diagnosis = fmt.Sprintf(
		"Field '%s' isn't a column on the queried table. Most often this means a canonical pattern (e.g. metric_by_dimension) was copied verbatim — those templates use placeholder names like <pk_column> / <name_column> that need to be substituted with this table's real columns.",
		field)
	res.RepairedQuery = fmt.Sprintf(
		`# Use describe_table to find the table's actual primary key and name
# columns, then substitute them where the pattern said '%s'.
query {
  <table> {
    <actual_pk_column>
    <actual_name_column>
    # ... the rest of your selection
  }
}`, field)
}

func fillUnknownRelArm(res *FixQueryErrorResult, errorMsg string) {
	res.Kind = fixKindUnknownRelationship
	res.Diagnosis = "GraphJin has no relationship between the named tables. Confirm the join path before retrying."
	res.FollowUpTools = []string{"find_path", "get_table_sample", "describe_table"}
	_ = errorMsg
}

func fillTableNotFoundArm(res *FixQueryErrorResult) {
	res.Kind = fixKindTableNotFound
	res.Diagnosis = "Table name not found. Check spelling and namespace; some databases are case-sensitive."
	res.FollowUpTools = []string{"list_tables", "list_namespaces"}
}

func fillColumnNotFoundArm(res *FixQueryErrorResult) {
	res.Kind = fixKindColumnNotFound
	res.Diagnosis = "Column not found on the named table. If the column actually exists, this often signals an upstream relationship issue (the compiler emitted a CTE that drops the column). Check describe_table; if the column is there, look for a distinct/group-by + nested-join shape mismatch."
	res.FollowUpTools = []string{"describe_table", "get_table_sample", "fix_query_error"}
}

func fillOperatorArm(res *FixQueryErrorResult) {
	res.Kind = fixKindOperatorInvalid
	res.Diagnosis = "Invalid operator or operand shape."
	res.FollowUpTools = []string{"get_query_syntax", "validate_where_clause"}
}

func fillSyntaxArm(res *FixQueryErrorResult) {
	res.Kind = fixKindSyntaxParse
	res.Diagnosis = "GraphQL syntax or parse error."
	res.FollowUpTools = []string{"get_query_syntax"}
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
	res.FollowUpTools = []string{"get_query_syntax"}
}

func fillGenericArm(res *FixQueryErrorResult) {
	res.Kind = fixKindGeneric
	res.Diagnosis = "Unrecognized error class — fall back to schema verification and incremental query simplification."
	res.FollowUpTools = []string{"list_tables", "describe_table", "get_query_syntax", "list_saved_queries"}
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
