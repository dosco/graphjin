package serv

import (
	"strings"
	"testing"
)

func TestRelaxHash1(t *testing.T) {
	var v1 = []byte(`
	products(
		limit: 30,

		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}`)

	var v2 = []byte(`
	products
	(limit: 30, where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
			name
			   price
	}  `)

	h1 := gqlHash(v1)
	h2 := gqlHash(v2)

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestRelaxHash2(t *testing.T) {
	var v1 = []byte(`
	{
		products(
			limit: 30
			order_by: { price: desc }
			distinct: [price]
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
		) {
			id
			name
			price
			user {
				id
				email
			}
		}
	}`)

	var v2 = []byte(` { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } `)

	h1 := gqlHash(v1)
	h2 := gqlHash(v2)

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}
