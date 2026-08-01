package psql_test

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

var (
	qcompile *qcode.Compiler
	pcompile *psql.Compiler
)

func TestMain(m *testing.M) {
	var err error

	schema, err := sdata.GetTestSchema()
	if err != nil {
		log.Fatal(err)
	}

	qcompile, err = qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "users", "customers"},
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
		Insert: qcode.InsertConfig{
			Presets: map[string]string{
				"price":      "$get_price",
				"user_id":    "$user_id",
				"created_at": "now",
				"updated_at": "now",
			},
		},
		Update: qcode.UpdateConfig{
			Filters: []string{"{ user_id: { eq: $user_id } }"},
			Presets: map[string]string{"updated_at": "now"},
		},
		Delete: qcode.DeleteConfig{
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("anon", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("anon1", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns:          []string{"id", "name", "price"},
			DisableFunctions: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar", "email", "products"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("bad_dude", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Filters:          []string{"false"},
			DisableFunctions: true,
		},
		Insert: qcode.InsertConfig{},
		Update: qcode.UpdateConfig{
			Filters: []string{"false"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// err = qcompile.AddRole("user", "", "mes", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "full_name", "avatar", "email"},
	// 		Filters: []string{
	// 			"{ id: { eq: $user_id } }",
	// 		},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	err = qcompile.AddRole("user", "public", "customers", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "vip"},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "public", "locations", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "geom", "boundary"},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	vars := map[string]string{
		"admin_account_id": "5",
		"get_price":        "sql:select price from prices where id = $product_id",
	}

	pcompile = psql.NewCompiler(psql.Config{
		Vars: vars,
	})

	os.Exit(m.Run())
}

func compileGQLToPSQL(t *testing.T, gql string,
	vars map[string]json.RawMessage,
	role string,
) {
	var v json.RawMessage
	var err error

	if v, err = json.Marshal(vars); err != nil {
		t.Error(err)
	}

	if err := _compileGQLToPSQL(t, gql, v, role); err != nil {
		t.Error(err)
	}
}

func compileGQLToPSQLExpectErr(t *testing.T, gql string,
	vars map[string]json.RawMessage,
	role string,
) {
	var v json.RawMessage
	var err error

	if v, err = json.Marshal(v); err != nil {
		t.Error(err)
	}

	if err := _compileGQLToPSQL(t, gql, v, role); err == nil {
		t.Error(errors.New("we were expecting an error"))
	}
}

func _compileGQLToPSQL(t *testing.T, gql string, vars json.RawMessage, role string) error {
	v := make(map[string]json.RawMessage)
	if err := json.Unmarshal(vars, &v); err != nil {
		t.Error(err)
	}

	for i := 0; i < 1000; i++ {
		qc, err := qcompile.Compile([]byte(gql), v, role, "")
		if err != nil {
			return err
		}

		_, _, err = pcompile.CompileEx(qc)
		if err != nil {
			return err
		}
	}

	return nil
}

// TestCompositeFK_ThroughColumn_EmitsFullJoinCondition is the end-to-end
// verification for P5: Stage 1b made @through(column:) match any column
// of a composite FK via ExtraPairs. This test confirms the SQL emitter
// then uses ALL composite columns in the join condition (not just the
// primary). If only the primary column appears in the emitted JOIN, the
// composite FK isn't actually being honored downstream — which would be
// a real bug to fix here.
//
// Schema (GetTestCompositeFKSchema):
//   enrollment.(term_id, course_id) ─ composite FK ─→ course_offering.(term_id, course_id)
//
// The query names course_id — the SECOND composite column, which lives
// in ExtraPairs not L/R. Pre-Stage-1b this returned "relationship not
// found".
func TestCompositeFK_ThroughColumn_EmitsFullJoinCondition(t *testing.T) {
	schema, err := sdata.GetTestCompositeFKSchema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatalf("qcode compiler: %v", err)
	}
	if err := qc.AddRole("user", "public", "enrollment", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "term_id", "course_id", "student_name"}},
	}); err != nil {
		t.Fatalf("add enrollment role: %v", err)
	}
	if err := qc.AddRole("user", "public", "course_offering", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"term_id", "course_id", "title"}},
	}); err != nil {
		t.Fatalf("add course_offering role: %v", err)
	}

	pc := psql.NewCompiler(psql.Config{})

	gql := `query {
		enrollment {
			id
			course_offering @through(column: "course_id") {
				term_id
				course_id
				title
			}
		}
	}`

	qcRes, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("qcode compile: %v", err)
	}

	_, sqlBytes, err := pc.CompileEx(qcRes)
	if err != nil {
		t.Fatalf("psql compile: %v", err)
	}
	sql := string(sqlBytes)

	// Both composite columns must appear in the emitted join condition,
	// not just somewhere in the SELECT. Look for term_id AND course_id
	// after the WHERE keyword that follows the LATERAL course_offering
	// subquery — that's where the join predicate lives.
	loIdx := strings.Index(strings.ToLower(sql), "course_offering")
	if loIdx < 0 {
		t.Fatalf("emitted SQL missing course_offering join:\n%s", sql)
	}
	whereIdx := strings.Index(strings.ToLower(sql[loIdx:]), "where")
	if whereIdx < 0 {
		t.Fatalf("emitted SQL has course_offering but no WHERE clause for the join:\n%s", sql)
	}
	joinClause := sql[loIdx+whereIdx:]
	if !strings.Contains(joinClause, "term_id") {
		t.Errorf("join clause missing term_id (primary composite column).\nfull SQL:\n%s\njoin clause:\n%s", sql, joinClause)
	}
	if !strings.Contains(joinClause, "course_id") {
		t.Errorf("join clause missing course_id (ExtraPairs composite column).\nfull SQL:\n%s\njoin clause:\n%s", sql, joinClause)
	}
	t.Logf("emitted SQL (composite-FK join verified):\n%s", sql)
}
