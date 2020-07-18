---
id: library
title: Use in your own GO code
sidebar_label: Use as a Library
---

Super Graph can be used as a library in an already existing project. The best part is that your API need not even be a GraphQL one. You can simply use Super Graph as an alternative to a GO ORM library or directly writing SQL. The following code is just a simple example:

```go
package main

import (
	"database/sql"
	"github.com/dosco/super-graph/core"
	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	_ "github.com/jackc/pgx/v4/stdlib"
)

func New(cfg *Cfg) *core.SuperGraph {
	dbConn, err := sql.Open("pgx", cfg.DB_URL)
	//check err

	superGraphConfig := NewConfig(cfg)

	supergraph, err := core.NewSuperGraph(&superGraphConfig, dbConn)
	//check err

	return supergraph
}

func Handler(superGraph *core.SuperGraph) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := getBodyFromRequest(w, r)
		//check err

		ctx := context.WithValue(r.Context(), core.UserIDKey, GetYourUserID())

		res, err := superGraph.GraphQL(ctx, body.Query, body.Variables)

		if err != nil {
			//check err
			return
		}

		render.JSON(w, r, res)
	}
}

func main() {
  superGraph := New(config)

  r := chi.NewRouter()

  r.Post("/api", Handler(superGraph))

  StartServer()
}
```

## Config Explained

The configuration is the same as [that in yaml](https://supergraph.dev/docs/config) except for that it is obviously written in Go and is just about configuring the `core` package (aka Super Graph library). We've tried to ensure that the config file is self-documenting and easy to work with. A config object is not required Super Graph can learn your database structure and be useful even when a config is not provided.

```go
conf := core.Config{
	//SecretKey is used to encrypt opaque values such as the cursor. Auto-generated if not set
	SecretKey: "[YOU_SHOULD_CHANGE_THIS]",

	//UseAllowList (aka production mode) when set to true ensures only queries lists
	//in the allow.list file can be used. All queries are pre-prepared so no compiling
	//happens and things are very fast.
	UseAllowList: false,

	//AllowListFile if the path to allow list file if not set the path is assumed
	//to be the same as the config path (allow.list)
	AllowListFile: "",

	//SetUserID forces the database session variable `user.id` to be set to the user id.
	//This variables can be used by triggers or other database functions
	SetUserID: false,

	//DefaultBlock ensures that in anonymous mode (role 'anon') all tables are blocked
	//from queries and mutations. To open access to tables in anonymous mode
	//they have to be added to the 'anon' role config.
	DefaultBlock: false,

	//Vars is a map of hardcoded variables that can be leveraged in your queries
	//(e.g. variable admin_id will be $admin_id in the query)
	Vars: map[string]string{
		"account_id": "sql:select account_id from users where id = $user_id",
		"team_id":    "123",
	},

	//Blocklist is a list of tables and columns that should be filtered out
	// from any and all queries
	Blocklist: []string{"password", "secrets", "credit_card_number"},

	//Tables contains all table specific configuration such as aliased tables
	//creating relationships between tables, etc
	Tables: []core.Table{},

	//RolesQuery if set enabled attributed based access control.
	//This query is use to fetch the user attributes that then dynamically define the users role.
	RolesQuery: "",

	//Roles contains all the configuration for all the roles you want to support `user` and `anon`
	//are two default roles. User role is for when a user ID is available and Anon when it's not.
	//If you're using the RolesQuery config to enable atribute based acess control then you
	// can add more custom roles.

	// Use .AddRoleTable(roleName, tableName, roleTableConfig) to set this.
	// example below
	Roles: []core.Role{}

	//Inflections is to add additionally singular to plural
	// mappings to the engine (eg. sheep: sheep)
	Inflections: map[string]string{},

	//Database schema name. Defaults to 'public'
	DBSchema: "",

	//Log warnings and other debug information
	Debug: false,
}

conf.AddRoleTable("user", "table_name", core.Query{
	Limit:            10,
	Filters:          []string{},
	Columns:          []string{},
	DisableFunctions: false,
	Block:            false,
});

conf.AddRoleTable("user", "table_name", core.Insert{
	Filters: []string{},
	Columns: []string{},
	Presets: map[string]string{},
	Block:   false,
})

...

```

::: note
If you're using a Postgres schema other than the default `public` then in addition to setting the `DBSchema` config param you also have to set the `search_path` runtime parameter on the DB connection itself. https://github.com/dosco/super-graph/issues/134#issuecomment-659562003
:::
