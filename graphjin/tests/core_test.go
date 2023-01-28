package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dosco/graphjin/conf/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

// nolint:errcheck
func TestReadInConfigWithEnvVars(t *testing.T) {
	devConfig := "secret_key: dev_secret_key\n"
	prodConfig := "inherits: dev\nsecret_key: \"prod_secret_key\"\n"

	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	fs := core.NewOsFS(dir)
	fs.Put("dev.yml", []byte(devConfig))
	fs.Put("prod.yml", []byte(prodConfig))

	c, err := conf.NewConfigWithFS(fs, "dev.yml")
	assert.NoError(t, err)
	assert.Equal(t, "dev_secret_key", c.SecretKey)

	c, err = conf.NewConfigWithFS(fs, "prod.yml")
	assert.NoError(t, err)
	assert.Equal(t, "prod_secret_key", c.SecretKey)

	// TODO: Issue with WASM
	// os.Setenv("GJ_SECRET_KEY", "new_dev_secret_key")
	// c, err = core.NewConfig(fs, "dev.yml")
	// assert.NoError(t, err)
	// assert.Equal(t, "new_dev_secret_key", c.SecretKey)

	// os.Setenv("GJ_SECRET_KEY", "new_prod_secret_key")
	// c, err = core.NewConfig(fs, "prod.yml")
	// assert.NoError(t, err)
	// assert.Equal(t, "new_prod_secret_key", c.SecretKey)

	// os.Unsetenv("GJ_SECRET_KEY")
}

