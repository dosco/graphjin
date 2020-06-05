package qcode

import (
	"errors"
	"testing"

	"github.com/chirino/graphql/schema"
)

func TestCompile1(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	err := qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
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
		} }`), "user")

	if err == nil {
		t.Fatal(errors.New("this should be an error id must be a variable"))
	}
}

func TestCompile2(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	err := qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
	}

	_, err = qc.Compile([]byte(`
	query { product(id: $id) {
			id
			name
		} }`), "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestCompile3(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	err := qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
	}

	_, err = qc.Compile([]byte(`
	mutation {
		product(id: $test, name: "Test") {
			id
			name
		}
	}`), "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCompile1(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile([]byte(`#`), "user")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidCompile2(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile([]byte(`{u(where:{not:0})}`), "user")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestEmptyCompile(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile([]byte(``), "user")

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
}`
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile([]byte(gql), "anon")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}

}

func TestFragmentsCompile(t *testing.T) {
	gql := `
fragment userFields on user {
  name
  email
}
	
query { users { ...userFields } }`
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile([]byte(gql), "anon")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
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

func BenchmarkQCompile(b *testing.B) {
	qcompile, _ := NewCompiler(Config{})

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, err := qcompile.Compile(gql, "user")

		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQCompileP(b *testing.B) {
	qcompile, _ := NewCompiler(Config{})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := qcompile.Compile(gql, "user")

			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkParse(b *testing.B) {

	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		_, err := Parse(gql)

		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseP(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := Parse(gql)

			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSchemaParse(b *testing.B) {

	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		doc := schema.QueryDocument{}
		err := doc.Parse(string(gql))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSchemaParseP(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			doc := schema.QueryDocument{}
			err := doc.Parse(string(gql))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
