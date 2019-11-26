package serv

import (
	"strings"
	"testing"
)

func TestGQLHash1(t *testing.T) {
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

	h1 := gqlHash(v1, nil, "")
	h2 := gqlHash(v2, nil, "")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLHash2(t *testing.T) {
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

	h1 := gqlHash(v1, nil, "")
	h2 := gqlHash(v2, nil, "")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLHash3(t *testing.T) {
	var v1 = `users {
			id
			email
			picture: avatar
			products(limit: 2, where: {price: {gt: 10}}) {
				id
				name
				description
			}
		}`

	var v2 = `
		users {
			id
			email
			picture: avatar
			products(limit: 2, where: {price: {gt: 10}}) {
				id
				name
				description
			}
		}
`

	h1 := gqlHash(v1, nil, "")
	h2 := gqlHash(v2, nil, "")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLHash4(t *testing.T) {
	var v1 = `
	query {
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

	var v2 = `   { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } `

	h1 := gqlHash(v1, nil, "")
	h2 := gqlHash(v2, nil, "")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLHashWithVars1(t *testing.T) {
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

	h1 := gqlHash(q1, []byte(v1), "user")
	h2 := gqlHash(q2, []byte(v2), "user")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLHashWithVars2(t *testing.T) {
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

	h1 := gqlHash(q1, []byte(v1), "user")
	h2 := gqlHash(q2, []byte(v2), "user")

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}
}

func TestGQLName1(t *testing.T) {
	var q = `
	query {
		products(
			distinct: [price]
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
		) { id name } }`

	name := gqlName(q)

	if len(name) != 0 {
		t.Fatal("Name should be empty, not ", name)
	}
}

func TestGQLName2(t *testing.T) {
	var q = `
	query hakuna_matata {
		products(
			distinct: [price]
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
		) {
			id
			name
		}
	}`

	name := gqlName(q)

	if name != "hakuna_matata" {
		t.Fatal("Name should be 'hakuna_matata', not ", name)
	}
}

func TestGQLName3(t *testing.T) {
	var q = `
	mutation means{ users { id } }`

	// var v2 = `   { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } `

	name := gqlName(q)

	if name != "means" {
		t.Fatal("Name should be 'means', not ", name)
	}
}

func TestGQLName4(t *testing.T) {
	var q = `
	query no_worries 
		users {
			id
		}
	}`

	name := gqlName(q)

	if name != "no_worries" {
		t.Fatal("Name should be 'no_worries', not ", name)
	}
}

func TestGQLName5(t *testing.T) {
	var q = `
	    {
		users {
			id
		}
	}`

	name := gqlName(q)

	if len(name) != 0 {
		t.Fatal("Name should be empty, not ", name)
	}
}
