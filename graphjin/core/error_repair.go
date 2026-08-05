package core

import (
	"fmt"
	"regexp"
	"strings"
)

type ErrorRepair struct {
	Kind          string   `json:"kind"`
	Diagnosis     string   `json:"diagnosis"`
	RepairedQuery string   `json:"repaired_query,omitempty"`
	Next          []string `json:"next,omitempty"`
}

const (
	repairKindMultiFKAmbiguity    = "multi_fk_ambiguity"
	repairKindDistinctJoinShape   = "distinct_aggregate_nested_join_shape"
	repairKindPartitionFilter     = "partition_filter_required"
	repairKindUnknownRelationship = "unknown_relationship"
	repairKindTableNotFound       = "table_not_found"
	repairKindColumnNotFound      = "column_not_found"
	repairKindFieldNotOnTable     = "field_not_on_table"
	repairKindAnalyticsDirective  = "analytics_directive"
	repairKindWrongDialect        = "wrong_dialect"
	repairKindOperatorInvalid     = "operator_or_syntax_invalid"
	repairKindSyntaxParse         = "syntax_or_parse_error"
	repairKindPermission          = "permission_denied"
	repairKindMutationNotAllowed  = "mutation_not_allowed"
	repairKindVariable            = "variable_error"
	repairKindGeneric             = "generic"
)

var (
	repairAmbiguousRel    = regexp.MustCompile(`ambiguous relationship\s+(\S+)\s*->\s*(\S+):\s*multiple foreign keys\s*\(([^)]+)\)`)
	repairNestedShape     = regexp.MustCompile(`nested selection '([^']+)' joins through parent column '([^']+)\.([^']+)', which is not in distinct: \[([^\]]+)\]`)
	repairPartitionReq    = regexp.MustCompile(`table\s+"([^"]+)"\s+requires a filter on (?:partition|temporal) column\s+"([^"]+)"`)
	repairFieldNotOnTable = regexp.MustCompile(`field '([^']+)' is not a column or a function`)
	repairWrongDialectArg = regexp.MustCompile(`unknown argument\s+['"` + "`" + `]?(aggregation|aggregate)['"` + "`" + `]?`)
)

func BuildGraphJinErrorRepair(query, errorMsg string) ErrorRepair {
	res := ErrorRepair{Kind: repairKindGeneric}
	errLower := strings.ToLower(errorMsg)

	switch {
	case repairAmbiguousRel.MatchString(errorMsg):
		fillRepairMultiFK(&res, errorMsg)
	case repairNestedShape.MatchString(errorMsg):
		fillRepairDistinctJoinShape(&res, errorMsg)
	case repairPartitionReq.MatchString(errorMsg):
		fillRepairPartitionFilter(&res, errorMsg)
	case isRepairAnalyticsDirective(errLower):
		res.Kind = repairKindAnalyticsDirective
		res.Diagnosis = "Use GraphJin analytics directives on real columns for reporting metrics. Use @running or @moving for row-level aggregates, @previous/@next for period comparisons, and @rank/@denseRank/@rowNumber for ranking."
		res.Next = []string{"query_catalog"}
	case repairFieldNotOnTable.MatchString(errorMsg):
		field := "<field>"
		if m := repairFieldNotOnTable.FindStringSubmatch(errorMsg); m != nil {
			field = m[1]
		}
		res.Kind = repairKindFieldNotOnTable
		res.Diagnosis = fmt.Sprintf("Field %q is not a column on the queried table. Check the catalog for the table's real columns before retrying.", field)
		res.Next = []string{"query_catalog", "validate_where_clause"}
	case isRepairWrongDialect(errorMsg, query):
		fillRepairWrongDialect(&res, errorMsg, query)
	case strings.Contains(errLower, "relationship not found"):
		res.Kind = repairKindUnknownRelationship
		res.Diagnosis = "GraphJin has no relationship between the named tables. Confirm the join path before retrying."
		res.Next = []string{"query_catalog"}
	case strings.Contains(errLower, "table") && (strings.Contains(errLower, "not found") || strings.Contains(errLower, "unknown")):
		res.Kind = repairKindTableNotFound
		res.Diagnosis = "Table name not found. Check spelling, database, schema, and role-visible catalog rows."
		res.Next = []string{"query_catalog"}
	case strings.Contains(errLower, "column") && (strings.Contains(errLower, "not found") || strings.Contains(errLower, "unknown") || strings.Contains(errLower, "does not exist")):
		res.Kind = repairKindColumnNotFound
		res.Diagnosis = "Column not found on the named table. Check catalog column rows and confirm the query shape did not drop the column in a distinct/group-by projection."
		res.Next = []string{"query_catalog", "validate_where_clause"}
	case strings.Contains(errLower, "operator") || strings.Contains(errLower, "invalid"):
		res.Kind = repairKindOperatorInvalid
		res.Diagnosis = "Invalid operator or operand shape."
		res.Next = []string{"query_catalog", "validate_where_clause"}
	case strings.Contains(errLower, "syntax") || strings.Contains(errLower, "parse"):
		res.Kind = repairKindSyntaxParse
		res.Diagnosis = "GraphQL syntax or parse error."
		res.Next = []string{"query_catalog"}
	case strings.Contains(errLower, "permission") || strings.Contains(errLower, "access") || strings.Contains(errLower, "denied"):
		res.Kind = repairKindPermission
		res.Diagnosis = "Permission or access denied for the current role."
		res.Next = []string{"query_catalog", "execute_saved_query"}
	case strings.Contains(errLower, "mutation") && strings.Contains(errLower, "not allowed"):
		res.Kind = repairKindMutationNotAllowed
		res.Diagnosis = "Mutations are disabled or blocked for this surface."
		res.Next = []string{"query_catalog", "execute_saved_query"}
	case strings.Contains(errLower, "variable") || strings.Contains(errLower, "$"):
		res.Kind = repairKindVariable
		res.Diagnosis = "Variable reference is missing, mistyped, or unbound."
		res.Next = []string{"query_catalog"}
	default:
		res.Diagnosis = "Unrecognized error class. Verify schema/catalog items and simplify the query incrementally."
		res.Next = []string{"query_catalog"}
	}

	return res
}

