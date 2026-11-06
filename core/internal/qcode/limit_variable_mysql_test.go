package qcode

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func newSingleTableCompiler(t *testing.T, dbType string) *Compiler {
	t.Helper()
	cols := []sdata.DBColumn{{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true}}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo(dbType, 80000, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	co, err := NewCompiler(schema, Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	return co
}

func TestMySQLFamilyAcceptsLimitVariablesOutsideSubscriptions(t *testing.T) {
	for _, db := range []string{"mysql", "mariadb"} {
		co := newSingleTableCompiler(t, db)
		qc, err := co.Compile([]byte(`query { users(limit: $limit, offset: $off) { id } }`), nil, "user", "")
		if err != nil {
			t.Fatalf("%s query: %v", db, err)
		}
		if p := qc.Selects[0].Paging; p.LimitVar != "limit" || p.OffsetVar != "off" {
			t.Fatalf("%s query: paging vars = %q/%q", db, p.LimitVar, p.OffsetVar)
		}

		for _, gql := range []string{
			`subscription { users(limit: $limit) { id } }`,
			`subscription { users(limit: 5, offset: $off) { id } }`,
		} {
			_, err := co.Compile([]byte(gql), nil, "user", "")
			if err == nil || !strings.Contains(err.Error(), "not supported in subscriptions") {
				t.Fatalf("%s %s: want subscription refusal, got %v", db, gql, err)
			}
		}
	}

	co := newSingleTableCompiler(t, "postgres")
	if _, err := co.Compile([]byte(`subscription { users(limit: $limit) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("postgres subscription with limit variable: %v", err)
	}
}
