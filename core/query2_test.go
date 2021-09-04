package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

func TestQuery(t *testing.T) {
	t.Run("queryWithVariableLimit", queryWithVariableLimit)
}

func queryWithVariableLimit(t *testing.T) {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		t.Error(err)
	}

	switch dbType {
	case "mysql":
		assert.NotNil(t, err)
	default:
		exp := `{"products": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5}, {"id": 6}, {"id": 7}, {"id": 8}, {"id": 9}, {"id": 10}]}`
		got := string(res.Data)
		assert.Equal(t, exp, got, "should equal")
	}
}

func TestAPQ(t *testing.T) {
	gql := `query getProducts {
		products(id: 2) {
			id
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, nil, &core.ReqConfig{
		APQKey: "getProducts",
	})
	if err != nil {
		t.Error(err)
	}

	res, err := gj.GraphQL(context.Background(), "", nil, &core.ReqConfig{
		APQKey: "getProducts",
	})
	if err != nil {
		t.Error(err)
	}

	exp := `{"products": {"id": 2}}`
	got := string(res.Data)
	assert.Equal(t, exp, got, "should equal")
}

func TestAllowList(t *testing.T) {
	gql1 := `query getProducts {
		products(id: 2) {
			id
		}
	}`

	gql2 := `query getProducts {
		products(id: 3) {
			id
			name
		}
	}`

	dir, err := ioutil.TempDir("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	conf1 := &core.Config{DBType: dbType, AllowListPath: dir}
	gj1, err := core.NewGraphJin(conf1, db)
	if err != nil {
		t.Error(err)
	}

	_, err = gj1.GraphQL(context.Background(), gql1, nil, nil)
	if err != nil {
		t.Error(err)
	}

	conf2 := &core.Config{DBType: dbType, AllowListPath: dir, Production: true}
	gj2, err := core.NewGraphJin(conf2, db)
	if err != nil {
		t.Error(err)
	}

	res, err := gj2.GraphQL(context.Background(), gql2, nil, nil)
	if err != nil {
		t.Error(err)
	}

	exp := `{"products": {"id": 2}}`
	got := string(res.Data)
	assert.Equal(t, exp, got, "should equal")
}

func TestConfigReuse(t *testing.T) {
	gql := `query {
		products(id: 2) {
			id
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj1, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	res1, err := gj1.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Error(err)
	}

	gj2, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	res2, err := gj2.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Error(err)
	}

	assert.Equal(t, res1.Data, res2.Data, "should equal")
}

func TestConfigRoleManagement(t *testing.T) {
	conf := &core.Config{}
	err := conf.AddRoleTable("test1", "products", core.Query{
		Columns: []string{"id", "name"},
	})
	if err != nil {
		panic(err)
	}
	assert.NotEmpty(t, conf.Roles)

	if err := conf.RemoveRoleTable("test1", "products"); err != nil {
		panic(err)
	}
	assert.Empty(t, conf.Roles)
}

func TestParallelRuns(t *testing.T) {
	gql := `query {
		me {
			id
			email
			products {
				id
			}
		}
	}`

	// TODO: introspection engine has race condition in dev
	// mode.
	conf := &core.Config{Production: true, DisableAllowList: true,
		Tables: []core.Table{
			{Name: "me", Table: "users"},
		},
	}

	err := conf.AddRoleTable("user", "me", core.Query{
		Filters: []string{"{ id: { eq: $user_id } }"},
	})
	if err != nil {
		panic(err)
	}

	g := errgroup.Group{}

	for i := 0; i < 10; i++ {
		x := i
		g.Go(func() error {
			for n := 0; n < 10; n++ {
				gj, err := core.NewGraphJin(conf, db)
				if err != nil {
					return fmt.Errorf("%d: %w", x, err)
				}

				ctx := context.WithValue(context.Background(), core.UserIDKey, x)
				_, err = gj.GraphQL(ctx, gql, nil, nil)
				if err != nil {
					return fmt.Errorf("%d: %w", x, err)
				}
				// fmt.Println(x, ">", string(res.Data))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Error(err)
	}
}

var benchGQL = `query {
	products(
		# returns only 30 items
		limit: 1,

		# starts from item 10, commented out for now
		# offset: 10,

		# orders the response items by highest price
		order_by: { price: desc },

		# only items with an id >= 30 and < 30 are returned
		where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
		id
		name
		price
		owner {
			full_name
			picture : avatar
			email
			category_counts(limit: 2) {
				count
				category {
					name
				}
			}
		}
		category(limit: 2) {
			id
			name
		}
	}
}`

func Example_veryComplexQuery() {
	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Tables = []core.Table{
		{
			Name:  "category_counts",
			Table: "users",
			Type:  "json",
			Columns: []core.Column{
				{Name: "category_id", Type: "int", ForeignKey: "categories.id"},
				{Name: "count", Type: "int"},
			},
		},
		{
			Name:    "products",
			Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}},
		},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), benchGQL, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(string(res.Data))
	// Output: {"products": [{"id": 27, "name": "Product 27", "owner": {"email": "user27@test.com", "picture": null, "full_name": "User 27", "category_counts": [{"count": 400, "category": {"name": "Category 1"}}, {"count": 600, "category": {"name": "Category 2"}}]}, "price": 37.5, "category": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}]}]}
}

var resultJSON json.RawMessage

func BenchmarkCompile(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Tables = []core.Table{
		{
			Name:  "category_counts",
			Table: "users",
			Type:  "json",
			Columns: []core.Column{
				{Name: "category_id", Type: "int", ForeignKey: "categories.id"},
				{Name: "count", Type: "int"},
			},
		},
		{
			Name:    "products",
			Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}},
		},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		b.Error(err)
	}

	for n := 0; n < b.N; n++ {
		res, err := gj.GraphQL(context.Background(), benchGQL, vars, nil)
		if err != nil {
			b.Fatal(err)
		}

		resultJSON = res.Data
	}
}
