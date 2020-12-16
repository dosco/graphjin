package qcode_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

var dbs *sdata.DBSchema

func init() {
	var err error

	dbs, err = sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)
	if err != nil {
		panic(err)
	}
}

func TestCompile1(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "Name"},
		},
	})
	if err != nil {
		t.Error(err)
	}

	_, err = qc.Compile([]byte(`
	query { product(id: 15) {
			id
			name
		} }`), nil, "user")

	if err == nil {
		t.Fatal(errors.New("this should be an error id must be a variable"))
	}
}

func TestCompile2(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
	}

	_, err = qc.Compile([]byte(`
	query { product(id: $id) {
			id
			price	
		} }`), nil, "user")

	if err == nil {
		t.Fatal(errors.New("expected an error: 'products.price' blocked"))
	}
}

func TestCompile3(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
	}

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	_, err = qc.Compile([]byte(`
	mutation {
		product(insert: $data) {
			id
			name
		}
	}`), vars, "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCompile1(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(`#`), nil, "user")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidCompile2(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(`{u(where:{not:0})}`), nil, "user")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestEmptyCompile(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(``), nil, "user")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidPostfixCompile(t *testing.T) {
	gql := `mutation 
updateThread {
  thread(update: $data, where: { slug: { eq: $slug } }) {
    slug
    title
    published
    createdAt : created_at
    totalVotes : cached_votes_total
    totalPosts : cached_posts_total
    vote : thread_vote(where: { user_id: { eq: $user_id } }) {
     id
    }
    topics {
      slug
      name
    }
	}
}
}}`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "anon")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}

}

func TestFragmentsCompile1(t *testing.T) {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}
	`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestFragmentsCompile2(t *testing.T) {
	gql := `
	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}

	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestFragmentsCompile3(t *testing.T) {
	gql := `
	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}

	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}

	`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user")

	if err != nil {
		t.Fatal(err)
	}
}

var gql = []byte(`
	{products(
		# returns only 30 items
		limit: 30,

		# starts from item 10, commented out for now
		# offset: 10,

		# orders the response items by highest price
		order_by: { price: desc },

		# no duplicate prices returned
		distinct: [ price ]

		# only items with an id >= 30 and < 30 are returned
		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}}`)

var gqlWithFragments = []byte(`
fragment userFields1 on user {
	id
	email
	__typename
}

query {
	users {
		...userFields2

		created_at
		...userFields1
		__typename
	}
}

fragment userFields2 on user {
	first_name
	last_name
	__typename
}`)

func BenchmarkQCompile(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, err := qcompile.Compile(gql, nil, "user")

		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQCompileP(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := qcompile.Compile(gql, nil, "user")

			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkQCompileFragment(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		_, err := qcompile.Compile(gqlWithFragments, nil, "user")

		if err != nil {
			b.Fatal(err)
		}
	}
}
