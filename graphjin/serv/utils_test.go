package serv

import (
	"strings"
	"testing"
)

func TestRelaxHash(t *testing.T) {
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

	h1 := relaxHash(v1)
	h2 := relaxHash(v2)

	if strings.Compare(h1, h2) != 0 {
		t.Fatal("Hashes don't match they should")
	}

}
