package sdata_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/assert"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestDWG(t *testing.T) {
	for i := 0; i < 10000; i++ {
		s, err := sdata.GetTestSchema()
		assert.NoErrorFatal(t, err)

		paths, err := s.FindPath("customers", "users", "")
		assert.NoErrorFatal(t, err)

		exp := `(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got := paths[0].String()
		assert.Equals(t, exp, got)

		paths, err = s.FindPath("purchases", "users", "")
		assert.NoErrorFatal(t, err)

		exp = `(public.purchases) public.purchases.customer_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.customers.id ==> RelOneToOne ==> (public.customers) public.customers.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got = paths[0].String()
		assert.Equals(t, exp, got)

		exp = `(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got = paths[1].String()
		assert.Equals(t, exp, got)

	}
}

func TestDWG_AmbiguousPath(t *testing.T) {
	s, err := sdata.GetTestMultiFKSchema()
	assert.NoErrorFatal(t, err)

	_, err = s.FindPath("orders", "users", "")
	if err == nil {
		t.Fatal("expected AmbiguousPathError when orders has two FKs to users")
	}

	var ambig *sdata.AmbiguousPathError
	if !errors.As(err, &ambig) {
		t.Fatalf("expected *AmbiguousPathError, got %T: %v", err, err)
	}

	if len(ambig.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(ambig.Candidates), ambig.Candidates)
	}

	cols := map[string]bool{}
	for _, c := range ambig.Candidates {
		cols[c.Column] = true
	}
	if !cols["customer_id"] || !cols["salesperson_id"] {
		t.Fatalf("expected candidates customer_id + salesperson_id, got %+v", ambig.Candidates)
	}

	if !strings.Contains(ambig.Error(), "ambiguous relationship") {
		t.Fatalf("error message missing 'ambiguous relationship' marker: %s", ambig.Error())
	}
}

func TestDWG_CompositeFK_ColumnHintMatchesExtraPair(t *testing.T) {
	s, err := sdata.GetTestCompositeFKSchema()
	assert.NoErrorFatal(t, err)

	// First composite column: lives in L/R on the edge.
	paths, err := s.FindPathByColumn("enrollment", "course_offering", "term_id")
	assert.NoErrorFatal(t, err)
	if len(paths) == 0 {
		t.Fatal("expected non-empty path for term_id (primary composite column)")
	}
	if len(paths[0].ExtraPairs) == 0 {
		t.Fatalf("expected composite ExtraPairs on the resolved edge, got %+v", paths[0])
	}

	// Second composite column: lives only in ExtraPairs. Pre-fix this returned
	// ErrPathNotFound because filterLinesByColumn ignored ExtraPairs.
	paths, err = s.FindPathByColumn("enrollment", "course_offering", "course_id")
	assert.NoErrorFatal(t, err)
	if len(paths) == 0 {
		t.Fatal("expected non-empty path for course_id (non-primary composite column)")
	}
	if len(paths[0].ExtraPairs) == 0 {
		t.Fatalf("composite ExtraPairs missing on edge resolved via course_id: %+v", paths[0])
	}
}

func TestDWG_AmbiguousPath_DisambiguatedByColumn(t *testing.T) {
	s, err := sdata.GetTestMultiFKSchema()
	assert.NoErrorFatal(t, err)

	paths, err := s.FindPathByColumn("orders", "users", "customer_id")
	assert.NoErrorFatal(t, err)
	if len(paths) == 0 {
		t.Fatal("expected non-empty path with column hint")
	}
	if !strings.EqualFold(paths[0].LC.Name, "customer_id") {
		t.Fatalf("expected path to use customer_id, got %s", paths[0].LC.Name)
	}

	paths, err = s.FindPathByColumn("orders", "users", "salesperson_id")
	assert.NoErrorFatal(t, err)
	if !strings.EqualFold(paths[0].LC.Name, "salesperson_id") {
		t.Fatalf("expected path to use salesperson_id, got %s", paths[0].LC.Name)
	}
}