func (r ErrorRepair) Known() bool {
	return r.Kind != "" && r.Kind != repairKindGeneric
}

func fillRepairMultiFK(res *ErrorRepair, errorMsg string) {
	res.Kind = repairKindMultiFKAmbiguity
	res.Next = []string{"query_catalog"}
	m := repairAmbiguousRel.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "Multiple foreign keys exist between two nested tables; the compiler cannot pick one without a hint."
		return
	}
	candidates := splitRepairCSV(m[3])
	res.Diagnosis = fmt.Sprintf("Multiple foreign keys exist between %s and %s. Pick one with @through(column: \"...\").", m[1], m[2])
	if len(candidates) != 0 {
		res.RepairedQuery = fmt.Sprintf("query {\n  %s {\n    %s @through(column: %q) { id }\n  }\n}", m[1], m[2], candidates[0])
	}
}

func fillRepairDistinctJoinShape(res *ErrorRepair, errorMsg string) {
	res.Kind = repairKindDistinctJoinShape
	res.Next = []string{"query_catalog", "validate_where_clause"}
	m := repairNestedShape.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "Cannot nest a join through a column outside the distinct/group-by; root the query at the dimension table instead."
		return
	}
	res.Diagnosis = fmt.Sprintf("Nested selection %q joins through %s.%s, which is not in distinct: [%s]. Root at the dimension table or drop the nested join.", m[1], m[2], m[3], m[4])
}

func fillRepairPartitionFilter(res *ErrorRepair, errorMsg string) {
	res.Kind = repairKindPartitionFilter
	res.Next = []string{"query_catalog", "validate_where_clause"}
	m := repairPartitionReq.FindStringSubmatch(errorMsg)
	if m == nil {
		res.Diagnosis = "This table requires a filter on its partition or temporal column; add a where clause or pass unrestricted: true."
		return
	}
	res.Diagnosis = fmt.Sprintf("Table %q requires a filter on column %q. Add a where clause or pass unrestricted: true only when a full scan is intended.", m[1], m[2])
	res.RepairedQuery = fmt.Sprintf("query {\n  %s(where: { %s: { gt: \"2026-01-01\" } }) { id }\n}", m[1], m[2])
}

func fillRepairWrongDialect(res *ErrorRepair, _ string, _ string) {
	res.Kind = repairKindWrongDialect
	res.Next = []string{"query_catalog", "validate_where_clause"}
	res.Diagnosis = "Query used an unsupported aggregate argument. Use GraphJin aggregate leaf fields or the supported Hasura-compatible <table>_aggregate query shape."
}

func isRepairAnalyticsDirective(errLower string) bool {
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

func isRepairWrongDialect(errorMsg, _ string) bool {
	return repairWrongDialectArg.MatchString(errorMsg)
}

func splitRepairCSV(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
