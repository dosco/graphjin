package core

import (
	"sort"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// NewTestGraphJin creates a GraphJin instance backed by named test DB schemas,
// suitable for unit testing API and handler code without a real database.
// Each database name maps to a separate in-memory schema.
// Use "default" for the standard test schema, or "with_database" for the
// variant that includes a database field on tables (e.g. for the "events" table).
func NewTestGraphJin(databaseSchemas map[string]string) (*GraphJin, error) {
	engine := &graphjinEngine{
		databases: make(map[string]*dbContext, len(databaseSchemas)),
	}

	names := make([]string, 0, len(databaseSchemas))
	for name := range databaseSchemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		variant := databaseSchemas[name]
		var schema *sdata.DBSchema
		var err error

		switch variant {
		case "with_database":
			schema, err = sdata.NewDBSchema(sdata.GetTestDBInfoWithDatabase(), nil)
		default:
			schema, err = sdata.GetTestSchema()
		}
		if err != nil {
			return nil, err
		}

		engine.databases[name] = &dbContext{
			name:   name,
			schema: schema,
		}
		if engine.defaultDB == "" {
			engine.defaultDB = name
		}
	}

	g := &GraphJin{}
	g.Store(engine)
	return g, nil
}
