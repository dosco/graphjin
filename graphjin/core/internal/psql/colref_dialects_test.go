package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func newDialectCompilers(t *testing.T, dbType string) (*qcode.Compiler, *psql.Compiler) {
	t.Helper()
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "user_id", "users"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "email", "products"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: dbType})
	return qc, pc
}

func compileWith(t *testing.T, qc *qcode.Compiler, pc *psql.Compiler, gql string) string {
	t.Helper()
	reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	_, sqlBytes, err := pc.CompileEx(reqQC)
	if err != nil {
		t.Fatal(err)
	}
	return string(sqlBytes)
}

func TestColRefOperand_MongoDB_SameCollection(t *testing.T) {
	qc, pc := newDialectCompilers(t, "mongodb")
	gql := `query {
		products(where: { price: { lt: { col: "id" } } }) {
			id
		}
	}`
	sql := compileWith(t, qc, pc, gql)
	want := `"$expr":{"$lt":["$price","$_id"]}`
	if !strings.Contains(sql, want) {
		t.Fatalf("[mongodb] expected %q in output; got:\n%s", want, sql)
	}
}

func TestColRefOperand_MongoDB_RejectsMultiHop(t *testing.T) {
	qc, pc := newDialectCompilers(t, "mongodb")
	gql := `query {
		products(where: { user_id: { eq: { col: "users.id" } } }) {
			id
		}
	}`
	reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pc.CompileEx(reqQC)
	if err == nil {
		t.Fatal("expected compile error for mongodb multi-hop col-ref, got nil")
	}
	if !strings.Contains(err.Error(), "mongodb") || !strings.Contains(err.Error(), "multi-hop") {
		t.Fatalf("expected mongodb-specific multi-hop rejection, got: %v", err)
	}
}

func TestColRefOperand_AllSQLDialects(t *testing.T) {
	sameTable := `query {
		products(where: { price: { lt: { col: "id" } } }) {
			id
		}
	}`
	belongsTo := `query {
		products(where: { user_id: { eq: { col: "users.id" } } }) {
			id
		}
	}`

	cases := []struct {
		dbType            string
		sameTableSubstr   string // exact substring expected in the WHERE
		belongsToSubstr   string // exact substring expected (correlated subquery)
	}{
		{
			dbType:          "postgres",
			sameTableSubstr: `("products"."price") < ("products"."id")`,
			belongsToSubstr: `(SELECT "users"."id" FROM "public"."users" WHERE "users"."id" = "products"."user_id")`,
		},
		{
			dbType:          "mysql",
			sameTableSubstr: "(`products`.`price`) < (`products`.`id`)",
			belongsToSubstr: "(SELECT `users`.`id` FROM `public`.`users` WHERE `users`.`id` = `products`.`user_id`)",
		},
		{
			dbType:          "mariadb",
			sameTableSubstr: "(`products_0`.`price`) < (`products_0`.`id`)",
			belongsToSubstr: "(SELECT `users`.`id` FROM `public`.`users` WHERE `users`.`id` = `products_0`.`user_id`)",
		},
		{
			dbType:          "sqlite",
			sameTableSubstr: `("products"."price") < ("products"."id")`,
			belongsToSubstr: `(SELECT "users"."id" FROM "public"."users" WHERE "users"."id" = "products"."user_id")`,
		},
		{
			dbType:          "mssql",
			sameTableSubstr: `([products_0].[price] < [products_0].[id])`,
			belongsToSubstr: `(SELECT [users].[id] FROM [public].[users] WHERE [users].[id] = [products_0].[user_id])`,
		},
		{
			dbType:          "oracle",
			sameTableSubstr: `("PRODUCTS"."PRICE") < ("PRODUCTS"."ID")`,
			belongsToSubstr: `(SELECT "USERS"."ID" FROM "PUBLIC"."USERS" WHERE "USERS"."ID" = "PRODUCTS"."USER_ID")`,
		},
		{
			dbType:          "snowflake",
			sameTableSubstr: `("products"."price") < ("products"."id")`,
			belongsToSubstr: `(SELECT "users"."id" FROM "public"."users" WHERE "users"."id" = "products"."user_id")`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.dbType+"_same_table", func(t *testing.T) {
			qc, pc := newDialectCompilers(t, tc.dbType)
			sql := compileWith(t, qc, pc, sameTable)
			if !strings.Contains(sql, tc.sameTableSubstr) {
				t.Fatalf("[%s] expected %q in SQL; got:\n%s", tc.dbType, tc.sameTableSubstr, sql)
			}
		})

		t.Run(tc.dbType+"_belongs_to", func(t *testing.T) {
			qc, pc := newDialectCompilers(t, tc.dbType)
			sql := compileWith(t, qc, pc, belongsTo)
			if !strings.Contains(sql, tc.belongsToSubstr) {
				t.Fatalf("[%s] expected %q in SQL; got:\n%s", tc.dbType, tc.belongsToSubstr, sql)
			}
		})
	}
}
