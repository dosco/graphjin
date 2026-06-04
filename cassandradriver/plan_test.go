package cassandradriver

import (
	"strings"
	"testing"
)

func usersNode() *Node {
	return &Node{
		Table:         "users",
		Columns:       []string{"id", "name", "email"},
		PartitionKeys: []string{"id"},
	}
}

// posts: partition key user_id, clustering created_at DESC then id ASC.
func postsNode() *Node {
	return &Node{
		Table:           "posts",
		Columns:         []string{"user_id", "created_at", "id", "title"},
		PartitionKeys:   []string{"user_id"},
		ClusteringKeys:  []string{"created_at", "id"},
		ClusteringOrder: map[string]string{"created_at": "desc", "id": "asc"},
	}
}

func TestPlanRead_EqOnPartitionKey(t *testing.T) {
	n := usersNode()
	n.Filters = []Filter{{Col: "id", Op: OpEq, Value: "u1"}}
	if _, err := PlanRead(n); err != nil {
		t.Fatalf("eq on partition key should be servable: %v", err)
	}
}

func TestPlanRead_PerOperatorRejections(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		want   string
	}{
		{"neq", Filter{Col: "id", Op: OpNeq, Value: "x"}, "!="},
		{"nin", Filter{Col: "id", Op: OpNin, Value: []any{"x"}}, "!="},
		{"like", Filter{Col: "name", Op: OpLike, Value: "a%"}, "LIKE"},
		{"ilike", Filter{Col: "name", Op: OpILike, Value: "a%"}, "LIKE"},
		{"isNull", Filter{Col: "email", Op: OpIsNull}, "NULL"},
		{"isNotNull", Filter{Col: "email", Op: OpIsNotNull}, "NULL"},
		{"or", Filter{Or: []Filter{{Col: "id", Op: OpEq, Value: "a"}, {Col: "id", Op: OpEq, Value: "b"}}}, "OR"},
		{"not", Filter{Not: []Filter{{Col: "id", Op: OpEq, Value: "a"}}}, "NOT"},
		{"nonKeyEq", Filter{Col: "name", Op: OpEq, Value: "a"}, "allow_filtering"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := usersNode()
			n.Filters = []Filter{tc.filter}
			_, err := PlanRead(n)
			if err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPlanRead_PartialPartitionKey(t *testing.T) {
	n := &Node{
		Table:         "events",
		Columns:       []string{"a", "b", "v"},
		PartitionKeys: []string{"a", "b"},
		Filters:       []Filter{{Col: "a", Op: OpEq, Value: "x"}},
	}
	_, err := PlanRead(n)
	if err == nil || !strings.Contains(err.Error(), "partial partition key") {
		t.Fatalf("composite partition key with only one column pinned must reject, got: %v", err)
	}
}

func TestPlanRead_RangeOnlyOnTerminalClustering(t *testing.T) {
	// Range on a non-terminal clustering column (created_at) while id is unrestricted.
	n := postsNode()
	n.Filters = []Filter{
		{Col: "user_id", Op: OpEq, Value: "u1"},
		{Col: "created_at", Op: OpGt, Value: "2020"},
		{Col: "id", Op: OpEq, Value: "p1"},
	}
	if _, err := PlanRead(n); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("range on non-terminal clustering column must reject, got: %v", err)
	}

	// Range on the terminal restricted clustering column is fine.
	ok := postsNode()
	ok.Filters = []Filter{
		{Col: "user_id", Op: OpEq, Value: "u1"},
		{Col: "created_at", Op: OpGt, Value: "2020"},
	}
	if _, err := PlanRead(ok); err != nil {
		t.Fatalf("range on terminal clustering column should be servable: %v", err)
	}
}

func TestPlanRead_RangeOnPartitionKeyRejected(t *testing.T) {
	n := usersNode()
	n.Filters = []Filter{{Col: "id", Op: OpGt, Value: "u1"}}
	if _, err := PlanRead(n); err == nil || !strings.Contains(err.Error(), "partition key") {
		t.Fatalf("range on partition key must reject, got: %v", err)
	}
}

func TestPlanRead_ClusteringPrefixGap(t *testing.T) {
	// Restrict id (clustering pos 1) without created_at (pos 0).
	n := postsNode()
	n.Filters = []Filter{
		{Col: "user_id", Op: OpEq, Value: "u1"},
		{Col: "id", Op: OpEq, Value: "p1"},
	}
	if _, err := PlanRead(n); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("clustering restriction with a prefix gap must reject, got: %v", err)
	}
}

