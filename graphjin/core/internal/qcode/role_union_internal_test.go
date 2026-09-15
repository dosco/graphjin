package qcode

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const (
	filPrice = `{ price: { gt: 10 } }`
	filName  = `{ name: { eq: "x" } }`
	filOwner = `{ user_id: { eq: $user_id } }`
)

func newUnionCompiler(t *testing.T, union bool, roles map[string]TRConfig) *Compiler {
	t.Helper()
	s, err := sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}
	co, err := NewCompiler(s, Config{UnionRoles: union})
	if err != nil {
		t.Fatal(err)
	}
	for role, trc := range roles {
		if err := co.AddRole(role, "public", "products", trc); err != nil {
			t.Fatalf("AddRole %s: %v", role, err)
		}
	}
	return co
}

func productsRole(co *Compiler, role string) trval {
	return co.getRole(role, "public", "products", "products")
}

func colNames(cols map[string]struct{}) string {
	if len(cols) == 0 {
		return "*"
	}
	out := make([]string, 0, len(cols))
	for c := range cols {
		out = append(out, c)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func query(filters []string, cols ...string) TRConfig {
	return TRConfig{Query: QueryConfig{Filters: filters, Columns: cols}}
}

func TestUnionReadRules(t *testing.T) {
	cases := []struct {
		name      string
		a, b      TRConfig
		wantBlock bool
		wantCols  string
		wantOp    ExpOp // OpNop means no row filter
		wantKids  int   // children when wantOp is OpOr
	}{
		{
			name:     "one component blocks",
			a:        query([]string{filPrice}, "id", "price"),
			b:        TRConfig{Query: QueryConfig{Block: true}},
			wantCols: "id,price", wantOp: OpGreaterThan,
		},
		{
			name:     "dominant component without filter",
			a:        query(nil, "id", "name", "price"),
			b:        query([]string{filPrice}, "id", "name"),
			wantCols: "id,name,price", wantOp: OpNop,
		},
		{
			name:     "same columns join filters with or",
			a:        query([]string{filPrice}, "id", "name"),
			b:        query([]string{filName}, "id", "name"),
			wantCols: "id,name", wantOp: OpOr, wantKids: 2,
		},
		{
			name:     "same filter joins columns",
			a:        query([]string{filPrice}, "id"),
			b:        query([]string{filPrice}, "name"),
			wantCols: "id,name", wantOp: OpGreaterThan,
		},
		{
			name:     "different filters and columns intersect columns",
			a:        query([]string{filPrice}, "id", "name", "price"),
			b:        query([]string{filName}, "id", "name"),
			wantCols: "id,name", wantOp: OpOr, wantKids: 2,
		},
		{
			name:      "empty intersection blocks",
			a:         query([]string{filPrice}, "price"),
			b:         query([]string{filName}, "name"),
			wantBlock: true,
		},
		{
			name:      "both block",
			a:         TRConfig{Query: QueryConfig{Block: true}},
			b:         TRConfig{Query: QueryConfig{Block: true}},
			wantBlock: true,
		},
		{
			name:     "false filter component is ignored",
			a:        query([]string{"false"}, "id", "name", "price"),
			b:        query([]string{filName}, "id"),
			wantCols: "id", wantOp: OpEquals,
		},
		{
			name:     "all false filters keep an empty result",
			a:        query([]string{"false"}, "id"),
			b:        query([]string{"false"}, "name"),
			wantCols: "id", wantOp: OpFalse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			co := newUnionCompiler(t, true, map[string]TRConfig{"a": tc.a, "b": tc.b})
			got := productsRole(co, "a+b")
			if got.role != "a+b" || !got.set {
				t.Fatalf("merged rule identity = %q set=%v", got.role, got.set)
			}
			if got.query.block != tc.wantBlock {
				t.Fatalf("block = %v, want %v", got.query.block, tc.wantBlock)
			}
			if tc.wantBlock {
				return
			}
			if cols := colNames(got.query.cols); cols != tc.wantCols {
				t.Fatalf("columns = %s, want %s", cols, tc.wantCols)
			}
			op := OpNop
			if got.query.fil != nil {
				op = got.query.fil.Op
			}
			if op != tc.wantOp {
				t.Fatalf("filter op = %v, want %v", op, tc.wantOp)
			}
			if tc.wantOp == OpOr && len(got.query.fil.Children) != tc.wantKids {
				t.Fatalf("or children = %d, want %d", len(got.query.fil.Children), tc.wantKids)
			}
		})
	}
}

func TestUnionComponentWithoutTableRuleIsUnrestricted(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"a": query([]string{filPrice}, "id"),
	})
	got := productsRole(co, "a+b")
	if got.query.block || len(got.query.cols) != 0 || !unfiltered(unionOp{fil: got.query.fil}) {
		t.Fatalf("a role without a products rule must stay unrestricted, got cols=%s fil=%v block=%v",
			colNames(got.query.cols), got.query.fil, got.query.block)
	}
}

