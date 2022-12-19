package graph

import (
	"testing"
)

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

func BenchmarkParseFragment(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		_, err := Parse(gqlWithFragments)

		if err != nil {
			b.Fatal(err)
		}
	}
}
