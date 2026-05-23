package psql_test

import (
	"strings"
	"testing"
)

func TestColRefOperand_SameTable(t *testing.T) {
	gql := `query {
		products(where: { price: { lt: { col: "id" } } }) {
			id
			name
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")

	if !strings.Contains(sql, `"price"`) || !strings.Contains(sql, `"id"`) {
		t.Fatalf("expected both column refs in SQL: %s", sql)
	}
	if !strings.Contains(sql, `<`) {
		t.Fatalf("expected `<` comparison in SQL: %s", sql)
	}
	if strings.Contains(sql, `'`) && strings.Contains(sql, `'id'`) {
		t.Fatalf("`id` should be a column reference, not a literal: %s", sql)
	}
}

func TestColRefOperand_BelongsToHop(t *testing.T) {
	gql := `query {
		products(where: { user_id: { eq: { col: "users.id" } } }) {
			id
			name
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")

	if !strings.Contains(sql, `SELECT "users"."id" FROM "public"."users"`) &&
		!strings.Contains(sql, `SELECT "users"."id" FROM "users"`) {
		t.Fatalf("expected correlated subquery for belongs-to col ref: %s", sql)
	}
}

func TestColRefOperand_RejectedOnListOp(t *testing.T) {
	gql := `query {
		products(where: { id: { in: { col: "user_id" } } }) {
			id
		}
	}`
	compileGQLToPSQLExpectErr(t, gql, nil, "user")
}

func TestColRefOperand_RejectedOnUnknownColumn(t *testing.T) {
	gql := `query {
		products(where: { price: { lt: { col: "nonexistent" } } }) {
			id
		}
	}`
	compileGQLToPSQLExpectErr(t, gql, nil, "user")
}

func TestColRefOperand_RejectedOnOneToMany(t *testing.T) {
	gql := `query {
		users(where: { id: { eq: { col: "products.id" } } }) {
			id
		}
	}`
	compileGQLToPSQLExpectErr(t, gql, nil, "user")
}

func TestColRefOperand_SameTableSQL(t *testing.T) {
	gql := `query {
		products(where: { price: { lt: { col: "id" } } }) {
			id
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")
	if !strings.Contains(sql, `("products"."price") < ("products"."id")`) {
		t.Fatalf("expected column-vs-column comparison, got: %s", sql)
	}
}

func TestColRefOperand_BelongsToSQL(t *testing.T) {
	gql := `query {
		products(where: { user_id: { eq: { col: "users.id" } } }) {
			id
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")
	want := `(SELECT "users"."id" FROM "public"."users" WHERE "users"."id" = "products"."user_id")`
	if !strings.Contains(sql, want) {
		t.Fatalf("expected correlated subquery %q in SQL: %s", want, sql)
	}
}
