package qcode

import (
	"errors"
	"testing"
)

func TestCompile1(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
			Columns: []string{"id", "Name"},
		},
	})

	_, err := qc.Compile([]byte(`
	{ product(id: 15) {
			id
			name
		} }`), "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestCompile2(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
			Columns: []string{"ID"},
		},
	})

	_, err := qc.Compile([]byte(`
	query { product(id: 15) {
			id
			name
		} }`), "user")

	if err != nil {
		t.Fatal(err)
	}
}

func TestCompile3(t *testing.T) {
	qc, _ := NewCompiler(Config{})
	qc.AddRole("user", "product", TRConfig{
		Query: QueryConfig{
			Columns: []string{"ID"},
		},
	})

	_, err := qc.Compile([]byte(`
	mutation {
		product(id: 15, name: "Test") {
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

var gql = []byte(`
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
		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}`)

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
