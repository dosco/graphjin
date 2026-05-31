package sdata

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func testPK(table string) DBColumn {
	return DBColumn{
		Schema:     "public",
		Table:      table,
		Name:       "id",
		Type:       "bigint",
		NotNull:    true,
		PrimaryKey: true,
		UniqueKey:  true,
	}
}

func testFK(table, name, fkeyTable string) DBColumn {
	return DBColumn{
		Schema:     "public",
		Table:      table,
		Name:       name,
		Type:       "bigint",
		FKeySchema: "public",
		FKeyTable:  fkeyTable,
		FKeyCol:    "id",
	}
}

func testSchema(t testing.TB, cols ...DBColumn) *DBSchema {
	t.Helper()

	di := NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)
	schema, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}
	return schema
}

func tableNodeID(t testing.TB, s *DBSchema, name string) int32 {
	t.Helper()

	ni, ok := s.tindex["public:"+name]
	if !ok {
		t.Fatalf("missing table node for %s", name)
	}
	return ni.nodeID
}

func tableNames(t testing.TB, s *DBSchema, ids []int32) []string {
	t.Helper()

	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if id < 0 || int(id) >= len(s.tables) {
			t.Fatalf("invalid table id %d", id)
		}
		names = append(names, s.tables[id].Name)
	}
	slices.Sort(names)
	return names
}

func TestFindPathUsesWeightedEdges(t *testing.T) {
	s := testSchema(t,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"), testFK("b", "c_id", "c"),
		testPK("c"),
	)

	a, err := s.Find("public", "a")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Find("public", "c")
	if err != nil {
		t.Fatal(err)
	}
	cID, ok := c.getColumn("id")
	if !ok {
		t.Fatal("missing c.id")
	}

	_, err = s.addToGraph(a, DBColumn{
		Schema: "public",
		Table:  "a",
		Name:   "c_remote_id",
		Type:   "bigint",
	}, c, cID, RelRemote)
	if err != nil {
		t.Fatal(err)
	}

	paths, err := s.FindPath("a", "c", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected lower-cost two-hop path, got %d hops: %+v", len(paths), paths)
	}
	if paths[0].LC.Name != "b_id" || paths[1].LC.Name != "c_id" {
		t.Fatalf("expected a -> b -> c path, got %s -> %s", paths[0].LC.Name, paths[1].LC.Name)
	}
}

func TestReachabilityRejectsImpossiblePath(t *testing.T) {
	s := testSchema(t,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"),
		testPK("x"), testFK("x", "y_id", "y"),
		testPK("y"),
	)

	if s.canReach(tableNodeID(t, s, "a"), tableNodeID(t, s, "x")) {
		t.Fatal("expected a and x to be in disconnected components")
	}

	_, err := s.FindPath("a", "x", "")
	if !errors.Is(err, ErrPathNotFound) {
		t.Fatalf("expected ErrPathNotFound, got %v", err)
	}
}

func TestFKCyclesDetectSelfRecursiveFK(t *testing.T) {
	managerID := testFK("employees", "manager_id", "employees")
	managerID.FKRecursive = true

	s := testSchema(t,
		testPK("employees"),
		managerID,
	)

	selfID := tableNodeID(t, s, "employees")
	if !slices.Contains(s.fkCycles.Self, selfID) {
		t.Fatalf("expected self-recursive employees FK, got %+v", s.fkCycles.Self)
	}
	if len(s.fkCycles.Components) != 0 {
		t.Fatalf("self-recursive FK should not create a multi-table component: %+v", s.fkCycles.Components)
	}
}

func TestFKCyclesDetectMultiTableCycle(t *testing.T) {
	s := testSchema(t,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"), testFK("b", "c_id", "c"),
		testPK("c"), testFK("c", "a_id", "a"),
	)

	if len(s.fkCycles.Components) != 1 {
		t.Fatalf("expected one FK cycle component, got %+v", s.fkCycles.Components)
	}
	got := tableNames(t, s, s.fkCycles.Components[0])
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected cycle %v, got %v", want, got)
	}
}

func TestFKCyclesIgnoreSyntheticReverseEdges(t *testing.T) {
	s := testSchema(t,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"), testFK("b", "c_id", "c"),
		testPK("c"),
	)

	if len(s.fkCycles.Self) != 0 {
		t.Fatalf("unexpected self cycles: %+v", s.fkCycles.Self)
	}
	if len(s.fkCycles.Components) != 0 {
		t.Fatalf("synthetic reverse traversal edges inflated SCCs: %+v", s.fkCycles.Components)
	}
}

