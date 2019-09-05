package serv

import (
	"strings"
	"testing"
)

func TestRelaxHash1(t *testing.T) {
	var v1 = `
	products(
		limit: 30,

		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}`

	var v2 = `
	products
	(limit: 30, where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
			name
			   price
	}  `

	h1 := gqlHash(v1, nil)
	h2 := gqlHash(v2, nil)

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestRelaxHash2(t *testing.T) {
	var v1 = `
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
	}`

	var v2 = ` { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } `

	h1 := gqlHash(v1, nil)
	h2 := gqlHash(v2, nil)

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestRelaxHashWithVars1(t *testing.T) {
	var q1 = `
	products(
		limit: 30,

		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}`

	var v1 = `
	{
		"insert": {
			"name": "Hello",
			"description": "World",
			"created_at": "now",
			"updated_at": "now",
			"test": { "type2": "b", "type1": "a" }
		},
		"user": 123
	}`

	var q2 = `
	products
	(limit: 30, where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
			name
			   price
	}  `

	var v2 = `{
		"insert": {
			"created_at": "now",
			"test": { "type1": "a", "type2": "b" },
			"name": "Hello",
			"updated_at": "now",
			"description": "World"
		},
		"user": 123
	}`

	h1 := gqlHash(q1, []byte(v1))
	h2 := gqlHash(q2, []byte(v2))

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestRelaxHashWithVars2(t *testing.T) {
	var q1 = `
	products(
		limit: 30,

		where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
	}`

	var v1 = `
	{
		"insert": [{
			"name": "Hello",
			"description": "World",
			"created_at": "now",
			"updated_at": "now",
			"test": { "type2": "b", "type1": "a" }
		},
		{
			"name": "Hello",
			"description": "World",
			"created_at": "now",
			"updated_at": "now",
			"test": { "type2": "b", "type1": "a" }
		}],
		"user": 123
	}`

	var q2 = `
	products
	(limit: 30, where: { id: { AND: { greater_or_equals: 20, lt: 28 } } }) {
		id
			name
			   price
	}  `

	var v2 = `{
		"insert": {
			"created_at": "now",
			"test": { "type1": "a", "type2": "b" },
			"name": "Hello",
			"updated_at": "now",
			"description": "World"
		},
		"user": 123
	}`

	h1 := gqlHash(q1, []byte(v1))
	h2 := gqlHash(q2, []byte(v2))

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}
