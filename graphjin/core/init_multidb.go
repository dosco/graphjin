package core

import (
	"database/sql"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// initMultiDB initializes multi-database support if configured.
// It creates a dbContext for each configured database.
func (gj *graphjinEngine) initMultiDB() error {
	// Check if multi-DB is configured
	if gj.conf.Databases == nil || len(gj.conf.Databases) == 0 {
		return nil
	}

	// Don't reinitialize if OptionSetDatabases already set up databases
	if gj.databases == nil {
		gj.databases = make(map[string]*dbContext)
	}

	// Find the default database
	for name, dbConf := range gj.conf.Databases {
		if dbConf.Default {
			gj.defaultDB = name
			break
		}
	}

	// If no default specified, use the first one or "default"
	if gj.defaultDB == "" {
		for name := range gj.conf.Databases {
			gj.defaultDB = name
			break
		}
	}

	// Create/update the default database context from the passed-in connection
	// This maintains backward compatibility - the main db connection becomes
	// the default database in multi-DB mode
	if gj.db != nil {
		if existingCtx, ok := gj.databases[gj.defaultDB]; ok {
			// Update existing context with schema/dbinfo from discovery
			existingCtx.dbinfo = gj.dbinfo
			existingCtx.schema = gj.schema
		} else {
			// Create new context
			defaultCtx := &dbContext{
				name:   gj.defaultDB,
				db:     gj.db,
				dbtype: gj.dbtype,
				dbinfo: gj.dbinfo,
				schema: gj.schema,
				// Compilers will be set after initialization
			}
			gj.databases[gj.defaultDB] = defaultCtx
		}
	}

	return nil
}

// initMultiDBCompilers initializes compilers for all database contexts.
// This must be called after initCompilers() to ensure the main compilers are ready.
func (gj *graphjinEngine) initMultiDBCompilers() error {
	if gj.databases == nil || len(gj.databases) == 0 {
		return nil
	}

	// Set compilers for the default database context
	if ctx, ok := gj.databases[gj.defaultDB]; ok {
		ctx.qcodeCompiler = gj.qcodeCompiler
		ctx.psqlCompiler = gj.psqlCompiler
	}

	return nil
}

// initDBContext creates a new database context for a specific database configuration.
// This is used when adding additional databases beyond the default.
func (gj *graphjinEngine) initDBContext(name string, db *sql.DB, dbConf DatabaseConfig) (*dbContext, error) {
	ctx := &dbContext{
		name:   name,
		db:     db,
		dbtype: dbConf.Type,
	}

	// Determine the database type
	if ctx.dbtype == "" {
		ctx.dbtype = "postgres"
	}

	// Discover schema for this database
	dbinfo, err := sdata.GetDBInfo(db, ctx.dbtype, gj.conf.Blocklist)
	if err != nil {
		return nil, fmt.Errorf("database %s: schema discovery failed: %w", name, err)
	}
	ctx.dbinfo = dbinfo

	// Process tables configured for this database
	if err = addTables(gj.conf, dbinfo, name); err != nil {
		return nil, fmt.Errorf("database %s: add tables failed: %w", name, err)
	}

	// Process foreign keys configured for this database
	if err = addForeignKeys(gj.conf, dbinfo, name); err != nil {
		return nil, fmt.Errorf("database %s: add foreign keys failed: %w", name, err)
	}

	// Create schema
	ctx.schema, err = sdata.NewDBSchema(dbinfo, getDBTableAliases(gj.conf))
	if err != nil {
		return nil, fmt.Errorf("database %s: schema creation failed: %w", name, err)
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
		return nil, fmt.Errorf("database %s: qcode compiler failed: %w", name, err)
	}

	// Create SQL compiler for this database's dialect
	ctx.psqlCompiler = psql.NewCompiler(psql.Config{
		Vars:            gj.conf.Vars,
		DBType:          ctx.schema.DBType(),
		DBVersion:       ctx.schema.DBVersion(),
		SecPrefix:       gj.printFormat,
		EnableCamelcase: gj.conf.EnableCamelcase,
	})

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

	// If this is marked as default and we don't have one, set it
	if dbConf.Default && gj.defaultDB == "" {
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

// setTableDatabases assigns database names to tables based on configuration.
// This is called during schema initialization to tag tables with their source database.
func (gj *graphjinEngine) setTableDatabases() {
	if gj.conf.Databases == nil || len(gj.conf.Databases) == 0 {
		return
	}

	// Build a map of table name to database
	tableToDb := make(map[string]string)

	// First, process tables listed in DatabaseConfig.Tables
	for dbName, dbConf := range gj.conf.Databases {
		for _, tableName := range dbConf.Tables {
			tableToDb[tableName] = dbName
		}
	}

	// Then, process tables with explicit Database field in config
	for _, t := range gj.conf.Tables {
		if t.Database != "" {
			tableToDb[t.Name] = t.Database
		}
	}

	// Apply to dbinfo tables
	if gj.dbinfo != nil {
		for i := range gj.dbinfo.Tables {
			if dbName, ok := tableToDb[gj.dbinfo.Tables[i].Name]; ok {
				gj.dbinfo.Tables[i].Database = dbName
			} else {
				// Assign default database
				gj.dbinfo.Tables[i].Database = gj.defaultDB
			}
		}
	}
}

// OptionSetDatabases sets multiple database connections for multi-database mode.
// The connections map should use the same keys as Config.Databases.
func OptionSetDatabases(connections map[string]*sql.DB) Option {
	return func(gj *graphjinEngine) error {
		if gj.databases == nil {
			gj.databases = make(map[string]*dbContext)
		}

		// Determine the default database from config (since gj.defaultDB may not be set yet)
		defaultDB := ""
		for name, dbConf := range gj.conf.Databases {
			if dbConf.Default {
				defaultDB = name
				break
			}
		}
		// If no default specified, use the first one
		if defaultDB == "" {
			for name := range gj.conf.Databases {
				defaultDB = name
				break
			}
		}

		for name, db := range connections {
			dbConf, ok := gj.conf.Databases[name]
			if !ok {
				return fmt.Errorf("database %s not found in config", name)
			}

			// For the default database, just store the connection
			// The default database is initialized through the normal path
			if name == defaultDB {
				gj.databases[name] = &dbContext{
					name:   name,
					db:     db,
					dbtype: dbConf.Type,
				}
				continue
			}

			// For non-default databases, fully initialize with schema discovery
			ctx, err := gj.initDBContext(name, db, dbConf)
			if err != nil {
				return fmt.Errorf("failed to initialize database %s: %w", name, err)
			}
			gj.databases[name] = ctx
		}

		return nil
	}
}
