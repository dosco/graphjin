package sdata_test

import (
	"testing"

	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/stretchr/testify/assert"
)

func TestDWG(t *testing.T) {
	for i := 0; i < 10000; i++ {
		s, err := sdata.GetTestSchema()
		assert.NoError(t, err)

		paths, err := s.FindPath("customers", "users", "")
		assert.NoError(t, err)

		assert.Equal(t,
			`(public.customers) public.customers.user_id -FK-> public.users.id ==> RelOneToOne ==> (public.users) public.users.id`,
			paths[0].String())

		paths, err = s.FindPath("purchases", "users", "")
		assert.NoError(t, err)

		assert.Equal(t,
			`(public.purchases) public.purchases.customer_id -FK-> public.customers.id ==> RelOneToOne ==> (public.customers) public.customers.id`,
			paths[0].String())

		assert.Equal(t,
			`(public.customers) public.customers.user_id -FK-> public.users.id ==> RelOneToOne ==> (public.users) public.users.id`,
			paths[1].String())
	}

}