func TestUnionLimitFunctionsAndUserFlag(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"a": {Query: QueryConfig{Filters: []string{filOwner}, Columns: []string{"id", "name"}, Limit: 10, DisableFunctions: true}},
		"b": {Query: QueryConfig{Filters: []string{filName}, Columns: []string{"id", "name"}, Limit: 20}},
		"c": {Query: QueryConfig{Filters: []string{filPrice}, Columns: []string{"id", "name"}}},
	})

	ab := productsRole(co, "a+b")
	if ab.query.limit != 20 {
		t.Fatalf("limit = %d, want the larger limit 20", ab.query.limit)
	}
	if !ab.query.disable.funcs {
		t.Fatal("functions must stay disabled when a component disables them")
	}
	if !ab.query.filNU {
		t.Fatal("merged filter must still require the user when a component filter uses $user_id")
	}

	if bc := productsRole(co, "b+c"); bc.query.limit != 0 {
		t.Fatalf("limit = %d, want 0 when a component has no limit", bc.query.limit)
	}
}

func TestUnionWriteRules(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"a": {
			Insert: InsertConfig{Columns: []string{"id", "name", "price"}, Presets: map[string]string{"user_id": "$user_id"}},
			Update: UpdateConfig{Filters: []string{filPrice}, Columns: []string{"name", "price"}},
			Delete: DeleteConfig{Filters: []string{filPrice}},
		},
		"b": {
			Insert: InsertConfig{Columns: []string{"id", "name"}, Presets: map[string]string{"user_id": "$user_id"}},
			Update: UpdateConfig{Filters: []string{filName}, Columns: []string{"name"}},
			Delete: DeleteConfig{Filters: []string{filName}},
		},
		"c": {
			Insert: InsertConfig{Columns: []string{"id"}, Presets: map[string]string{"user_id": "1"}},
			Update: UpdateConfig{Filters: []string{filName}, Columns: []string{"price"}},
			Delete: DeleteConfig{Block: true},
		},
	})

	ab := productsRole(co, "a+b")
	if ab.insert.block || colNames(ab.insert.cols) != "id,name,price" || ab.insert.presets["user_id"] != "$user_id" {
		t.Fatalf("insert = block %v cols %s presets %v; want the covering component id,name,price with the shared preset",
			ab.insert.block, colNames(ab.insert.cols), ab.insert.presets)
	}
	if ab.update.block || colNames(ab.update.cols) != "name" || ab.update.fil.Op != OpOr {
		t.Fatalf("update = block %v cols %s; want intersected columns and an or filter",
			ab.update.block, colNames(ab.update.cols))
	}
	if ab.delete.block || ab.delete.fil.Op != OpOr {
		t.Fatalf("delete must allow rows either component may delete")
	}

	ac := productsRole(co, "a+c")
	if !ac.insert.block {
		t.Fatal("insert must block when component presets differ")
	}
	if !productsRole(co, "b+c").update.block {
		t.Fatal("update must block when the column intersection is empty")
	}
	if ac.delete.block || ac.delete.fil.Op != OpGreaterThan {
		t.Fatal("delete must keep the only component that allows it")
	}
}

func TestUnionInsertNeverWidensColumns(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"a": {Insert: InsertConfig{Columns: []string{"id", "price"}}},
		"b": {Insert: InsertConfig{Columns: []string{"id", "name"}}},
		"c": {Insert: InsertConfig{Columns: []string{"price"}}},
		"d": {Insert: InsertConfig{Columns: []string{"name"}}},
	})
	if ab := productsRole(co, "a+b"); ab.insert.block || colNames(ab.insert.cols) != "id" {
		t.Fatalf("insert = block %v cols %s; want only the shared column id", ab.insert.block, colNames(ab.insert.cols))
	}
	if cd := productsRole(co, "c+d"); !cd.insert.block {
		t.Fatal("insert must block when no column is shared")
	}
}

func TestUnionRolesDisabledKeepsPlainLookup(t *testing.T) {
	co := newUnionCompiler(t, false, map[string]TRConfig{
		"a": query([]string{filPrice}, "id"),
		"b": query([]string{filName}, "name"),
	})
	if got := productsRole(co, "a+b"); got.set {
		t.Fatal("without union roles a name with + must not merge rules")
	}
}