func TestAPQ(t *testing.T) {
	gql := `query getProducts {
		products(id: 2) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = gj.GraphQL(context.Background(), gql, nil, &core.ReqConfig{
		APQKey: "getProducts",
	})
	if err != nil {
		t.Error(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), "", nil, &core.ReqConfig{
		APQKey: "getProducts",
	})
	if err != nil {
		t.Error(err)
		return
	}

	exp := `{"products": {"id": 2}}`
	got := string(res.Data)
	assert.Equal(t, exp, got, "should equal")
}

func TestAllowList(t *testing.T) {
	gql1 := `
	query getProducts {
		products(id: 2) {
			id
		}
	}`

	gql2 := `
	query getProducts {
		products(id: 3) {
			id
			name
		}
	}`

	gql3 := `
	query getUsers {
		users(id: 3) {
			id
			name
		}
	}`

	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	fs := core.NewOsFS(dir)
	err = fs.Put("queries/getProducts.gql", []byte(gql1))
	if err != nil {
		t.Error(err)
		return
	}

	conf1 := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj1, err := core.NewGraphJin(conf1, db, core.OptionSetFS(fs))
	if err != nil {
		t.Error(err)
		return
	}

	exp1 := `{"products": {"id": 2}}`

	res1, err := gj1.GraphQL(context.Background(), gql1, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp1, string(res1.Data))

	conf2 := newConfig(&core.Config{DBType: dbType, Production: true})
	gj2, err := core.NewGraphJin(conf2, db, core.OptionSetFS(fs))
	assert.NoError(t, err)

	res2, err := gj2.GraphQL(context.Background(), gql2, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp1, string(res2.Data))

	res3, err := gj2.GraphQLByName(context.Background(), "getProducts", nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp1, string(res3.Data))

	_, err = gj2.GraphQL(context.Background(), gql3, nil, nil)
	assert.ErrorContains(t, err, "unknown graphql query")
}

func TestAllowListWithNamespace(t *testing.T) {
	gql1 := `
	fragment Product on products {
		id
		name
	}
	query getProducts {
		products(id: 2) {
			...Product
		}
	}`

	gql2 := `query getProducts {
		products(id: 3) {
			id
			name
		}
	}`

	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	fs := core.NewOsFS(dir)

	conf1 := newConfig(&core.Config{DBType: dbType})
	gj1, err := core.NewGraphJin(conf1, db,
		core.OptionSetFS(fs), core.OptionSetNamespace(("web")))
	if err != nil {
		t.Error(err)
		return
	}

	_, err = gj1.GraphQL(context.Background(), gql1, nil, nil)
	if err != nil {
		t.Error(err)
		return
	}

	conf2 := newConfig(&core.Config{DBType: dbType, Production: true})
	gj2, err := core.NewGraphJin(conf2, db, core.OptionSetFS(fs))
	if err != nil {
		t.Error(err)
		return
	}

	var rc core.ReqConfig
	rc.SetNamespace("api")

	_, err = gj2.GraphQL(context.Background(), gql2, nil, &rc)
	assert.ErrorContains(t, err, "unknown graphql query")
}

func TestDisableProdSecurity(t *testing.T) {
	gql1 := `
	query getProducts {
		products(id: 2) {
			id
		}
	}`

	gql2 := `
	query getProducts {
		products(id: 3) {
			id
		}
	}`

	conf1 := newConfig(&core.Config{DBType: dbType, Production: true})
	gj1, err := core.NewGraphJin(conf1, db)
	if err != nil {
		panic(err)
	}

	_, err = gj1.GraphQL(context.Background(), gql1, nil, nil)
	assert.ErrorContains(t, err, "unknown graphql query")

	conf2 := newConfig(&core.Config{
		DBType:              dbType,
		Production:          true,
		DisableProdSecurity: true,
	})
	gj2, err := core.NewGraphJin(conf2, db)
	if err != nil {
		panic(err)
	}

	res, err := gj2.GraphQL(context.Background(), gql1, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, `{"products": {"id": 2}}`, string(res.Data))

	res, err = gj2.GraphQL(context.Background(), gql2, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, `{"products": {"id": 3}}`, string(res.Data))
}

func TestEnableSchema(t *testing.T) {
	gql := `
	fragment Product on products {
		id
		name
	}
	query getProducts {
		products(id: 2) {
			...Product
		}
	}`

	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fs := core.NewOsFS(dir)

	conf1 := newConfig(&core.Config{DBType: dbType, EnableSchema: true})
	gj1, err := core.NewGraphJin(conf1, db, core.OptionSetFS(fs))
	if err != nil {
		panic(err)
	}

	res1, err := gj1.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		panic(err)
	}
	assert.Equal(t, stdJSON(res1.Data), `{"products":{"id":2,"name":"Product 2"}}`)

	time.Sleep(3 * time.Second)

	conf2 := newConfig(&core.Config{DBType: dbType, EnableSchema: true, Production: true})
	gj2, err := core.NewGraphJin(conf2, db, core.OptionSetFS(fs))
	if err != nil {
		panic(err)
	}

	res2, err := gj2.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		panic(err)
	}
	assert.Equal(t, stdJSON(res2.Data), `{"products":{"id":2,"name":"Product 2"}}`)
}

func TestConfigReuse(t *testing.T) {
	gql := `query {
		products(id: 2) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	for i := 0; i < 50; i++ {
		gj1, err := core.NewGraphJin(conf, db)
		if err != nil {
			panic(err)
		}

		res1, err := gj1.GraphQL(context.Background(), gql, nil, nil)
		if err != nil {
			panic(err)
		}

		gj2, err := core.NewGraphJin(conf, db)
		if err != nil {
			panic(err)
		}
		res2, err := gj2.GraphQL(context.Background(), gql, nil, nil)
		if err != nil {
			panic(err)
		}
		assert.Equal(t, res1.Data, res2.Data, "should equal")
	}
}

func TestConfigRoleManagement(t *testing.T) {
	conf := newConfig(&core.Config{})

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

	g := errgroup.Group{}

	for i := 0; i < 10; i++ {
		x := i
		g.Go(func() error {
			for n := 0; n < 10; n++ {
				conf := newConfig(&core.Config{
					DBType:           dbType,
					Production:       false,
					DisableAllowList: true,
					Tables: []core.Table{
						{Name: "me", Table: "users"},
					},
				})

				err := conf.AddRoleTable("user", "me", core.Query{
					Filters: []string{"{ id: { eq: $user_id } }"},
				})
				if err != nil {
					return err
				}

				gj, err := core.NewGraphJin(conf, db)
				if err != nil {
					return fmt.Errorf("%d: %w", x, err)
				}

				ctx := context.WithValue(context.Background(), core.UserIDKey, x)
				_, err = gj.GraphQL(ctx, gql, nil, nil)
				if err != nil {
					return fmt.Errorf("%d: %w", x, err)
				}
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
		# returns only 1 items
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

var resultJSON json.RawMessage

func BenchmarkCompile(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
		panic(err)
	}

	for n := 0; n < b.N; n++ {
		res, err := gj.GraphQL(context.Background(), benchGQL, vars, nil)
		if err != nil {
			panic(err)
		}

		resultJSON = res.Data
	}
}