func TestFindPathThroughCycleDoesNotLoop(t *testing.T) {
	s := testSchema(t,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"), testFK("b", "c_id", "c"),
		testPK("c"), testFK("c", "a_id", "a"), testFK("c", "d_id", "d"),
		testPK("d"),
	)

	paths, err := s.FindPath("a", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 || len(paths) > 3 {
		t.Fatalf("expected bounded path through cycle, got %d hops: %+v", len(paths), paths)
	}
}

func TestFindPathPrefersDirectRelationshipOverCheaperIndirectPath(t *testing.T) {
	cols := []DBColumn{
		testPK("users"),
		{Schema: "public", Table: "users", Name: "category_counts", Type: "json"},
		testPK("products"),
		testFK("products", "owner_id", "users"),
		testFK("products", "category_ids", "categories"),
		testPK("categories"),
	}
	di := NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)

	categoryCounts := NewDBTable("public", "category_counts", "json", []DBColumn{
		testFK("category_counts", "category_id", "categories"),
		{Schema: "public", Table: "category_counts", Name: "count", Type: "int"},
	})
	categoryCounts.PrimaryCol = DBColumn{
		Schema:     "public",
		Table:      "users",
		Name:       "category_counts",
		Type:       "json",
		PrimaryKey: true,
	}
	categoryCounts.SecondaryCol = DBColumn{
		Schema:     "public",
		Table:      "users",
		Name:       "id",
		Type:       "bigint",
		PrimaryKey: true,
		UniqueKey:  true,
	}
	di.AddTable(categoryCounts)

	s, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}

	paths, err := s.FindPath("category_counts", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected direct embedded path, got %d hops: %+v", len(paths), paths)
	}
	if paths[0].Rel != RelEmbedded {
		t.Fatalf("expected RelEmbedded path, got %s: %+v", paths[0].Rel, paths[0])
	}
	if paths[0].LC.Table != "users" || paths[0].LC.Name != "category_counts" {
		t.Fatalf("expected users.category_counts as embedded source, got %s.%s",
			paths[0].LC.Table, paths[0].LC.Name)
	}
}

func densePathSchema(tb testing.TB, n, branch int) *DBSchema {
	tb.Helper()

	cols := make([]DBColumn, 0, n*(branch+1))
	for i := 0; i < n; i++ {
		table := fmt.Sprintf("t%d", i)
		cols = append(cols, testPK(table))
		for j := 1; j <= branch && i+j < n; j++ {
			cols = append(cols, testFK(table, fmt.Sprintf("t%d_id", i+j), fmt.Sprintf("t%d", i+j)))
		}
	}
	return testSchema(tb, cols...)
}

func twoComponentSchema(tb testing.TB, n int) *DBSchema {
	tb.Helper()

	cols := make([]DBColumn, 0, n*4)
	for prefix := 0; prefix < 2; prefix++ {
		for i := 0; i < n; i++ {
			table := fmt.Sprintf("c%d_t%d", prefix, i)
			cols = append(cols, testPK(table))
			if i+1 < n {
				cols = append(cols, testFK(table, fmt.Sprintf("c%d_t%d_id", prefix, i+1), fmt.Sprintf("c%d_t%d", prefix, i+1)))
			}
		}
	}
	return testSchema(tb, cols...)
}

func clearPathCacheForBenchmark(s *DBSchema) {
	s.pathCacheMu.Lock()
	s.pathCache = make(map[pathCacheKey][]int32)
	s.pathCacheMu.Unlock()
}

func BenchmarkFindPathDenseGraphCold(b *testing.B) {
	s := densePathSchema(b, 80, 8)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clearPathCacheForBenchmark(s)
		if _, err := s.FindPath("t0", "t79", ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindPathRepeatedLookup(b *testing.B) {
	s := densePathSchema(b, 80, 8)
	if _, err := s.FindPath("t0", "t79", ""); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.FindPath("t0", "t79", ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindPathUnreachable(b *testing.B) {
	s := twoComponentSchema(b, 40)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.FindPath("c0_t0", "c1_t0", ""); !errors.Is(err, ErrPathNotFound) {
			b.Fatalf("expected ErrPathNotFound, got %v", err)
		}
	}
}

func BenchmarkFindPathWeightedChoiceCold(b *testing.B) {
	s := testSchema(b,
		testPK("a"), testFK("a", "b_id", "b"),
		testPK("b"), testFK("b", "c_id", "c"),
		testPK("c"),
	)

	a, err := s.Find("public", "a")
	if err != nil {
		b.Fatal(err)
	}
	c, err := s.Find("public", "c")
	if err != nil {
		b.Fatal(err)
	}
	cID, ok := c.getColumn("id")
	if !ok {
		b.Fatal("missing c.id")
	}
	if _, err = s.addToGraph(a, DBColumn{
		Schema: "public",
		Table:  "a",
		Name:   "c_remote_id",
		Type:   "bigint",
	}, c, cID, RelRemote); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clearPathCacheForBenchmark(s)
		if _, err := s.FindPath("a", "c", ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompositeFKSchemaBuild(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		di := GetTestCompositeFKDBInfo()
		if _, err := NewDBSchema(di, nil); err != nil {
			b.Fatal(err)
		}
	}
}
