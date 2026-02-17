package tests_test

import (
	"github.com/dosco/graphjin/core/v3"
)

// requireMultiDB returns true if running in multi-DB mode (for Example_ skip logic)
func requireMultiDB() bool {
	return multiDBMode
}

// newMultiDBConfig creates a Config for multi-database tests
func newMultiDBConfig(conf *core.Config) *core.Config {
	if !multiDBMode {
		return newConfig(conf)
	}

	conf.DBType = "postgres" // Default/primary database
	conf.DBSchemaPollDuration = -1

	// Configure multi-DB
	conf.Databases = map[string]core.DatabaseConfig{
		"postgres": {
			Type: "postgres",
		},
		"sqlite": {
			Type: "sqlite",
		},
		"mongodb": {
			Type: "mongodb",
		},
	}

	return conf
}

// multiDBTables returns table-to-database mappings for multi-DB tests
func multiDBTables() []core.Table {
	return []core.Table{
		// PostgreSQL tables (schema: public)
		{Name: "users", Schema: "public", Database: "postgres"},
		{Name: "categories", Schema: "public", Database: "postgres"},
		{Name: "products", Schema: "public", Database: "postgres"},

		// SQLite tables (schema: main)
		{Name: "audit_logs", Schema: "main", Database: "sqlite"},
		{Name: "local_cache", Schema: "main", Database: "sqlite"},

		// MongoDB collections (no schema)
		{
			Name:     "events",
			Database: "mongodb",
			Columns: []core.Column{
				{Name: "user_id", ForeignKey: "users.id"},
			},
		},
	}
}

// newMultiDBGraphJin creates a GraphJin instance configured for multi-database
func newMultiDBGraphJin(conf *core.Config) (*core.GraphJin, error) {
	if !multiDBMode {
		return core.NewGraphJin(newConfig(conf), db)
	}

	mdbConf := newMultiDBConfig(conf)

	// Use the OptionSetDatabases to provide all connections
	return core.NewGraphJin(mdbConf, multiDBs["postgres"],
		core.OptionSetDatabases(multiDBs))
}