func TestUnionRuleIsCached(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"a": query([]string{filPrice}, "id", "name"),
		"b": query([]string{filName}, "id", "name"),
	})
	first := productsRole(co, "a+b")
	second := productsRole(co, "a+b")
	if first.query.fil != second.query.fil {
		t.Fatal("merged rule should be built once and reused")
	}
}

// TestUnionReadNeverOverGrants checks the safety invariant on random rules:
// every column the merged rule allows, under every row filter it applies,
// must be allowed together by one single component.
func TestUnionReadNeverOverGrants(t *testing.T) {
	allCols := []string{"id", "name", "price", "description"}
	filters := [][]string{nil, {filPrice}, {filName}, {filOwner}, {"false"}}
	rng := rand.New(rand.NewSource(639))

	for iter := 0; iter < 2000; iter++ {
		n := 2 + rng.Intn(3)
		roles := make(map[string]TRConfig, n)
		names := make([]string, 0, n)
		for i := 0; i < n; i++ {
			var cols []string
			if rng.Intn(4) != 0 {
				for _, c := range allCols {
					if rng.Intn(2) == 0 {
						cols = append(cols, c)
					}
				}
			}
			name := fmt.Sprintf("r%d", i)
			names = append(names, name)
			roles[name] = TRConfig{Query: QueryConfig{
				Filters: filters[rng.Intn(len(filters))],
				Columns: cols,
				Block:   rng.Intn(6) == 0,
			}}
		}

		co := newUnionCompiler(t, true, roles)
		comps := make([]trval, 0, n)
		for _, name := range names {
			comps = append(comps, productsRole(co, name))
		}
		merged := productsRole(co, strings.Join(names, unionRoleSeparator))
		if merged.query.block {
			continue
		}

		disjuncts := []*Exp{merged.query.fil}
		if merged.query.fil != nil && merged.query.fil.Op == OpOr {
			disjuncts = merged.query.fil.Children
		}
		allowed := allCols
		if len(merged.query.cols) != 0 {
			allowed = nil
			for c := range merged.query.cols {
				allowed = append(allowed, c)
			}
		}

		keyOf := make(map[*Exp]string, len(comps))
		for _, c := range comps {
			keyOf[c.query.fil] = c.query.filKey
		}
		for _, d := range disjuncts {
			for _, col := range allowed {
				if !someComponentAllows(comps, keyOf, d, col) {
					t.Fatalf("iteration %d: merged rule allows column %q under filter %v, but no single component allows both; roles=%+v",
						iter, col, d, roles)
				}
			}
		}
	}
}

func someComponentAllows(comps []trval, keyOf map[*Exp]string, disjunct *Exp, col string) bool {
	disjunctUnfiltered := unfiltered(unionOp{fil: disjunct})
	disjunctKey, known := keyOf[disjunct]
	for _, c := range comps {
		if c.query.block {
			continue
		}
		if len(c.query.cols) != 0 {
			if _, ok := c.query.cols[col]; !ok {
				continue
			}
		}
		switch {
		case unfiltered(unionOp{fil: c.query.fil}):
			// The component allows every row, so any row filter is narrower.
			return true
		case disjunctUnfiltered:
			// An unfiltered merge needs an unfiltered component.
		case disjunct.Op == OpFalse:
			return true
		case known && c.query.filKey == disjunctKey:
			return true
		}
	}
	return false
}

// TestUnionSafetyCheckDetectsNaiveMerge proves the invariant check above can
// fail: a plain union of columns with an OR of filters over-grants.
func TestUnionSafetyCheckDetectsNaiveMerge(t *testing.T) {
	co := newUnionCompiler(t, true, map[string]TRConfig{
		"finance": query([]string{filPrice}, "id", "name", "price"),
		"sales":   query([]string{filName}, "id", "name"),
	})
	finance, sales := productsRole(co, "finance"), productsRole(co, "sales")
	comps := []trval{finance, sales}
	keyOf := map[*Exp]string{finance.query.fil: finance.query.filKey, sales.query.fil: sales.query.filKey}

	// A naive merge would expose price under the sales filter.
	if someComponentAllows(comps, keyOf, sales.query.fil, "price") {
		t.Fatal("safety check accepted price under the sales filter")
	}

	merged := productsRole(co, "finance+sales")
	if _, ok := merged.query.cols["price"]; ok {
		t.Fatal("merged rule must not expose price")
	}
}
