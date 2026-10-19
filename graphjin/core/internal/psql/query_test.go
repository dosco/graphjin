package psql_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func simpleQuery(t *testing.T) {
	gql := `query {
		products {
			id
			user {
				id
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withNestedOrderBy(t *testing.T) {
	gql := `query {
	               products(
	                       where: { and: {customer: { user: { email: { eq: "http" } } }, 
						   		not: { customer: { user: { email: { eq: ".com"}  }}}}}
	                       order_by: { customer: { vip: desc }}
	               ) {
	                       id
	                       user {
	                               id
	                       }
	               }
	       }`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withVariableLimit(t *testing.T) {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"limit": json.RawMessage(`100`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
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

			# only items with an id >= 20 and < 28 are returned
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
			id
			name
			price
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withWhereIn(t *testing.T) {
	gql := `query {
		products(where: { id: { in: $list } }) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"list": json.RawMessage(`[1,2,3]`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
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

	compileGQLToPSQL(t, gql, nil, "user")
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

	compileGQLToPSQL(t, gql, nil, "user")
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

	compileGQLToPSQL(t, gql, nil, "user")
}

func withNestedWhere(t *testing.T) {
	gql := `query {
			products(where: { comments: { users: { email: { eq: $email } } } }) {
			 id
		 }
	}`

	vars := map[string]json.RawMessage{
		"email": json.RawMessage(`"test@test.com"`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func withAlternateName(t *testing.T) {
	gql := `query {
			comments {
			 id
			 commenter {
				 email
			 }
		 }
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func fetchByID(t *testing.T) {
	gql := `query {
		products(id: $id) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func searchQuery(t *testing.T) {
	gql := `query {
		products(search: $query) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "admin")
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

	compileGQLToPSQL(t, gql, nil, "user")
}

func oneToManyReverse(t *testing.T) {
	gql := `query {
		products {
			name
			price
			users {
				email
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func oneToManyArray(t *testing.T) {
	gql := `
	query {
		products {
			name
			price
			tags {
				id
				name
			}
		}
		tags {
			name
			products {
				name
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "admin")
}

func manyToMany(t *testing.T) {
	gql := `query {
		products {
			name
			customers {
				user {
					email
					full_name
				}
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func manyToManyReverse(t *testing.T) {
	gql := `query {
		customers {
			user {
				email
				full_name
			}
			products {
				name
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func aggFunction(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func aggFunctionBlockedByCol(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	compileGQLToPSQLExpectErr(t, gql, nil, "anon")
}

func aggFunctionDisabled(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	compileGQLToPSQLExpectErr(t, gql, nil, "anon1")
}

func aggFunctionWithFilter(t *testing.T) {
	gql := `query {
		products(where: { id: { gt: 10 } }) {
			id
			max_price
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func syntheticTables(t *testing.T) {
	gql := `query {
		me {
			email
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func queryWithVariables(t *testing.T) {
	gql := `query {
		products(id: $PRODUCT_ID, where: { price: { eq: $PRODUCT_PRICE } }) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withWhereOnRelations(t *testing.T) {
	gql := `query {
		users(where: { 
				not: { 
					products: { 
						price: { gt: 3 }
					} 
				} 
			}) {
			id
			email
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func multiRoot(t *testing.T) {
	gql := `query {
		products {
			id
			name
			customer {
				vip
			}
		}
		users {
			id
			email
		}
		customers {
			id
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withFragment1(t *testing.T) {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	query {
		users {
			...userFields2
	
			avatar
			...userFields1
		}
	}
	
	fragment userFields2 on user {
		full_name
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withFragment2(t *testing.T) {
	gql := `
	query {
		users {
			...userFields2
	
			avatar
			...userFields1
		}
	}

	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withFragment3(t *testing.T) {
	gql := `

	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
		...userFields1
	}

	query {
		users {
			...userFields2
	
			avatar
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withFragment4(t *testing.T) {
	gql := `

	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
	}

	query {
		users {
			...userFields2
	
			avatar
			...userFields1
		}
	}`

	compileGQLToPSQL(t, gql, nil, "anon")
}

func withPolymorphicUnion(t *testing.T) {
	gql := `

	fragment userFields on user {
		id
		email
	}
	
	fragment productFields on product {
		id
		name
	}

	query {
		notifications {
			id
			subject {
				...on users {
					...userFields
				}
				...on products {
					...productFields
				}
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func withSkipAndIncludeDirectives(t *testing.T) {
	gql := `
	query {
		products(limit: 6) @include(ifVar: $test) {
			id
			name
		}
		users(limit: 3) @skip(ifVar: $test) {
			id
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func subscription(t *testing.T) {
	gql := `subscription test {
		users(id: $id) {
			id
			email
		}
	}`
	compileGQLToPSQL(t, gql, nil, "user")
}

// func remoteJoin(t *testing.T) {
// 	gql := `query {
// 		customers {
// 			email
// 			payments {
// 				customer_id
// 			}
// 		}
// 	}`

// 	compileGQLToPSQL(t, gql, nil, "user")
// }

// func withInlineFragment(t *testing.T) {
// 	gql := `
// 	query {
// 		users {
// 			... on users {
// 				id
// 				email
// 			}
// 			created_at
// 			... on user {
// 				first_name
// 				last_name
// 			}
// 		}
// 	}
// `

// 	compileGQLToPSQL(t, gql, nil, "anon")
// }

func withCursor(t *testing.T) {
	gql := `query {
		products(
			first: 20
			after: $cursor
			order_by: { price: desc }) {
			name
		}
		products_cursor
	}`

	vars := map[string]json.RawMessage{
		"cursor": json.RawMessage(`"0,1"`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func jsonColumnAsTable(t *testing.T) {
	gql := `query {
		products {
			id
			name
			tag_count {
				count
				tags {
					name
				}
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "admin")
}

func recursiveTableParents(t *testing.T) {
	gql := `query {
		reply : comments(id: $id) {
			id
			comments(find: "parents") {
				id
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"id": json.RawMessage(`2`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func recursiveTableChildren(t *testing.T) {
	gql := `query {
		comments(id: $id) {
			id
			replies: comments(find: "children") {
				id
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"id": json.RawMessage(`6`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func nullForAuthRequiredInAnon(t *testing.T) {
	gql := `query {
		products {
			id
			name
			user(where: { id: { eq: $user_id } }) {
				id
				email
			}
		}
	}`

	compileGQLToPSQL(t, gql, nil, "anon")
}

func blockedQuery(t *testing.T) {
	gql := `query {
		users(id: $id, where: { id: { gt: 3 } }) {
			id
			full_name
			email
		}
	}`

	compileGQLToPSQL(t, gql, nil, "bad_dude")
}

func blockedFunctions(t *testing.T) {
	gql := `query {
		users {
			count_id
			email
		}
	}`

	compileGQLToPSQLExpectErr(t, gql, nil, "bad_dude")
}

func multiRootSameTable(t *testing.T) {
	gql := `query {
		q1: products(where: { id: { eq: 3 } }) {
			id
			name
		}
		q2: products(where: { id: { eq: 4 } }) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func TestCompileQuery(t *testing.T) {
	t.Run("simpleQuery", simpleQuery)
	t.Run("withVariableLimit", withVariableLimit)
	t.Run("withComplexArgs", withComplexArgs)
	t.Run("withNestedOrderBy", withNestedOrderBy)
	t.Run("withWhereIn", withWhereIn)
	t.Run("withWhereAndList", withWhereAndList)
	t.Run("withWhereIsNull", withWhereIsNull)
	t.Run("withWhereMultiOr", withWhereMultiOr)
	t.Run("withNestedWhere", withNestedWhere)
	t.Run("withAlternateName", withAlternateName)
	t.Run("fetchByID", fetchByID)
	t.Run("searchQuery", searchQuery)
	t.Run("oneToMany", oneToMany)
	t.Run("oneToManyReverse", oneToManyReverse)
	t.Run("oneToManyArray", oneToManyArray)
	t.Run("manyToMany", manyToMany)
	t.Run("manyToManyReverse", manyToManyReverse)
	t.Run("aggFunction", aggFunction)
	t.Run("aggFunctionBlockedByCol", aggFunctionBlockedByCol)
	t.Run("aggFunctionDisabled", aggFunctionDisabled)
	t.Run("aggFunctionWithFilter", aggFunctionWithFilter)
	t.Run("syntheticTables", syntheticTables)
	t.Run("queryWithVariables", queryWithVariables)
	t.Run("withWhereOnRelations", withWhereOnRelations)
	t.Run("multiRoot", multiRoot)
	t.Run("withFragment1", withFragment1)
	t.Run("withFragment2", withFragment2)
	t.Run("withFragment3", withFragment3)
	t.Run("withFragment4", withFragment4)
	t.Run("withPolymorphicUnion", withPolymorphicUnion)
	t.Run("withSkipAndIncludeDirectives", withSkipAndIncludeDirectives)
	t.Run("subscription", subscription)
	// t.Run("remoteJoin", remoteJoin)
	// t.Run("withInlineFragment", withInlineFragment)
	t.Run("jsonColumnAsTable", jsonColumnAsTable)
	t.Run("recursiveTableParents", recursiveTableParents)
	t.Run("recursiveTableChildren", recursiveTableChildren)
	t.Run("withCursor", withCursor)
	t.Run("nullForAuthRequiredInAnon", nullForAuthRequiredInAnon)
	t.Run("blockedQuery", blockedQuery)
	t.Run("blockedFunctions", blockedFunctions)
	t.Run("multiRootSameTable", multiRootSameTable)
	t.Run("distinctWithAggCount", distinctWithAggCount)
	t.Run("distinctWithAggMultiple", distinctWithAggMultiple)
	t.Run("distinctWithAggAndWhere", distinctWithAggAndWhere)
	t.Run("aggWithoutDistinct", aggWithoutDistinct)
	t.Run("partitionFilterInSQL", partitionFilterInSQL)
	t.Run("warehouseColumnProjection", warehouseColumnProjection)
}

// --- distinct + aggregation tests ---
// These verify that GROUP BY uses only the distinct columns, not the PK.
// Bug: __gj_id (PK) was included in GROUP BY making every group unique (count=1).

func distinctWithAggCount(t *testing.T) {
	gql := `query {
		products(distinct: [name]) {
			name
			count_id
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")

	// GROUP BY should only contain the distinct column (name), not the PK (id)
	if bytes.Contains([]byte(sql), []byte(`GROUP BY`)) {
		if !bytes.Contains([]byte(sql), []byte(`"name"`)) {
			t.Error("GROUP BY should contain the distinct column 'name'")
		}
		// The PK 'id' should not appear as a raw column in GROUP BY
		// (it can appear inside count() which is fine)
		groupByIdx := bytes.Index([]byte(sql), []byte(`GROUP BY`))
		limitIdx := bytes.Index([]byte(sql), []byte(`LIMIT`))
		if limitIdx == -1 {
			limitIdx = len(sql)
		}
		groupByClause := sql[groupByIdx:limitIdx]
		// Check that the group by section doesn't contain a bare "products"."id"
		// outside of an aggregate function
		if bytes.Contains([]byte(groupByClause), []byte(`"id"`)) {
			t.Error("GROUP BY should NOT contain the PK 'id' when distinct + aggregation is used")
		}
	}
}

func distinctWithAggMultiple(t *testing.T) {
	gql := `query {
		products(distinct: [name]) {
			name
			count_id
			max_price
		}
	}`
	compileGQLToPSQL(t, gql, nil, "user")
}

func distinctWithAggAndWhere(t *testing.T) {
	gql := `query {
		products(distinct: [name], where: { price: { gt: 10 } }) {
			name
			count_id
		}
	}`
	compileGQLToPSQL(t, gql, nil, "user")
}

func aggWithoutDistinct(t *testing.T) {
	// Aggregation without distinct should still work (GROUP BY all BCols)
	gql := `query {
		products {
			name
			count_price
		}
	}`
	compileGQLToPSQL(t, gql, nil, "user")
}

// compileGQLToPSQLString compiles and returns the SQL string for inspection
func compileGQLToPSQLString(t *testing.T, gql string,
	vars map[string]json.RawMessage,
	role string,
) string {
	t.Helper()
	var v json.RawMessage
	var err error

	if v, err = json.Marshal(vars); err != nil {
		t.Fatal(err)
	}

	vm := make(map[string]json.RawMessage)
	if err := json.Unmarshal(v, &vm); err != nil {
		t.Fatal(err)
	}

	qc, err := qcompile.Compile([]byte(gql), vm, role, "")
	if err != nil {
		t.Fatal(err)
	}

	_, sqlBytes, err := pcompile.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	return string(sqlBytes)
}

func partitionFilterInSQL(t *testing.T) {
	// Self-contained test: create a partitioned schema + compilers
	pSchema, err := sdata.GetTestPartitionedSchema()
	if err != nil {
		t.Fatal(err)
	}

	pQCompile, err := qcode.NewCompiler(pSchema, qcode.Config{DBSchema: pSchema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	err = pQCompile.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	pPCompile := psql.NewCompiler(psql.Config{})

	gql := `query {
		products {
			id
			name
		}
	}`

	qc, err := pQCompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	_, sqlBytes, err := pPCompile.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := string(sqlBytes)

	// The generated SQL should contain the partition bound expression
	if !strings.Contains(sql, "CURRENT_TIMESTAMP - INTERVAL") {
		t.Errorf("expected partition bound in SQL, got:\n%s", sql)
	}

	// Should reference the partition column
	if !strings.Contains(sql, "created_at") {
		t.Errorf("expected 'created_at' in SQL, got:\n%s", sql)
	}

	t.Logf("Generated SQL:\n%s", sql)
}

func warehouseColumnProjection(t *testing.T) {
	// Snowflake: ORDER BY column not in user fields should NOT appear in inner SELECT
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	sfQCompile, err := qcode.NewCompiler(sfSchema, qcode.Config{DBSchema: sfSchema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	err = sfQCompile.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sfPCompile := psql.NewCompiler(psql.Config{})

	gql := `query {
		products(order_by: { price: desc }) {
			id
			name
		}
	}`

	qc, err := sfQCompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	_, sqlBytes, err := sfPCompile.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := string(sqlBytes)

	// The innermost SELECT column list should only have id and name, NOT price.
	// Extract the inner SELECT: between "SELECT " and " FROM" in the deepest subquery.
	// The SQL should have: SELECT "products"."id", "products"."name" FROM
	// NOT: SELECT "products"."id", "products"."name", "products"."price" FROM
	innerSelect := sql[strings.LastIndex(sql, `SELECT "products"."`):]
	innerSelect = innerSelect[:strings.Index(innerSelect, ` FROM`)]

	if strings.Contains(innerSelect, `"price"`) {
		t.Errorf("Snowflake: ORDER BY column 'price' should not be in inner SELECT columns.\nInner SELECT: %s", innerSelect)
	}

	// But ORDER BY should still reference price
	if !strings.Contains(sql, `ORDER BY`) || !strings.Contains(sql, `"price"`) {
		t.Errorf("Snowflake: ORDER BY clause should still reference 'price'.\nSQL: %s", sql)
	}

	t.Logf("Generated SQL:\n%s", sql)
}

var benchGQL = []byte(`query {
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
}`)

var result []byte

func BenchmarkCompile(b *testing.B) {
	var w bytes.Buffer

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		w.Reset()

		qc, err := qcompile.Compile(benchGQL, nil, "user", "")
		if err != nil {
			b.Fatal(err)
		}

		_, err = pcompile.Compile(&w, qc)
		if err != nil {
			b.Fatal(err)
		}
		result = w.Bytes()
	}
}

func BenchmarkCompileParallel(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var w bytes.Buffer

		for pb.Next() {
			w.Reset()

			qc, err := qcompile.Compile(benchGQL, nil, "user", "")
			if err != nil {
				b.Fatal(err)
			}

			_, err = pcompile.Compile(&w, qc)
			if err != nil {
				b.Fatal(err)
			}
			result = w.Bytes()
		}
	})
}
