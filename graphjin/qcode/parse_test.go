package qcode

import (
	"errors"
	"testing"
)

/*
func compareOp(op1, op2 Operation) error {
	if op1.Type != op2.Type {
		return errors.New("operator type mismatch")
	}

	if op1.Name != op2.Name {
		return errors.New("operator name mismatch")
	}

	if len(op1.Args) != len(op2.Args) {
		return errors.New("operator args length mismatch")
	}

	for i := range op1.Args {
		if !reflect.DeepEqual(op1.Args[i], op2.Args[i]) {
			return fmt.Errorf("operator args: %v != %v", op1.Args[i], op2.Args[i])
		}
	}

	if len(op1.Fields) != len(op2.Fields) {
		return errors.New("operator field length mismatch")
	}

	for i := range op1.Fields {
		if !reflect.DeepEqual(op1.Fields[i].Args, op2.Fields[i].Args) {
			return fmt.Errorf("operator field args: %v != %v", op1.Fields[i].Args, op2.Fields[i].Args)
		}
	}

	for i := range op1.Fields {
		if !reflect.DeepEqual(op1.Fields[i].Children, op2.Fields[i].Children) {
			return fmt.Errorf("operator field fields: %v != %v", op1.Fields[i].Children, op2.Fields[i].Children)
		}
	}

	return nil
}
*/

func TestCompile(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})

	_, err := qcompile.CompileQuery([]byte(`
	product(id: 15) {
			id
			name
		}`))

	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCompile1(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.CompileQuery([]byte(`#`))

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidCompile2(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.CompileQuery([]byte(`{u(where:{not:0})}`))

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestEmptyCompile(t *testing.T) {
	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.CompileQuery([]byte(``))

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
		_, err := qcompile.CompileQuery(gql)

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
			_, err := qcompile.CompileQuery(gql)

			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
