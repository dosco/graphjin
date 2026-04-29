package serv

// Three canonical query shapes; Wrong/Right contrast is load-bearing for small-model pattern matching.

type QueryPattern struct {
	Name         string `json:"name"`
	Title        string `json:"title"`
	Question     string `json:"question"`
	Rule         string `json:"rule"`
	Why          string `json:"why"`
	WrongExample string `json:"wrong_example,omitempty"`
	WrongReason  string `json:"wrong_reason,omitempty"`
	RightExample string `json:"right_example"`
}

func canonicalQueryPatterns() []QueryPattern {
	return []QueryPattern{
		{
			Name:     "metric_by_dimension",
			Title:    "Metric by dimension",
			Question: "Examples: revenue by category, users by country, orders by region.",
			Rule:     "Root at the dimension table. Nest down to the fact table. Aggregate at the LEAF.",
			Why:      "GraphJin has no GROUP BY at non-root levels. Bucketing happens by choice of root, not by distinct: on a nested FK. distinct: dedupes rows; it does not bucket.",
			WrongExample: `query {
  fact_table(distinct: [dimension_fk]) {
    sum_metric
  }
}`,
			WrongReason:  "distinct on a fact-table FK dedupes rows but does not produce per-dimension aggregates. The compiler will reject this when the nested join references a column outside the distinct list.",
			RightExample: `query {
  dimension_table {
    id
    name
    fact_table {
      sum_metric
    }
  }
}`,
		},
		{
			Name:     "time_series",
			Title:    "Time series",
			Question: "Examples: revenue per month, signups per day, sessions per hour.",
			Rule:     "Root at the fact table. distinct: [date_col]. Aggregate at the same level.",
			Why:      "Time buckets are produced by the date column itself; there's no separate dimension table to root at. distinct on the date column groups rows by bucket; the aggregate runs over each group.",
			RightExample: `query {
  fact_table(distinct: [date_col], order_by: { date_col: asc }) {
    date_col
    sum_metric
  }
}`,
		},
		{
			Name:     "top_n",
			Title:    "Top-N entities by metric",
			Question: "Examples: top 10 customers by spend, top 5 products by revenue.",
			Rule:     "Root at the fact table. distinct: [entity_fk]. order_by the aggregate desc + limit.",
			Why:      "Top-N is a ranked-cut over per-entity aggregates. Rooting at the fact and grouping by the entity FK gives one row per entity; ordering by the aggregate alias and limiting yields the top N.",
			WrongExample: `query {
  fact_table(distinct: [entity_fk], order_by: { sum_amount: desc }, limit: 10) {
    entity_fk
    sum_amount
    entity_table { name }
  }
}
# (the nested entity_table join here would fail — it joins through
# entity_fk which IS in distinct, so this specific shape compiles, but
# nesting any OTHER join through a non-distinct FK would not)`,
			WrongReason:  "Nesting through a non-distinct FK column is rejected (a column dropped by GROUP BY cannot be a join key). Only nest joins whose FK column is itself in distinct.",
			RightExample: `query {
  fact_table(distinct: [customer_id], order_by: { sum_amount: desc }, limit: 10) {
    customer_id
    sum_amount: sum(amount)
  }
}`,
		},
	}
}
