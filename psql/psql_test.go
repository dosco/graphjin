package psql

import (
	"log"
	"os"
	"testing"

	"github.com/dosco/super-graph/qcode"
)

const (
	errNotExpected = "Generated SQL did not match what was expected"
)

var (
	qcompile *qcode.Compiler
	pcompile *Compiler
)

func TestMain(m *testing.M) {
	var err error

	qcompile, err = qcode.NewCompiler(qcode.Config{
		DefaultFilter: []string{
			`{ user_id: { _eq: $user_id } }`,
		},
		FilterMap: map[string][]string{
			"users": []string{
				"{ id: { eq: $user_id } }",
			},
			"products": []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
			"customers": []string{},
			"mes": []string{
				"{ id: { eq: $user_id } }",
			},
		},
		Blacklist: []string{
			"secret",
			"password",
			"token",
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	tables := []*DBTable{
		&DBTable{Name: "customers", Type: "table"},
		&DBTable{Name: "users", Type: "table"},
		&DBTable{Name: "products", Type: "table"},
		&DBTable{Name: "purchases", Type: "table"},
	}

	columns := [][]*DBColumn{
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 4, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 5, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 6, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 7, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 8, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 9, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 10, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 4, Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 5, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 6, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 7, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 8, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 9, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 10, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 11, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 2, Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 3, Name: "description", Type: "text", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 4, Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 5, Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "users", FKeyColID: []int{1}},
			&DBColumn{ID: 6, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 7, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 8, Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 2, Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "customers", FKeyColID: []int{1}},
			&DBColumn{ID: 3, Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "products", FKeyColID: []int{1}},
			&DBColumn{ID: 4, Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 5, Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 6, Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)},
			&DBColumn{ID: 7, Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, Uniquekey: false, FKeyTable: "", FKeyColID: []int(nil)}},
	}

	schema := &DBSchema{
		Tables: make(map[string]*DBTableInfo),
		RelMap: make(map[uint64]*DBRel),
	}

	for i, t := range tables {
		schema.updateSchema(t, columns[i])
	}

	vars := NewVariables(map[string]string{
		"account_id": "select account_id from users where id = $user_id",
	})

	pcompile = NewCompiler(Config{
		Schema: schema,
		Vars:   vars,
		TableMap: map[string]string{
			"mes": "users",
		},
	})

	os.Exit(m.Run())
}

func compileGQLToPSQL(gql string) (string, error) {
	qc, err := qcompile.CompileQuery(gql)
	if err != nil {
		return "", err
	}

	_, sqlStmts, err := pcompile.Compile(qc)
	if err != nil {
		return "", err
	}

	return sqlStmts[0], nil
}

