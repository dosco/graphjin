package catalog

type Feature struct {
	ID             string
	Kind           string
	Name           string
	Scope          string
	Summary        string
	Arguments      []FeatureArg
	AppliesTo      []string
	DialectSupport string
	Examples       []string
	CommonMistakes []string
	ReplacementFor []string
	SuggestedNext  []string
}

type FeatureArg struct {
	Name     string
	Type     string
	Required bool
	Values   []string
	Summary  string
}

func LanguageFeatures() []Feature {
	return cloneFeatures(languageFeatures)
}

func cloneFeatures(in []Feature) []Feature {
	out := make([]Feature, len(in))
	copy(out, in)
	for i := range out {
		out[i].Arguments = append([]FeatureArg(nil), in[i].Arguments...)
		out[i].AppliesTo = append([]string(nil), in[i].AppliesTo...)
		out[i].Examples = append([]string(nil), in[i].Examples...)
		out[i].CommonMistakes = append([]string(nil), in[i].CommonMistakes...)
		out[i].ReplacementFor = append([]string(nil), in[i].ReplacementFor...)
		out[i].SuggestedNext = append([]string(nil), in[i].SuggestedNext...)
	}
	return out
}

var languageFeatures = []Feature{
	{
		ID:      "directive.object",
		Kind:    "directive",
		Name:    "@object",
		Scope:   "selector",
		Summary: "Return a single object instead of an array and implicitly limit the selector to one row.",
		Examples: []string{
			`{ users(where: { id: { eq: $id } }) @object { id email } }`,
		},
		SuggestedNext: []string{"validate_where_clause", "execute_graphql"},
	},
	{
		ID:      "directive.through",
		Kind:    "directive",
		Name:    "@through",
		Scope:   "selector",
		Summary: "Disambiguate many-to-many joins or multiple foreign keys by naming the join table or FK column.",
		Arguments: []FeatureArg{
			{Name: "table", Type: "string", Summary: "Intermediate join table."},
			{Name: "column", Type: "string", Summary: "Foreign key column to follow."},
		},
		Examples: []string{
			`{ billofmaterials { product @through(column: "componentid") { name } } }`,
		},
		CommonMistakes: []string{"Guessing the FK path instead of asking the catalog for relationship items."},
		SuggestedNext:  []string{"query_catalog", "query_catalog(id)"},
	},
	{
		ID:        "directive.schema",
		Kind:      "directive",
		Name:      "@schema",
		Scope:     "selector",
		Summary:   "Route a selector to a specific database schema when names are ambiguous.",
		Arguments: []FeatureArg{{Name: "name", Type: "string", Required: true}},
		Examples:  []string{`{ users @schema(name: "tenant_a") { id email } }`},
	},
	{
		ID:        "directive.database",
		Kind:      "directive",
		Name:      "@database",
		Scope:     "schema",
		Summary:   "Assign a GraphQL schema type/table to a named configured database.",
		Arguments: []FeatureArg{{Name: "name", Type: "string", Required: true}},
		Examples:  []string{`type users @database(name: "warehouse") { id: ID! }`},
	},
	{
		ID:      "directive.include",
		Kind:    "directive",
		Name:    "@include",
		Scope:   "selector_or_field",
		Summary: "Conditionally include a selector or field by role or variable.",
		Arguments: []FeatureArg{
			{Name: "ifRole", Type: "string", Summary: "Include when the current role matches."},
			{Name: "ifVar", Type: "string", Summary: "Include when the named variable is true."},
		},
	},
	{
		ID:      "directive.skip",
		Kind:    "directive",
		Name:    "@skip",
		Scope:   "selector_or_field",
		Summary: "Conditionally skip a selector or field by role or variable.",
		Arguments: []FeatureArg{
			{Name: "ifRole", Type: "string", Summary: "Skip when the current role matches."},
			{Name: "ifVar", Type: "string", Summary: "Skip when the named variable is true."},
		},
	},
	{
		ID:      "directive.not_related",
		Kind:    "directive",
		Name:    "@notRelated",
		Scope:   "selector",
		Summary: "Disable automatic relationship detection for a selector.",
	},
	{
		ID:      "directive.cache_control",
		Kind:    "directive",
		Name:    "@cacheControl",
		Scope:   "operation",
		Summary: "Set cache behavior for a query operation.",
		Arguments: []FeatureArg{
			{Name: "maxAge", Type: "integer", Summary: "TTL in seconds."},
			{Name: "scope", Type: "string", Summary: "Cache scope."},
		},
	},
	{
		ID:             "directive.running",
		Kind:           "directive",
		Name:           "@running",
		Scope:          "field",
		Summary:        "Create a running aggregate while keeping each input row visible.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments: []FeatureArg{
			{Name: "aggregate", Type: "enum", Required: true, Values: []string{"sum", "avg", "count", "min", "max"}},
			{Name: "by", Type: "column|string|string[]", Summary: "Optional partition columns."},
			{Name: "orderBy", Type: "order_by", Required: true, Summary: "Deterministic row ordering."},
			{Name: "order", Type: "asc|desc", Summary: "Shorthand for ordering by the annotated field."},
		},
		AppliesTo: []string{"numeric_column", "ordered_rows"},
		Examples:  []string{`{ orders { account_id month total running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc }) } }`},
		CommonMistakes: []string{
			"Using plain aggregates when the original rows must remain visible.",
			"Omitting orderBy/order, which makes reporting rows nondeterministic.",
		},
		SuggestedNext: []string{"query_catalog", "validate_where_clause", "execute_graphql"},
	},
	{
		ID:             "directive.moving",
		Kind:           "directive",
		Name:           "@moving",
		Scope:          "field",
		Summary:        "Create a trailing moving aggregate over a fixed number of rows.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments: []FeatureArg{
			{Name: "aggregate", Type: "enum", Required: true, Values: []string{"sum", "avg", "count", "min", "max"}},
			{Name: "rows", Type: "integer", Required: true, Summary: "Positive trailing row count."},
			{Name: "by", Type: "column|string|string[]", Summary: "Optional partition columns."},
			{Name: "orderBy", Type: "order_by", Required: true},
			{Name: "order", Type: "asc|desc"},
		},
		AppliesTo:      []string{"numeric_column", "ordered_rows"},
		Examples:       []string{`{ orders { account_id month total moving_avg_total: total @moving(aggregate: avg, rows: 6, by: "account_id", orderBy: { month: asc }) } }`},
		SuggestedNext:  []string{"query_catalog", "validate_where_clause", "execute_graphql"},
		CommonMistakes: []string{"Using @moving without rows."},
	},
	{
		ID:             "directive.previous",
		Kind:           "directive",
		Name:           "@previous",
		Scope:          "field",
		Summary:        "Return the previous row value for period comparisons.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments: []FeatureArg{
			{Name: "by", Type: "column|string|string[]", Summary: "Optional partition columns."},
			{Name: "orderBy", Type: "order_by", Required: true},
			{Name: "order", Type: "asc|desc"},
		},
		Examples: []string{`{ orders { account_id month total previous_total: total @previous(by: "account_id", orderBy: { month: asc }) } }`},
	},
	{
		ID:             "directive.next",
		Kind:           "directive",
		Name:           "@next",
		Scope:          "field",
		Summary:        "Return the next row value for period comparisons.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments: []FeatureArg{
			{Name: "by", Type: "column|string|string[]"},
			{Name: "orderBy", Type: "order_by", Required: true},
			{Name: "order", Type: "asc|desc"},
		},
	},
	{
		ID:             "directive.first",
		Kind:           "directive",
		Name:           "@first",
		Scope:          "field",
		Summary:        "Return the first value in an ordered group.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments:      []FeatureArg{{Name: "by", Type: "column|string|string[]"}, {Name: "orderBy", Type: "order_by", Required: true}, {Name: "order", Type: "asc|desc"}},
	},
	{
		ID:             "directive.last",
		Kind:           "directive",
		Name:           "@last",
		Scope:          "field",
		Summary:        "Return the last value in an ordered group.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments:      []FeatureArg{{Name: "by", Type: "column|string|string[]"}, {Name: "orderBy", Type: "order_by", Required: true}, {Name: "order", Type: "asc|desc"}},
	},
	{
		ID:             "directive.rank",
		Kind:           "directive",
		Name:           "@rank",
		Scope:          "field",
		Summary:        "Rank rows inside an optional group.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments:      []FeatureArg{{Name: "by", Type: "column|string|string[]"}, {Name: "orderBy", Type: "order_by"}, {Name: "order", Type: "asc|desc", Required: true}},
		Examples:       []string{`{ orders { account_id total rank_by_total: total @rank(by: "account_id", order: desc) } }`},
	},
	{
		ID:             "directive.dense_rank",
		Kind:           "directive",
		Name:           "@denseRank",
		Scope:          "field",
		Summary:        "Dense-rank rows inside an optional group.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments:      []FeatureArg{{Name: "by", Type: "column|string|string[]"}, {Name: "orderBy", Type: "order_by"}, {Name: "order", Type: "asc|desc", Required: true}},
	},
	{
		ID:             "directive.row_number",
		Kind:           "directive",
		Name:           "@rowNumber",
		Scope:          "field",
		Summary:        "Number rows inside an optional ordered group.",
		DialectSupport: "SQL databases with window-function support. MongoDB is unsupported.",
		Arguments:      []FeatureArg{{Name: "by", Type: "column|string|string[]"}, {Name: "orderBy", Type: "order_by"}, {Name: "order", Type: "asc|desc", Required: true}},
	},
	{
		ID:             "deprecated.window",
		Kind:           "deprecated_feature",
		Name:           "@window",
		Scope:          "field",
		Summary:        "Deprecated raw window directive. Use GraphJin analytics directives instead.",
		ReplacementFor: []string{"@window"},
		CommonMistakes: []string{"Using partition/frame/lag_/lead_ syntax copied from older examples."},
		Examples:       []string{`running_total: total @running(aggregate: sum, by: "account_id", orderBy: { month: asc })`},
		SuggestedNext:  []string{"query_catalog", "graphjin_repair"},
	},
	{
		ID:      "operator.filters",
		Kind:    "operator_set",
		Name:    "filter operators",
		Scope:   "where",
		Summary: "GraphJin where operators are typed by column shape.",
		Examples: []string{
			`where: { price: { gt: 50 }, name: { ilike: "%shoe%" }, id: { in: [1, 2, 3] } }`,
		},
		CommonMistakes: []string{
			"in/nin require arrays even for one value.",
			"Numeric and boolean operators require numeric/boolean values, not strings.",
		},
		SuggestedNext: []string{"query_catalog", "validate_where_clause"},
	},
	{
		ID:      "pattern.grouped_summary",
		Kind:    "query_pattern",
		Name:    "grouped summary",
		Scope:   "query",
		Summary: "Use distinct plus aggregate fields for one-row-per-group summaries; group_by does not exist.",
		Examples: []string{
			`{ orders(distinct: [account_id]) { account_id count_id sum_total } }`,
		},
		CommonMistakes: []string{"Trying to use group_by."},
	},
	{
		ID:      "pattern.expression_aggregate",
		Kind:    "query_pattern",
		Name:    "expression aggregate",
		Scope:   "query",
		Summary: "Use aggregate expr trees for arithmetic metrics such as revenue, margins, ratios, and weighted sums.",
		Examples: []string{
			`{ order_items { revenue: sum(expr: { mul: [unit_price, quantity] }) } }`,
		},
	},
	{
		ID:      "pattern.recursive",
		Kind:    "query_pattern",
		Name:    "recursive relationship query",
		Scope:   "query",
		Summary: "Use find: parents or find: children on self-referencing tables.",
		Examples: []string{
			`{ comments(find: "children", where: { id: { eq: $id } }) { id body } }`,
		},
	},
	{
		ID:      "mutation.basic",
		Kind:    "mutation_pattern",
		Name:    "insert/update/upsert/delete",
		Scope:   "mutation",
		Summary: "GraphJin mutations use insert, update, upsert, and delete arguments on table selectors.",
		Examples: []string{
			`mutation { users(insert: { email: "a@example.com" }) { id email } }`,
			`mutation { users(id: $id, update: { name: "Ada" }) { id name } }`,
		},
	},
	{
		ID:      "mutation.insert_conflict_get",
		Kind:    "mutation_pattern",
		Name:    "insert or get existing row",
		Scope:   "mutation",
		Summary: "Use on_conflict: get on a single insert to return the unchanged existing row for exactly one inferred supplied unique key. This is not an upsert and never updates the row.",
		Examples: []string{
			`mutation { users(insert: { email: "a@example.com", name: "Submitted" }, on_conflict: get) { id email name } }`,
		},
	},
}
