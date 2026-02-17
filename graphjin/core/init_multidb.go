package core

import (
	"bytes"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// discoverAllDatabases runs Phase 1: schema discovery for all databases.
// This populates ctx.dbinfo for each database context.
func (gj *graphjinEngine) discoverAllDatabases() error {
	for _, ctx := range gj.databases {
		if err := gj.discoverDatabase(ctx); err != nil {
			return err
		}
	}
	return nil
}

// discoverDatabase discovers raw schema metadata for a single database.
func (gj *graphjinEngine) discoverDatabase(ctx *dbContext) error {
	// Validate dbtype
	if ctx.dbtype == "" {
		ctx.dbtype = "postgres"
	}

	// If dbinfo already provided (e.g., from watcher or tests), skip discovery
	if ctx.dbinfo != nil {
		return nil
	}

	isPrimary := (ctx.name == gj.defaultDB)

	// For the primary DB: load schema from db.graphql when in MockDB mode
	// or when EnableSchema is on in production.
	if isPrimary && ((gj.prod && gj.conf.EnableSchema) || gj.conf.MockDB) {
		b, err := gj.fs.Get("db.graphql")
		if err != nil {
			if gj.conf.MockDB {
				return fmt.Errorf("mock_db is enabled but db.graphql not found: %w", err)
			}
			// EnableSchema in prod but file not found — fall through to live discovery
		} else {
			ds, err := qcode.ParseSchema(b)
			if err != nil {
				return err
			}
			ctx.dbinfo = sdata.NewDBInfo(ds.Type, ds.Version, ds.Schema, "",
				ds.Columns, ds.Functions, gj.conf.Blocklist)
		}
	}

	// dbinfo could already be set from the above block
	if ctx.dbinfo != nil {
		return nil
	}

	if gj.conf.MockDB {
		return fmt.Errorf("mock_db is enabled but db.graphql not found")
	}

	// No DB connection — nothing to discover
	if ctx.db == nil {
		return nil
	}

	dbinfo, err := sdata.GetDBInfo(ctx.db, ctx.dbtype, gj.conf.Blocklist)
	if err != nil {
		return fmt.Errorf("database %s: schema discovery failed: %w", ctx.name, err)
	}
	ctx.dbinfo = dbinfo

	// In dev mode with EnableSchema, write the schema out for future use
	if isPrimary && !gj.prod && gj.conf.EnableSchema {
		var buf bytes.Buffer
		if err := writeSchema(ctx.dbinfo, &buf); err != nil {
			return err
		}
		if err := gj.fs.Put("db.graphql", buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

// finalizeAllDatabases runs Phase 3: schema + compiler creation for all databases.
// This must be called after initResolvers() which may add remote tables to the
// primary database's dbinfo.
func (gj *graphjinEngine) finalizeAllDatabases() error {
	for _, ctx := range gj.databases {
		if err := gj.finalizeDatabaseSchema(ctx); err != nil {
			return err
		}
	}
	return nil
}

// finalizeDatabaseSchema creates schema and compilers for a single database.
func (gj *graphjinEngine) finalizeDatabaseSchema(ctx *dbContext) error {
	if ctx.dbinfo == nil {
		return nil
	}

	// Graceful degradation: if no tables were discovered, log a warning
	// and return nil — the watcher will re-check periodically.
	if len(ctx.dbinfo.Tables) == 0 {
		ps := gj.conf.DBSchemaPollDuration
		if ps < 5*time.Second {
			ps = 10 * time.Second
		}
		gj.log.Printf("warning: no tables found in database '%s', rechecking every %s",
			ctx.dbinfo.Name, ps)
		return nil
	}

	// Process table config info (order-by config, etc.) for tables belonging to this database.
	// Also fill in empty Schema fields and handle Oracle lowercase.
	{
		schema := ctx.dbinfo.Schema
		for i, t := range gj.conf.Tables {
			// Only process tables that belong to this database
			if t.Database != "" && t.Database != ctx.name {
				continue
			}
			// Oracle requires lowercase identifiers
			if ctx.dbtype == "oracle" {
				gj.conf.Tables[i].Schema = strings.ToLower(gj.conf.Tables[i].Schema)
				gj.conf.Tables[i].Name = strings.ToLower(gj.conf.Tables[i].Name)
				gj.conf.Tables[i].Table = strings.ToLower(gj.conf.Tables[i].Table)
				t = gj.conf.Tables[i]
			}
			// Fill in empty Schema from dbinfo.Schema
			if t.Schema == "" {
				gj.conf.Tables[i].Schema = schema
				t.Schema = schema
			}
			// Skip aliases
			if t.Table != "" && t.Type == "" {
				continue
			}
			if err := gj.addTableInfo(t); err != nil {
				return err
			}
		}
	}

	// Tag all discovered tables with the owning database name
	for i := range ctx.dbinfo.Tables {
		ctx.dbinfo.Tables[i].Database = ctx.name
	}

	// Process tables configured for this database
	if err := addTables(gj.conf, ctx.dbinfo, ctx.name); err != nil {
		return fmt.Errorf("database %s: add tables failed: %w", ctx.name, err)
	}

	// Process foreign keys configured for this database
	if err := addForeignKeys(gj.conf, ctx.dbinfo, ctx.name); err != nil {
		return fmt.Errorf("database %s: add foreign keys failed: %w", ctx.name, err)
	}

	// Process full-text search configuration for this database
	if err := addFullTextColumns(gj.conf, ctx.dbinfo, ctx.name); err != nil {
		return fmt.Errorf("database %s: add fulltext columns failed: %w", ctx.name, err)
	}

	// Process functions for all databases
	if err := addFunctions(gj.conf, ctx.dbinfo); err != nil {
		return fmt.Errorf("database %s: add functions failed: %w", ctx.name, err)
	}

	// Create schema
	var err error
	ctx.schema, err = sdata.NewDBSchema(ctx.dbinfo, getDBTableAliases(gj.conf))
	if err != nil {
		return fmt.Errorf("database %s: schema creation failed: %w", ctx.name, err)
	}

	// Create QCode compiler for this database
	qcc := qcode.Config{
		TConfig:             gj.tmap,
		DefaultBlock:        gj.conf.DefaultBlock,
		DefaultLimit:        gj.conf.DefaultLimit,
		DisableAgg:          gj.conf.DisableAgg,
		DisableFuncs:        gj.conf.DisableFuncs,
		EnableCamelcase:     gj.conf.EnableCamelcase,
		DBSchema:            ctx.schema.DBSchema(),
		EnableCacheTracking: gj.conf.CacheTrackingEnabled,
	}

	ctx.qcodeCompiler, err = qcode.NewCompiler(ctx.schema, qcc)
	if err != nil {
		return fmt.Errorf("database %s: qcode compiler failed: %w", ctx.name, err)
	}

	// Add roles to the compiler
	if err := addRoles(gj.conf, ctx.qcodeCompiler); err != nil {
		return fmt.Errorf("database %s: add roles failed: %w", ctx.name, err)
	}

	// Create SQL compiler for this database's dialect
	ctx.psqlCompiler = psql.NewCompiler(psql.Config{
		Vars:            gj.conf.Vars,
		DBType:          ctx.schema.DBType(),
		DBVersion:       ctx.schema.DBVersion(),
		SecPrefix:       gj.printFormat,
		EnableCamelcase: gj.conf.EnableCamelcase,
	})
	ctx.psqlCompiler.SetSchemaInfo(ctx.schema.GetTables())

	return nil
}

// initDBContext creates a fully initialized database context for runtime additions.
// This is used by AddDatabase after GraphJin is already running.
func (gj *graphjinEngine) initDBContext(name string, db *sql.DB, dbConf DatabaseConfig) (*dbContext, error) {
	ctx := &dbContext{
		name:   name,
		db:     db,
		dbtype: dbConf.Type,
	}

	if err := gj.discoverDatabase(ctx); err != nil {
		return nil, err
	}
	if err := gj.finalizeDatabaseSchema(ctx); err != nil {
		return nil, err
	}

	return ctx, nil
}

// AddDatabase adds a new database to the multi-database configuration at runtime.
// This can be used to add databases after GraphJin is initialized.
func (gj *graphjinEngine) AddDatabase(name string, db *sql.DB, dbConf DatabaseConfig) error {
	if gj.databases == nil {
		gj.databases = make(map[string]*dbContext)
	}

	if _, exists := gj.databases[name]; exists {
		return fmt.Errorf("database %s already exists", name)
	}

	ctx, err := gj.initDBContext(name, db, dbConf)
	if err != nil {
		return err
	}

	gj.databases[name] = ctx

	// If we don't have a default yet, set it
	if gj.defaultDB == "" {
		gj.defaultDB = name
	}

	return nil
}

// RemoveDatabase removes a database from the multi-database configuration.
// Note: This does not close the database connection.
func (gj *graphjinEngine) RemoveDatabase(name string) error {
	if gj.databases == nil {
		return fmt.Errorf("no databases configured")
	}

	if _, exists := gj.databases[name]; !exists {
		return fmt.Errorf("database %s not found", name)
	}

	if name == gj.defaultDB {
		return fmt.Errorf("cannot remove default database %s", name)
	}

	delete(gj.databases, name)
	return nil
}

// GetDatabase returns the database context for the specified name.
// If name is empty, returns the default database context.
func (gj *graphjinEngine) GetDatabase(name string) (*dbContext, bool) {
	if gj.databases == nil {
		return nil, false
	}

	if name == "" {
		name = gj.defaultDB
	}

	ctx, ok := gj.databases[name]
	return ctx, ok
}

// ListDatabases returns a list of all configured database names.
func (gj *graphjinEngine) ListDatabases() []string {
	if gj.databases == nil {
		return nil
	}

	names := make([]string, 0, len(gj.databases))
	for name := range gj.databases {
		names = append(names, name)
	}
	return names
}

// sortedDatabaseNames returns database names in deterministic order:
// the default database first, then the rest in alphabetical order.
func (gj *graphjinEngine) sortedDatabaseNames() []string {
	if len(gj.databases) == 0 {
		return nil
	}
	names := make([]string, 0, len(gj.databases))
	defaultFound := false
	for name := range gj.databases {
		if name == gj.defaultDB {
			defaultFound = true
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if defaultFound {
		return append([]string{gj.defaultDB}, names...)
	}
	return names
}

// OptionSetDatabases sets multiple database connections for multi-database mode.
// The connections map should use the same keys as Config.Databases.
// Only stores bare dbContexts — full initialization happens in discoverAllDatabases
// and finalizeAllDatabases.
func OptionSetDatabases(connections map[string]*sql.DB) Option {
	return func(gj *graphjinEngine) error {
		if gj.databases == nil {
			gj.databases = make(map[string]*dbContext)
		}

		for name, db := range connections {
			dbConf, ok := gj.conf.Databases[name]
			if !ok {
				return fmt.Errorf("database %s not found in config", name)
			}

			// Store bare context — full init happens later
			gj.databases[name] = &dbContext{
				name:   name,
				db:     db,
				dbtype: dbConf.Type,
			}
		}

		return nil
	}
}