func withComplexArgs(t *testing.T) {
	gql := `query {
		products(
			# returns only 30 items
			limit: 30,
	
			# starts from item 10, commented out for now
			# offset: 10,
	
			# orders the response items by highest price
			order_by: { price: desc },
	
			# no duplicate prices returned
			distinct: [ price ]
			
			# only items with an id >= 30 and < 30 are returned
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products" ORDER BY "products_0.ob.price" DESC), '[]') AS "products" FROM (SELECT  DISTINCT ON ("products_0.ob.price") row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "products", "products_0"."price" AS "products_0.ob.price" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."id") < (28)) AND (("products"."id") >= (20))) LIMIT ('30') :: integer) AS "products_0" ORDER BY "products_0.ob.price" DESC LIMIT ('30') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereMultiOr(t *testing.T) {
	gql := `query {
		products(
			where: { 
				or: { 
					not: { id: { is_null: true } }, 
					price: { gt: 10 },
					price: { lt: 20 } 
				} }
			) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."price") < (20)) OR (("products"."price") > (10)) OR NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereIsNull(t *testing.T) {
	gql := `query {
		products(
			where: { 
				and: { 
					not: { id: { is_null: true } }, 
					price: { gt: 10 } 
				}}) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."price") > (10)) AND NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereAndList(t *testing.T) {
	gql := `query {
		products(
			where: { 
				and: [ 
					{ not: { id: { is_null: true } } }, 
					{ price: { gt: 10 } }, 
				] } ) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."price") > (10)) AND NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func oneToMany(t *testing.T) {
	gql := `query {
		users {
			email
			products {
				name
				price
			}
		}
	}`

	sql := `SELECT json_object_agg('users', users) FROM (SELECT coalesce(json_agg("users"), '[]') AS "users" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "users_0"."email" AS "email", "products_1_join"."products" AS "products") AS "sel_0")) AS "users" FROM (SELECT "users"."email", "users"."id" FROM "users" WHERE ((("users"."id") = ('{{user_id}}'))) LIMIT ('20') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "products_1"."name" AS "name", "products_1"."price" AS "price") AS "sel_1")) AS "products" FROM (SELECT "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('20') :: integer) AS "products_1" LIMIT ('20') :: integer) AS "products_1") AS "products_1_join" ON ('true') LIMIT ('20') :: integer) AS "users_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func belongsTo(t *testing.T) {
	gql := `query {
		products {
			name
			price
			users {
				email
			}
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "products_0"."price" AS "price", "users_1_join"."users" AS "users") AS "sel_0")) AS "products" FROM (SELECT "products"."name", "products"."price", "products"."user_id" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8))) LIMIT ('20') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("users"), '[]') AS "users" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "users_1"."email" AS "email") AS "sel_1")) AS "users" FROM (SELECT "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('20') :: integer) AS "users_1" LIMIT ('20') :: integer) AS "users_1") AS "users_1_join" ON ('true') LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func manyToMany(t *testing.T) {
	gql := `query { 
		products {
			name
			customers {
				email
				full_name
			}
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "customers_1_join"."customers" AS "customers") AS "sel_0")) AS "products" FROM (SELECT "products"."name", "products"."id" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8))) LIMIT ('20') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("customers"), '[]') AS "customers" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "customers_1"."email" AS "email", "customers_1"."full_name" AS "full_name") AS "sel_1")) AS "customers" FROM (SELECT "customers"."email", "customers"."full_name" FROM "customers" LEFT OUTER JOIN "purchases" ON (("purchases"."product_id") = ("products_0"."id")) WHERE ((("customers"."id") = ("purchases"."customer_id"))) LIMIT ('20') :: integer) AS "customers_1" LIMIT ('20') :: integer) AS "customers_1") AS "customers_1_join" ON ('true') LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func manyToManyReverse(t *testing.T) {
	gql := `query {
		customers {
			email
			full_name
			products {
				name
			}
		}
	}`

	sql := `SELECT json_object_agg('customers', customers) FROM (SELECT coalesce(json_agg("customers"), '[]') AS "customers" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "customers_0"."email" AS "email", "customers_0"."full_name" AS "full_name", "products_1_join"."products" AS "products") AS "sel_0")) AS "customers" FROM (SELECT "customers"."email", "customers"."full_name", "customers"."id" FROM "customers" LIMIT ('20') :: integer) AS "customers_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "products_1"."name" AS "name") AS "sel_1")) AS "products" FROM (SELECT "products"."name" FROM "products" LEFT OUTER JOIN "purchases" ON (("purchases"."customer_id") = ("customers_0"."id")) WHERE ((("products"."id") = ("purchases"."product_id"))) LIMIT ('20') :: integer) AS "products_1" LIMIT ('20') :: integer) AS "products_1") AS "products_1_join" ON ('true') LIMIT ('20') :: integer) AS "customers_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func fetchByID(t *testing.T) {
	gql := `query {
		product(id: 15) {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('product', products) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("id") = (15))) LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func searchQuery(t *testing.T) {
	gql := `query {
		products(search: "Imperial") {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("tsv") @@ to_tsquery('Imperial'))) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunction(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "products_0"."count_price" AS "count_price") AS "sel_0")) AS "products" FROM (SELECT "products"."name", count("products"."price") AS count_price FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8))) GROUP BY "products"."name" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunctionWithFilter(t *testing.T) {
	gql := `query {
		products(where: { id: { gt: 10 } }) {
			id
			max_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("products"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."max_price" AS "max_price") AS "sel_0")) AS "products" FROM (SELECT "products"."id", max("products"."price") AS max_price FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."id") > (10))) GROUP BY "products"."id" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "products_0") AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func queryWithVariables(t *testing.T) {
	gql := `query {
		product(id: $PRODUCT_ID, where: { price: { eq: $PRODUCT_PRICE } }) {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('product', products) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "products" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > (0)) AND (("products"."price") < (8)) AND (("products"."price") = ('{{PRODUCT_PRICE}}')) AND (("id") = ('{{PRODUCT_ID}}'))) LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func syntheticTables(t *testing.T) {
	gql := `query {
		me {
			email
		}
	}`

	sql := `SELECT json_object_agg('me', mes) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "mes_0"."email" AS "email") AS "sel_0")) AS "mes" FROM (SELECT "mes"."email" FROM "users" AS "mes" WHERE ((("mes"."id") = ('{{user_id}}'))) LIMIT ('1') :: integer) AS "mes_0" LIMIT ('1') :: integer) AS "done_1337";`

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	if resSQL != sql {
		t.Fatal(errNotExpected)
	}
}

func TestCompileGQL(t *testing.T) {
	t.Run("withComplexArgs", withComplexArgs)
	t.Run("withWhereAndList", withWhereAndList)
	t.Run("withWhereIsNull", withWhereIsNull)
	t.Run("withWhereMultiOr", withWhereMultiOr)
	t.Run("fetchByID", fetchByID)
	t.Run("searchQuery", searchQuery)
	t.Run("belongsTo", belongsTo)
	t.Run("oneToMany", oneToMany)
	t.Run("manyToMany", manyToMany)
	t.Run("manyToManyReverse", manyToManyReverse)
	t.Run("aggFunction", aggFunction)
	t.Run("aggFunctionWithFilter", aggFunctionWithFilter)
	t.Run("queryWithVariables", queryWithVariables)
	t.Run("syntheticTables", syntheticTables)
}

func BenchmarkCompileGQLToSQL(b *testing.B) {
	gql := `query {
		products(
			# returns only 30 items
			limit: 30,
	
			# starts from item 10, commented out for now
			# offset: 10,
	
			# orders the response items by highest price
			order_by: { price: desc },
	
			# only items with an id >= 30 and < 30 are returned
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
			id
			name
			price
			user {
				full_name
				picture : avatar
			}
		}
	}`

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, err := compileGQLToPSQL(gql)
		if err != nil {
			b.Fatal(err)
		}
	}
}
