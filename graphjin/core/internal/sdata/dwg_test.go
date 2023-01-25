package sdata_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/stretchr/testify/assert"
)

func TestDWG(t *testing.T) {
	for i := 0; i < 10000; i++ {
		s, err := sdata.GetTestSchema()
		assert.NoError(t, err)

		paths, err := s.FindPath("customers", "users", "")
		assert.NoError(t, err)

		assert.Equal(t,
			`(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`,
			paths[0].String())

		paths, err = s.FindPath("purchases", "users", "")
		assert.NoError(t, err)

		assert.Equal(t,
			`(public.purchases) public.purchases.customer_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.customers.id ==> RelOneToOne ==> (public.customers) public.customers.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`,
			paths[0].String())

		assert.Equal(t,
			`(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`,
			paths[1].String())
	}
}
