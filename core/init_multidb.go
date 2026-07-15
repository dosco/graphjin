package core

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/introspection"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"golang.org/x/sync/errgroup"
)

// discoverAllDatabases runs Phase 1: schema discovery for all databases.
// This populates ctx.dbinfo for each database context.
func (gj *graphjinEngine) discoverAllDatabases() error {
	// Discover sources concurrently — each writes only its own ctx.dbinfo, so
	// there is no shared state to race on. Bounded so a config with many sources
	// doesn't open a large number of connections at once.
	var g errgroup.Group
	g.SetLimit(8)
	for _, ctx := range gj.databases {
		ctx := ctx
		g.Go(func() error {
			return gj.discoverDatabase(ctx)
		})
	}
	return g.Wait()
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

	if gj.runtimeSchemaCacheFirst {
		cached, err := gj.loadRuntimeSchemaSnapshot(ctx)
		if err == nil {
			ctx.dbinfo = cached
			return nil
		}
		if legacy, legacyErr := gj.loadRuntimeSchemaDDL(ctx); legacyErr == nil {
			ctx.dbinfo = legacy
			return nil
		}
		if gj.runtimeSchemaCacheRequired {
			return fmt.Errorf("database %s: required runtime schema cache is unavailable: %w", ctx.name, err)
		}
	}

	isPrimary := (ctx.name == gj.defaultDB)

	// For the primary DB: load schema from GraphJin DDL when in MockDB mode
	// or when EnableSchema is on in production.
	if isPrimary && ((gj.prod && gj.conf.EnableSchema) || gj.conf.MockDB) {
		b, schemaPath, err := gj.loadSchemaDDL()
		if err != nil {
			if gj.conf.MockDB {
				return fmt.Errorf("mock_db is enabled but %s not found: %w", SchemaDDLFile, err)
			}
			// EnableSchema in prod but file not found — fall through to live discovery
		} else {
			if schemaPath == LegacySchemaGraphQLFile && gj.log != nil {
				gj.log.Printf("%s is deprecated; rename it to %s", LegacySchemaGraphQLFile, SchemaDDLFile)
			}
			ds, err := qcode.ParseSchema(b)
			if err != nil {
				return fmt.Errorf("failed to parse %s: %w", schemaPath, err)
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
		return fmt.Errorf("mock_db is enabled but %s not found", SchemaDDLFile)
	}

	// No DB connection — nothing to discover
	if ctx.db == nil {
		return nil
	}

	dbinfo, err := introspection.GetDBInfo(context.Background(), ctx.db, ctx.dbtype, gj.conf.Blocklist)
	if err != nil {
		cached, cacheErr := gj.loadRuntimeSchemaSnapshot(ctx)
		if cacheErr != nil {
			cached, cacheErr = gj.loadRuntimeSchemaDDL(ctx)
		}
		if cacheErr == nil {
			if gj.log != nil {
				gj.log.Printf("database %s: live schema discovery failed, using cached %s: %v",
					ctx.name, gj.runtimeSchemaDDLPath(ctx.name), err)
			}
			ctx.dbinfo = cached
			return nil
		}
		return fmt.Errorf("database %s: schema discovery failed: %w", ctx.name, err)
	}
	ctx.dbinfo = dbinfo
	gj.writeRuntimeSchemaSnapshot(ctx) //nolint:errcheck // Cache write is best-effort.
	gj.writeRuntimeSchemaDDL(ctx)      //nolint:errcheck // Cache write is best-effort.

	// In dev mode with EnableSchema, write the schema out for future use
	if isPrimary && !gj.prod && gj.conf.EnableSchema {
		var buf bytes.Buffer
		if err := writeSchema(ctx.dbinfo, &buf); err != nil {
			return err
		}
		if err := gj.fs.Put(SchemaDDLFile, buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

func (gj *graphjinEngine) loadRuntimeSchemaSnapshot(ctx *dbContext) (*sdata.DBInfo, error) {
	if gj == nil || gj.fs == nil || ctx == nil {
		return nil, fmt.Errorf("schema snapshot cache is not available")
	}
	name := gj.runtimeSchemaSnapshotPath(ctx.name)
	ok, err := gj.fs.Exists(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s not found", name)
	}
	b, err := gj.fs.Get(name)
	if err != nil {
		return nil, err
	}
	return sdata.UnmarshalDBInfoSnapshot(b)
}

func (gj *graphjinEngine) writeRuntimeSchemaSnapshot(ctx *dbContext) error {
	if gj == nil || gj.fs == nil || ctx == nil || ctx.dbinfo == nil {
		return nil
	}
	b, err := sdata.MarshalDBInfoSnapshot(ctx.dbinfo)
	if err != nil {
		return err
	}
	return gj.fs.Put(gj.runtimeSchemaSnapshotPath(ctx.name), b)
}

func (gj *graphjinEngine) runtimeSchemaSnapshotPath(source string) string {
	dir := strings.TrimSpace(gj.runtimeSchemaDDLDir)
	if dir == "" {
		return RuntimeSchemaSnapshotPath(source)
	}
	return path.Join(dir, path.Base(RuntimeSchemaSnapshotPath(source)))
}

func (gj *graphjinEngine) loadRuntimeSchemaDDL(ctx *dbContext) (*sdata.DBInfo, error) {
	if gj == nil || gj.fs == nil || ctx == nil {
		return nil, fmt.Errorf("schema DDL cache is not available")
	}
	name := gj.runtimeSchemaDDLPath(ctx.name)
	ok, err := gj.fs.Exists(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s not found", name)
	}
	b, err := gj.fs.Get(name)
	if err != nil {
		return nil, err
	}
	ds, err := qcode.ParseSchema(b)
	if err != nil {
		return nil, err
	}
	dbType := strings.TrimSpace(ctx.dbtype)
	if dbType == "" {
		dbType = strings.TrimSpace(ds.Type)
	}
	if dbType == "" {
		dbType = "postgres"
	}
	schema := ds.Schema
	if schema == "" {
		schema = defaultSchemaForDBType(dbType)
	}
	return sdata.NewDBInfo(dbType, ds.Version, schema, "", ds.Columns, ds.Functions, gj.conf.Blocklist), nil
}

func (gj *graphjinEngine) writeRuntimeSchemaDDL(ctx *dbContext) error {
	if gj == nil || gj.fs == nil || ctx == nil || ctx.dbinfo == nil {
		return nil
	}
	var buf bytes.Buffer
	if err := writeSchema(ctx.dbinfo, &buf); err != nil {
		return err
	}
	return gj.fs.Put(gj.runtimeSchemaDDLPath(ctx.name), buf.Bytes())
}

func (gj *graphjinEngine) runtimeSchemaDDLPath(source string) string {
	if gj == nil || strings.TrimSpace(gj.runtimeSchemaDDLDir) == "" {
		return RuntimeSchemaDDLPath(source)
	}
	return path.Join(gj.runtimeSchemaDDLDir, sanitizeSchemaDDLName(source)+".ddl")
}

func (gj *graphjinEngine) loadSchemaDDL() ([]byte, string, error) {
	if gj.fs == nil {
		return nil, "", fmt.Errorf("filesystem is not configured")
	}
	for _, name := range []string{SchemaDDLFile, LegacySchemaGraphQLFile} {
		ok, err := gj.fs.Exists(name)
		if err != nil {
			return nil, name, err
		}
		if !ok {
			continue
		}
		b, err := gj.fs.Get(name)
		if err != nil {
			return nil, name, err
		}
		return b, name, nil
	}
	return nil, SchemaDDLFile, fmt.Errorf("%s or legacy %s not found", SchemaDDLFile, LegacySchemaGraphQLFile)
}

// finalizeAllDatabases runs Phase 3: schema + compiler creation for all databases.
// This must be called after initResolvers() which may add remote tables to the
// primary database's dbinfo.
func (gj *graphjinEngine) finalizeAllDatabases() error {
	for _, name := range gj.sortedDatabaseNames() {
		ctx := gj.databases[name]
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

	// Ensure conf.Tables has entries for all discovered tables in this database.
	// Without this, groupRootsByDatabase cannot route queries/mutations to
	// non-default databases because it only checks conf.Tables.
	// This runs on both init and Reload(), so dynamic config changes are covered.
	gj.ensureDiscoveredTablesInConfig(ctx)

	// Process tables configured for this database
	if err := addTables(gj.conf, ctx.dbinfo, ctx.name); err != nil {
		return fmt.Errorf("database %s: add tables failed: %w", ctx.name, err)
	}

	// Process foreign keys configured for this database
	if err := addForeignKeys(gj.conf, ctx.dbinfo, ctx.name, gj.collectDBInfos()); err != nil {
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

	if err := gj.applySourceAccessRules(ctx.dbinfo, ctx.name); err != nil {
		return fmt.Errorf("database %s: source access failed: %w", ctx.name, err)
	}

	if dbConf, ok := gj.conf.Databases[ctx.name]; ok && dbConf.ManagedType == "codesql" {
		addCodeSQLVirtualColumns(ctx.dbinfo)
		hideRawCodeSQLTables(ctx.dbinfo)
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
		AnalyticsMode:       gj.conf.EffectiveAnalyticsMode(ctx.name),
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
	if err := addRoles(gj.conf, ctx.qcodeCompiler, ctx.name); err != nil {
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

func addCodeSQLVirtualColumns(di *sdata.DBInfo) {
	for _, table := range []string{"code_symbols", "code_nodes", "code_captures", "gj_code"} {
		for _, col := range []string{"code", "code_context"} {
			_ = di.AddColumn(di.Schema, table, sdata.DBColumn{
				Name:           col,
				Type:           "text",
				CodeSQLVirtual: col,
			})
		}
	}
	for _, colName := range []string{"file_id", "symbol_id", "parent_id", "target_symbol_id"} {
		col, err := di.GetColumn(di.Schema, "gj_code", colName)
		if err != nil {
			continue
		}
		col.FKeySchema = di.Schema
		col.FKeyTable = "gj_code"
		col.FKeyCol = "id"
		col.FKeyIsUnique = true
	}
}

func hideRawCodeSQLTables(di *sdata.DBInfo) {
	if di == nil {
		return
	}
	for i := range di.Tables {
		name := strings.ToLower(di.Tables[i].Name)
		if name == "gj_code" {
			continue
		}
		if strings.HasPrefix(name, "code_") {
			di.Tables[i].Blocked = true
			for j := range di.Tables[i].Columns {
				di.Tables[i].Columns[j].Blocked = true
			}
		}
	}
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

// ensureDiscoveredTablesInConfig adds minimal conf.Tables entries for tables
// discovered in a database's schema that don't already have config entries.
// This ensures groupRootsByDatabase can route queries/mutations to the correct
// database even when the user hasn't explicitly configured every table.
func (gj *graphjinEngine) ensureDiscoveredTablesInConfig(ctx *dbContext) {
	if ctx.dbinfo == nil {
		return
	}

	for _, dt := range ctx.dbinfo.Tables {
		// Skip internal/virtual tables
		if dt.Name == "" {
			continue
		}

		// Check if a config entry already exists for this table name
		found := false
		for _, t := range gj.conf.Tables {
			if strings.EqualFold(t.Name, dt.Name) {
				found = true
				break
			}
		}
		if found {
			continue
		}

		// Add a minimal config entry so groupRootsByDatabase can find it
		gj.conf.Tables = append(gj.conf.Tables, Table{
			Name:     dt.Name,
			Schema:   dt.Schema,
			Database: ctx.name,
			Source:   ctx.name,
		})
	}
}

// collectDBInfos returns a map of database name -> DBInfo for all initialized databases.
// Used to resolve cross-database foreign key references during config processing.
func (gj *graphjinEngine) collectDBInfos() map[string]*sdata.DBInfo {
	m := make(map[string]*sdata.DBInfo, len(gj.databases))
	for name, ctx := range gj.databases {
		if ctx.dbinfo != nil {
			m[name] = ctx.dbinfo
		}
	}
	return m
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
