---
id: config-go
title: Configuration in Go (SG as a library)
sidebar_label: Configuration in Go
---

The configuration is the same as [that in yaml](https://supergraph.dev/docs/config), is obviously written in Go and obviously is just about the `core` pkg (SG as a library).

We've tried to ensure that the config file is self-documenting and easy to work with.

```go
core.Config{
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

	//Blocklist is a list of tables and columns that should be filtered out from any and all queries
	Blocklist: []string{"password", "secrets"},

	//Tables contains all table specific configuration such as aliased tables
	//creating relationships between tables, etc
	Tables: []core.Table{
		{
			Name:      "players",
			Table:     "players",
			Type:      "",
			Blocklist: []string{"account_id"},
			Remotes: []core.Remote{{Name: "", ID: "", Path: "", URL: "", Debug: false, PassHeaders: []string{""}, SetHeaders: []struct {
				Name  string
				Value string
			}{}}},
			Columns: []core.Column{{Name: "", Type: "", ForeignKey: ""}}},
	},

	//RolesQuery if set enabled attributed based access control.
	//This query is use to fetch the user attributes that then dynamically define the users role.
	RolesQuery: "",

	//Roles contains all the configuration for all the roles you want to support `user` and `anon`
	//are two default roles. User role is for when a user ID is available and Anon when it's not.
	//If you're using the RolesQuery config to enable atribute based acess control then you can add more custom roles.
	Roles: []core.Role{
		{
			Name:  "",
			Match: "",
			Tables: []core.RoleTable{
				{
					Name:     "",
					ReadOnly: false,
					Query: &core.Query{
						Limit:            10,
						Filters:          []string{},
						Columns:          []string{},
						DisableFunctions: false,
						Block:            false,
					},
					Insert: &core.Insert{
						Filters: []string{},
						Columns: []string{},
						Presets: map[string]string{},
						Block:   false,
					},
					Update: &core.Update{
						Filters: nil,
						Columns: nil,
						Presets: nil,
						Block:   false,
					},
					Delete: &core.Delete{
						Filters: nil,
						Columns: nil,
						Block:   false,
					}},
			},
		},
	},

	//Inflections is to add additionally singular to plural mappings to the engine (eg. sheep: sheep)
	Inflections: map[string]string{},

	//Database schema name. Defaults to 'public'
	DBSchema: "",

	//Log warnings and other debug information
	Debug: false,
}
```