func TestPlanRead_OrderBy(t *testing.T) {
	base := func() *Node {
		n := postsNode()
		n.Filters = []Filter{{Col: "user_id", Op: OpEq, Value: "u1"}}
		return n
	}

	// Forward clustering order.
	n := base()
	n.OrderBy = []OrderBy{{Col: "created_at", Order: "desc"}, {Col: "id", Order: "asc"}}
	if p, err := PlanRead(n); err != nil || p.Reversed {
		t.Fatalf("clustering-order ORDER BY should be servable and not reversed: %v", err)
	}

	// Fully reversed.
	rev := base()
	rev.OrderBy = []OrderBy{{Col: "created_at", Order: "asc"}, {Col: "id", Order: "desc"}}
	if p, err := PlanRead(rev); err != nil || !p.Reversed {
		t.Fatalf("fully reversed ORDER BY should be servable and reversed: %v", err)
	}

	// Non-clustering column.
	bad := base()
	bad.OrderBy = []OrderBy{{Col: "title", Order: "asc"}}
	if _, err := PlanRead(bad); err == nil || !strings.Contains(err.Error(), "clustering") {
		t.Fatalf("ORDER BY on non-clustering column must reject, got: %v", err)
	}

	// nulls variant.
	nulls := base()
	nulls.OrderBy = []OrderBy{{Col: "created_at", Order: "desc_nulls_last"}}
	if _, err := PlanRead(nulls); err == nil || !strings.Contains(err.Error(), "NULLS") {
		t.Fatalf("nulls ORDER BY must reject, got: %v", err)
	}

	// Mixed directions.
	mixed := base()
	mixed.OrderBy = []OrderBy{{Col: "created_at", Order: "desc"}, {Col: "id", Order: "desc"}}
	if _, err := PlanRead(mixed); err == nil || !strings.Contains(err.Error(), "direction") {
		t.Fatalf("mixed-direction ORDER BY must reject, got: %v", err)
	}
}

func TestPlanRead_Paging(t *testing.T) {
	n := usersNode()
	n.Filters = []Filter{{Col: "id", Op: OpEq, Value: "u1"}}
	n.Paging = PagingOffset
	if _, err := PlanRead(n); err == nil || !strings.Contains(err.Error(), "offset") {
		t.Fatalf("offset paging must reject, got: %v", err)
	}

	bw := usersNode() // no clustering keys -> backward not possible
	bw.Filters = []Filter{{Col: "id", Op: OpEq, Value: "u1"}}
	bw.Paging = PagingBackward
	if _, err := PlanRead(bw); err == nil || !strings.Contains(err.Error(), "backward") {
		t.Fatalf("backward paging without clustering keys must reject, got: %v", err)
	}
}

func TestPlanRead_AllowFiltering(t *testing.T) {
	n := usersNode()
	n.AllowFiltering = true
	n.Filters = []Filter{{Col: "name", Op: OpEq, Value: "amit"}}
	p, err := PlanRead(n)
	if err != nil {
		t.Fatalf("allow_filtering should permit non-key eq: %v", err)
	}
	if !p.AllowFiltering {
		t.Fatalf("plan should carry AllowFiltering")
	}

	// Even with allow_filtering, OR/LIKE/IS NULL stay rejected.
	for _, f := range []Filter{
		{Or: []Filter{{Col: "id", Op: OpEq, Value: "a"}, {Col: "id", Op: OpEq, Value: "b"}}},
		{Col: "name", Op: OpLike, Value: "a%"},
		{Col: "email", Op: OpIsNull},
	} {
		nn := usersNode()
		nn.AllowFiltering = true
		nn.Filters = []Filter{f}
		if _, err := PlanRead(nn); err == nil {
			t.Fatalf("allow_filtering must not rescue %+v", f)
		}
	}
}

func TestPlanMutation_FullPK(t *testing.T) {
	// Insert missing a clustering key.
	ins := &Mutation{
		Type:           OpInsert,
		Table:          "posts",
		PartitionKeys:  []string{"user_id"},
		ClusteringKeys: []string{"id"},
		Set:            []Assignment{{Col: "user_id", Value: "u1"}, {Col: "title", Value: "hi"}},
	}
	if err := PlanMutation(ins); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("insert missing clustering key must reject, got: %v", err)
	}

	// Update WHERE missing a key column.
	upd := &Mutation{
		Type:           OpUpdate,
		Table:          "posts",
		PartitionKeys:  []string{"user_id"},
		ClusteringKeys: []string{"id"},
		Set:            []Assignment{{Col: "title", Value: "x"}},
		Where:          []Filter{{Col: "user_id", Op: OpEq, Value: "u1"}},
	}
	if err := PlanMutation(upd); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("update with partial PK in WHERE must reject, got: %v", err)
	}
}

func TestPlanMutation_CounterCannotInsert(t *testing.T) {
	m := &Mutation{
		Type:          OpInsert,
		Table:         "counts",
		PartitionKeys: []string{"id"},
		CounterCols:   []string{"hits"},
		Set:           []Assignment{{Col: "id", Value: "x"}, {Col: "hits", Value: 1}},
	}
	if err := PlanMutation(m); err == nil || !strings.Contains(err.Error(), "counter") {
		t.Fatalf("counter column in INSERT must reject, got: %v", err)
	}
}

func TestPlanMutation_ConnectRejected(t *testing.T) {
	m := &Mutation{Type: "connect", Table: "x", PartitionKeys: []string{"id"}}
	if err := PlanMutation(m); err == nil || !strings.Contains(err.Error(), "nested/connect") {
		t.Fatalf("connect mutation must reject, got: %v", err)
	}
}
