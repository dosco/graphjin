package sdata_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/assert"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestDWG(t *testing.T) {
	for i := 0; i < 10000; i++ {
		s, err := sdata.GetTestSchema()
		assert.NoErrorFatal(t, err)

		paths, err := s.FindPath("customers", "users", "")
		assert.NoErrorFatal(t, err)

		exp := `(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got := paths[0].String()
		assert.Equals(t, exp, got)

		paths, err = s.FindPath("purchases", "users", "")
		assert.NoErrorFatal(t, err)

		exp = `(public.purchases) public.purchases.customer_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.customers.id ==> RelOneToOne ==> (public.customers) public.customers.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got = paths[0].String()
		assert.Equals(t, exp, got)

		exp = `(public.customers) public.customers.user_id [id:0, type:bigint, array:false, notNull:false, fulltext:false] -> public.users.id ==> RelOneToOne ==> (public.users) public.users.id [id:0, type:bigint, array:false, notNull:true, fulltext:false]`
		got = paths[1].String()
		assert.Equals(t, exp, got)

	}
}
