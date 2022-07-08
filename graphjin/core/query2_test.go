package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/spf13/afero"
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)

	if err != nil {
		t.Error(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		t.Error(err)
		return
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
	fs := afero.NewBasePathFs(afero.NewOsFs(), dir)

	conf1 := newConfig(&core.Config{DBType: dbType})
	gj1, err := core.NewGraphJin(conf1, db, core.OptionSetFS(fs))
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

	res, err := gj2.GraphQL(context.Background(), gql2, nil, nil)
	if err != nil {
		t.Error(err)
		return
	}

	exp := `{"products": {"id": 2}}`
	got := string(res.Data)
	assert.Equal(t, exp, got, "should equal")
}

func TestAllowListWithNamespace(t *testing.T) {
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
	fs := afero.NewBasePathFs(afero.NewOsFs(), dir)

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

	_, err = gj2.GraphQL(context.Background(), gql2, nil,
		&core.ReqConfig{Namespace: core.Namespace{"api", true}})

	assert.ErrorContains(t, err, "not found in prepared statements")
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
					Production:       true,
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

func Example_queryVeryComplex() {
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
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), benchGQL, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	printJSON(res.Data)
	// Output: {"products":[{"category":[{"id":1,"name":"Category 1"},{"id":2,"name":"Category 2"}],"id":27,"name":"Product 27","owner":{"category_counts":[{"category":{"name":"Category 1"},"count":400},{"category":{"name":"Category 2"},"count":600}],"email":"user27@test.com","full_name":"User 27","picture":null},"price":37.5}]}
}

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
func TestCueValidationQuerySingleIntVarValue(t *testing.T) {
	gql := `query @validation(cue:"id:2") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationQuerySingleIntVarValue(t *testing.T) {
	gql := `query @validation(cue:"id:2") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":3}`), nil)
	if err == nil {
		t.Error("expected validation error")
	}
}
func TestCueValidationQuerySingleIntVarType(t *testing.T) {
	gql := `query @validation(cue:"id:int") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueValidationQuerySingleIntVarOR(t *testing.T) {
	gql := `query @validation(cue:"id: 3 | 2") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationQuerySingleIntVarOR(t *testing.T) {
	gql := `query @validation(cue:"id: 3 | 2") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":4}`), nil)
	if err == nil {
		t.Error(err)
	}
}
func TestCueValidationQuerySingleStringVarOR(t *testing.T) {
	// TODO: couldn't find a way to pass string inside cue through plain graphql query ( " )
	// (only way is using varibales and escape \")
	gql := `query @validation(cue:$validation) {
		users(where: {email:$mail}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"mail":"mail@example.com","validation":"mail: \"mail@example.com\" | \"mail@example.org\" "}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationQuerySingleStringVarOR(t *testing.T) {
	gql := `query @validation(cue:$validation) {
		users(where: {email:$mail}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"mail":"mail@example.net","validation":"mail: \"mail@example.com\" | \"mail@example.org\" "}`), nil)
	if err == nil {
		t.Error(err)
	}
}
func TestCueInvalidationQuerySingleIntVarType(t *testing.T) {
	gql := `query @validation(cue:"email:int") {
		users(where: {email:$email}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"email":"mail@example.com"}`), nil)
	if err == nil {
		t.Error("expected validation error")
	}
}
func TestCueValidationMutationMapVarStringsLen(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":105, "email":"mail1@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
		},
		"validation":"import (\"strings\"), inp: {id?: int, full_name: string & strings.MinRunes(3) & strings.MaxRunes(22), created_at:\"now\", updated_at:\"now\", email: string}"
	}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationMutationMapVarStringsLen(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":106, "email":"mail2@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
		},
		"validation":"import (\"strings\"), inp: {id?: int, full_name: string & strings.MinRunes(3) & strings.MaxRunes(22), created_at:\"now\", updated_at:\"now\", email: string}"
	}`), nil)
	if err == nil {
		t.Error(err)
	}
}

func TestCueValidationMutationMapVarIntMaxMin(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":101, "email":"mail3@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
		},
		"validation":" inp: {id?: int & >100 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
	}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationMutationMapVarIntMaxMin(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":107, "email":"mail4@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
		},
		"validation":"inp: {id?: int & >100 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
	}`), nil)
	if err == nil {
		t.Error(err)
	}
}
func TestCueValidationMutationMapVarOptionalKey(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":111, "email":"mail7@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
		},
		"validation":"inp: {id?: int, phone?: string, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
	}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueValidationMutationMapVarRegex(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":108, "email":"mail5@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
		},
		"validation":"inp: {id?: int & >100 & <110, full_name: string , created_at:\"now\", updated_at:\"now\", email: =~\"^[a-zA-Z0-9.!#$+%&'*/=?^_{|}\\\\-`+"`"+`~]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$\"}"
	}`), nil) // regex from : https://cuelang.org/play/?id=iFcZKx72Bwm#cue@export@cue
	if err != nil {
		t.Error(err)
		return
	}
}
func TestCueInvalidationMutationMapVarRegex(t *testing.T) {
	gql := `mutation @validation(cue:$validation) {
		users(insert:$inp) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
		"inp":{
			"id":109, "email":"mail6@ex`+"`"+`ample.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
		},
		"validation":"inp: {id?: int & >110 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: =~\"^[a-zA-Z0-9.!#$+%&'*/=?^_{|}\\\\-`+"`"+`~]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$\"}"
	}`), nil)
	if err == nil {
		t.Error(err)
	}
}
