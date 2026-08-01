package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestAnalyticsRoleAllowlist(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		query   string
		blocked string
	}{
		{
			name:    "partition by",
			query:   `{ products { metric: name @rowNumber(by: "user_id", orderBy: { id: asc }) } }`,
			blocked: "user_id",
		},
		{
			name:    "orderBy",
			query:   `{ products { metric: name @rank(by: "id", orderBy: { price: desc }) } }`,
			blocked: "price",
		},
		{
			name:    "order shorthand",
			query:   `{ products { metric: price @rank(by: "id", order: desc) } }`,
			blocked: "price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qc.Compile([]byte(`query `+tt.query), nil, "user", "")
			if err == nil {
				t.Fatal("expected compile error for disallowed analytics column, got nil")
			}
			if !strings.Contains(err.Error(), tt.blocked) ||
				!strings.Contains(err.Error(), "db column blocked") {
				t.Fatalf("error should report blocked column %q, got: %v", tt.blocked, err)
			}
		})
	}

	if _, err := qc.Compile([]byte(`
		query { products { metric: name @rowNumber(by: "id", orderBy: { name: asc }) } }
	`), nil, "user", ""); err != nil {
		t.Fatalf("analytics over allowed columns should compile: %v", err)
	}
}

func TestAnalyticsBlockedColumn(t *testing.T) {
	di := sdata.GetTestDBInfo()
	for _, name := range []string{"user_id", "created_at"} {
		col, err := di.GetColumn("public", "products", name)
		if err != nil {
			t.Fatal(err)
		}
		col.Blocked = true
	}
	blockedSchema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(blockedSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		query   string
		blocked string
	}{
		{
			name:    "partition by",
			query:   `{ products { metric: name @rowNumber(by: "user_id", orderBy: { id: asc }) } }`,
			blocked: "user_id",
		},
		{
			name:    "orderBy",
			query:   `{ products { metric: name @rank(by: "id", orderBy: { created_at: desc }) } }`,
			blocked: "created_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qc.Compile([]byte(`query `+tt.query), nil, "user", "")
			if err == nil {
				t.Fatal("expected compile error for blocked analytics column, got nil")
			}
			if !strings.Contains(err.Error(), tt.blocked) ||
				!strings.Contains(err.Error(), "db column blocked") {
				t.Fatalf("error should report blocked column %q, got: %v", tt.blocked, err)
			}
		})
	}

	if _, err := qc.Compile([]byte(`
		query { products { metric: name @rowNumber(by: "id", orderBy: { name: asc }) } }
	`), nil, "user", ""); err != nil {
		t.Fatalf("analytics over unblocked columns should compile: %v", err)
	}
}
