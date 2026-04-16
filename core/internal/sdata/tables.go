package sdata

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/util"
	"golang.org/x/sync/errgroup"
)

// introspectionQueryTimeout bounds each individual schema-discovery SQL
// query. Without this, a hung network read from the driver (seen with
// go-ora against Oracle) could block a test run indefinitely.
const introspectionQueryTimeout = 30 * time.Second

// DBInfo holds the database schema information
type DBInfo struct {
	Type    string
	Version int
	Schema  string
	Name    string

	Tables       []DBTable
	Functions    []DBFunction
	VTables      []VirtualTable    `json:"-"`
	CompositeFKs []CompositeFKInfo `json:"-"`
	colMap       map[string]int
	tableMap     map[string]int
	hash         int
}

// DBTable holds the database table information
type DBTable struct {
	Comment    string
	Schema     string
	Name       string
	OrigName   string // Original name before normalization (e.g., PascalCase for MSSQL)
	OrigSchema string // Original schema before normalization
	Type       string
	// Database is the name of the database this table belongs to (for multi-database support).
	// Empty string means the default database.
	Database           string
	Columns            []DBColumn
	PrimaryCols        []DBColumn
	PrimaryCol         DBColumn // backward compat: alias for PrimaryCols[0]
	SecondaryCol       DBColumn
	FullText           []DBColumn
	Blocked            bool
	Func               DBFunction
	ClusteringKeys     []string // Snowflake clustering key columns (normalized to snake_case)
	PartitionKey       string   // Partition column name (from config, e.g., "created_at")
	PartitionRangeDays int      // Default range in days for auto-injected partition filter (0 = warn only)
	colMap             map[string]int
}

// VirtualTable holds the virtual table information
type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
}

// GetDBInfo returns the database schema information.
//
// The context bounds the full discovery run (all queries + retries). Callers
// that don't have a context can use context.Background(); an internal
// per-query timeout (introspectionQueryTimeout) still applies on top so a
// hung driver read can't block forever.
func GetDBInfo(
	ctx context.Context,
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	retryDelays := []time.Duration{
		0,
		50 * time.Millisecond,
		100 * time.Millisecond,
		250 * time.Millisecond,
		500 * time.Millisecond,
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var lastErr error
	for i, delay := range retryDelays {
		if i > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		di, err := getDBInfoOnce(ctx, db, dbType, blockList)
		if err == nil {
			return di, nil
		}
		if !isRetryableDiscoveryError(err) {
			return nil, err
		}
		lastErr = err
	}

	return nil, lastErr
}

func getDBInfoOnce(
	ctx context.Context,
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	var dbVersion int
	var dbSchema, dbName string
	var cols []DBColumn
	var funcs []DBFunction
	var compositeFKs []CompositeFKInfo

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		qctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
		defer cancel()

		var row *sql.Row

		switch dbType {
		case "postgres", "":
			row = db.QueryRowContext(qctx, postgresInfo)
		case "mysql":
			row = db.QueryRowContext(qctx, mysqlInfo)
		case "mariadb":
			row = db.QueryRowContext(qctx, mariadbInfo)
		case "sqlite":
			row = db.QueryRowContext(qctx, sqliteInfo)
		case "oracle":
			row = db.QueryRowContext(qctx, oracleInfo)
		case "mssql":
			row = db.QueryRowContext(qctx, mssqlInfo)
		case "snowflake":
			row = db.QueryRowContext(qctx, snowflakeInfo)
		case "mongodb":
			// MongoDB returns info via the driver's introspection
			row = db.QueryRowContext(qctx, mongodbInfo)
		default:
			return fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, mongodb", dbType)
		}

		if err := row.Scan(&dbVersion, &dbSchema, &dbName); err != nil {
			return err
		}
		if dbType == "oracle" {
			dbSchema = strings.ToLower(dbSchema)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		if cols, err = DiscoverColumns(gctx, db, dbType, blockList); err != nil {
			return err
		}

		if funcs, err = DiscoverFunctions(gctx, db, dbType, blockList); err != nil {
			return err
		}

		// Short-circuit: if column-level FK data shows no table with
		// 2+ columns pointing at the same foreign table, there cannot
		// be any composite FKs and we can skip the expensive dialect-
		// specific introspection query. This is the biggest single
		// cost removal for the common case where test fixtures and
		// small schemas have at most one composite FK.
		if !hasCompositeFKCandidates(cols) {
			compositeFKs = nil
			return nil
		}

		if compositeFKs, err = DiscoverCompositeFKs(gctx, db, dbType); err != nil {
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// When the database returns an empty default schema (e.g., PostgreSQL with
	// search_path=''), infer it from discovered columns by picking the schema
	// with the most tables. This ensures Find() lookups and cross-schema joins
	// work correctly even with non-standard search_path configurations.
	if dbSchema == "" && len(cols) > 0 {
		dbSchema = inferDefaultSchema(cols)
	}

	di := NewDBInfo(
		dbType,
		dbVersion,
		dbSchema,
		dbName,
		cols,
		funcs,
		blockList)
	di.CompositeFKs = compositeFKs

	// For Snowflake, discover clustering keys and attach to tables.
	// Non-fatal: if this fails we just skip clustering optimization.
	if dbType == "snowflake" {
		if ck, err := discoverClusteringKeys(ctx, db); err == nil {
			for i := range di.Tables {
				key := di.Tables[i].Schema + ":" + di.Tables[i].Name
				if keys, ok := ck[key]; ok {
					di.Tables[i].ClusteringKeys = keys

					// Auto-set partition key from leading clustering column if
					// it's a temporal type and no explicit partition config exists.
					// This enables automatic "missing partition filter" warnings.
					if di.Tables[i].PartitionKey == "" {
						autoSetPartitionFromClustering(&di.Tables[i])
					}
				}
			}
		}
	}

	return di, nil
}

// hasCompositeFKCandidates returns true if any table in the column set has
// two or more columns referencing the same foreign (schema, table). This is
// a necessary condition for a composite foreign key to exist, derived
// entirely from data already collected by DiscoverColumns. If it returns
// false, the expensive DiscoverCompositeFKs query can be skipped.
func hasCompositeFKCandidates(cols []DBColumn) bool {
	counts := make(map[string]int)
	for i := range cols {
		c := &cols[i]
		if c.FKeyTable == "" {
			continue
		}
		k := c.Schema + ":" + c.Table + ":" + c.FKeySchema + ":" + c.FKeyTable
		counts[k]++
		if counts[k] > 1 {
			return true
		}
	}
	return false
}

func isRetryableDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation / deadline errors are terminal — retrying will
	// almost always hit the same timeout and just multiply the wait.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, driver.ErrBadConn) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, " eof") ||
		strings.HasSuffix(msg, "eof") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "bad connection") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe")
}

// NewDBInfo returns a new DBInfo object
func NewDBInfo(
	dbType string,
	dbVersion int,
	dbSchema string,
	dbName string,
	cols []DBColumn,
	funcs []DBFunction,
	blockList []string,
) *DBInfo {
	di := &DBInfo{
		Type:      dbType,
		Version:   dbVersion,
		Schema:    dbSchema,
		Name:      dbName,
		Functions: funcs,
		colMap:    make(map[string]int),
		tableMap:  make(map[string]int),
	}

	type st struct {
		database string
		schema   string
		table    string
	}

	tm := make(map[st][]DBColumn)
	for i, c := range cols {
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = i

		k := st{c.Database, c.Schema, c.Table}
		tm[k] = append(tm[k], c)
	}

	for k, tcols := range tm {
		ti := NewDBTable(k.schema, k.table, "", tcols)
		ti.Database = k.database
		if strings.HasPrefix(ti.Name, "_gj_") {
			continue
		}
		ti.Blocked = isInList(ti.Name, blockList)
		di.AddTable(ti)
	}

	for _, f := range funcs {
		if f.Type != "record" || len(f.Outputs) == 0 {
			continue
		}

		var cols []DBColumn
		for _, v := range f.Outputs {
			cols = append(cols, DBColumn{
				ID:   int32(v.ID),
				Name: v.Name,
				Type: v.Type,
			})
		}
		t := NewDBTable(f.Schema, f.Name, "function", cols)
		t.Func = f
		di.AddTable(t)
	}

	h := fnv.New128()
	hv := fmt.Sprintf("%s%d%s%s", dbType, dbVersion, dbSchema, dbName)
	h.Write([]byte(hv))

	for _, c := range cols {
		h.Write([]byte(c.String()))
	}

	for _, fn := range funcs {
		h.Write([]byte(fn.String()))
	}

	di.hash = h.Size()
	return di
}

// NewDBTable returns a new DBTable object
func NewDBTable(schema, name, _type string, cols []DBColumn) DBTable {
	ti := DBTable{
		Schema:  schema,
		Name:    name,
		Type:    _type,
		Columns: cols,
		colMap:  make(map[string]int, len(cols)),
	}

	// Propagate original table/schema names from the first column (MSSQL)
	if len(cols) > 0 && cols[0].OrigTable != "" {
		ti.OrigName = cols[0].OrigTable
		ti.OrigSchema = cols[0].OrigSchema
	}

	for i, c := range cols {
		cols[i].Schema = schema
		cols[i].Table = name

		switch {
		case c.FullText:
			ti.FullText = append(ti.FullText, c)

		case c.PrimaryKey:
			ti.PrimaryCols = append(ti.PrimaryCols, c)

		}
		ti.colMap[c.Name] = i
	}
	if len(ti.PrimaryCols) > 0 {
		ti.PrimaryCol = ti.PrimaryCols[0]
	}
	return ti
}

// HasCompositePK returns true if the table has a multi-column primary key.
func (t *DBTable) HasCompositePK() bool {
	return len(t.PrimaryCols) > 1
}

// PKColNames returns the names of all primary key columns.
func (t *DBTable) PKColNames() []string {
	names := make([]string, len(t.PrimaryCols))
	for i, c := range t.PrimaryCols {
		names[i] = c.Name
	}
	return names
}

// IsPKCol returns true if the named column is part of the primary key.
func (t *DBTable) IsPKCol(name string) bool {
	for _, c := range t.PrimaryCols {
		if c.Name == name {
			return true
		}
	}
	return false
}

// GetColumnIndex returns the index of a column in the table by name, and whether it was found.
// Lookup is exact first, then falls back to case-insensitive (needed for
// Snowflake where stored names are often UPPERCASE but callers may pass
// lowercase).
func (t *DBTable) GetColumnIndex(name string) (int, bool) {
	if t.colMap == nil {
		return 0, false
	}
	if i, ok := t.colMap[name]; ok {
		return i, true
	}
	for k, i := range t.colMap {
		if strings.EqualFold(k, name) {
			return i, true
		}
	}
	return 0, false
}

// AddTable adds a table to the DBInfo object
func (di *DBInfo) AddTable(t DBTable) {
	for i, c := range t.Columns {
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = i
	}

	i := len(di.Tables)
	di.Tables = append(di.Tables, t)
	di.tableMap[(t.Schema + ":" + t.Name)] = i
}

// GetTable returns a table from the DBInfo object
func (di *DBInfo) GetColumn(schema, table, column string) (*DBColumn, error) {
	t, err := di.GetTable(schema, table)
	if err != nil {
		return nil, err
	}

	cid, ok := t.GetColumnIndex(column)
	if !ok {
		return nil, fmt.Errorf("column: '%s.%s.%s' not found", schema, table, column)
	}

	return &t.Columns[cid], nil
}

// GetTable returns a table from the DBInfo object
func (di *DBInfo) GetTable(schema, table string) (*DBTable, error) {
	tid, ok := di.tableMap[(schema + ":" + table)]
	if !ok {
		return nil, fmt.Errorf("table: '%s.%s' not found", schema, table)
	}

	return &di.Tables[tid], nil
}

// DBColumn returns the column as a string
type DBColumn struct {
	Comment      string
	ID           int32
	Name         string
	OrigName     string // Original name before normalization (e.g., PascalCase for MSSQL)
	Type         string
	Array        bool
	NotNull      bool
	PrimaryKey   bool
	UniqueKey    bool
	FullText     bool
	FKRecursive  bool
	FKeyDatabase string // Target database for cross-database FKs (empty = same db)
	FKeySchema   string
	FKeyTable    string
	FKeyCol      string
	FKeyIsUnique bool // True if FK target column is PK/unique (for correct rel type)
	Blocked      bool
	Table        string
	Schema       string
	Database     string
	Default      string
	Index        bool
	IndexName    string
	FKOnDelete   string
	FKOnUpdate   string

	// Original names before normalization (used to build dialect name maps for MSSQL)
	OrigTable      string
	OrigSchema     string
	OrigFKeyTable  string
	OrigFKeySchema string
	OrigFKeyCol    string
}

// ColPair represents a column pair in a composite foreign key relationship.
type ColPair struct {
	L DBColumn // Local column
	R DBColumn // Referenced (foreign) column
}

// CompositeFKInfo holds metadata about a composite (multi-column) foreign key constraint.
type CompositeFKInfo struct {
	Schema         string
	Table          string
	ConstraintName string
	LocalCols      []string
	FKeySchema     string
	FKeyTable      string
	FKeyCols       []string
}

// DiscoverColumns returns the columns of a table
func DiscoverColumns(ctx context.Context, db *sql.DB, dbtype string, blockList []string) ([]DBColumn, error) {
	var sqlStmt string

	switch dbtype {
	case "postgres", "":
		sqlStmt = postgresColumnsStmt
	case "mysql":
		sqlStmt = mysqlColumnsStmt
	case "mariadb":
		sqlStmt = mariadbColumnsStmt
	case "sqlite":
		sqlStmt = sqliteColumnsStmt
	case "oracle":
		sqlStmt = oracleColumnsStmt
	case "mssql":
		sqlStmt = mssqlColumnsStmt
	case "snowflake":
		sqlStmt = snowflakeColumnsStmt
	case "mongodb":
		// MongoDB uses JSON query DSL - the driver handles introspection
		sqlStmt = mongodbColumnsStmt
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, mongodb", dbtype)
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, sqlStmt)
	if err != nil {
		// Snowflake fallback: if INFORMATION_SCHEMA.COLUMNS is unreachable
		// (restricted role), fall back to SHOW COLUMNS IN DATABASE which
		// reads from the metadata service and has broader visibility.
		if dbtype == "snowflake" {
			return discoverColumnsSnowflakeSHOW(ctx, db)
		}
		return nil, fmt.Errorf("error fetching columns: %w", err)
	}
	defer rows.Close()

	cmap := make(map[string]DBColumn)

	i := 0
	// we have to rescan and update columns to overcome
	// weird bugs in mysql like joins with information_schema
	// don't work in 8.0.22 etc.
	for rows.Next() {
		var c DBColumn
		c.ID = int32(i)

		err = rows.Scan(&c.Schema,
			&c.Table,
			&c.Name,
			&c.Type,
			&c.NotNull,
			&c.PrimaryKey,
			&c.UniqueKey,
			&c.Array,
			&c.FullText,
			&c.FKeySchema,
			&c.FKeyTable,
			&c.FKeyCol)

		c.FKeySchema = strings.TrimSpace(c.FKeySchema)
		c.FKeyTable = strings.TrimSpace(c.FKeyTable)
		c.FKeyCol = strings.TrimSpace(c.FKeyCol)

		if err != nil {
			return nil, err
		}

		if dbtype == "mssql" {
			c.OrigName = c.Name
			c.OrigTable = c.Table
			c.OrigSchema = c.Schema
			c.OrigFKeyTable = c.FKeyTable
			c.OrigFKeySchema = c.FKeySchema
			c.OrigFKeyCol = c.FKeyCol
		}

		if dbtype == "sqlite" || dbtype == "oracle" || dbtype == "mssql" {
			c.Name = util.ToSnake(c.Name)
			c.Table = strings.ToLower(c.Table)
			c.Schema = strings.ToLower(c.Schema)
			c.Type = strings.ToLower(c.Type)
			c.FKeyTable = strings.ToLower(c.FKeyTable)
			c.FKeySchema = strings.ToLower(c.FKeySchema)
			c.FKeyCol = util.ToSnake(c.FKeyCol)
		}

		// Snowflake preserves identifier case (real Snowflake stores unquoted
		// identifiers as UPPERCASE; we keep what discovery returned). Only
		// the type string is lowercased so it matches the snowflakeCastType
		// switch table.
		if dbtype == "snowflake" {
			c.Type = strings.ToLower(strings.TrimSpace(c.Type))
		}

		k := (c.Schema + ":" + c.Table + ":" + c.Name)
		v, ok := cmap[k]
		if !ok {
			v = c
			v.ID = int32(len(cmap))
			if strings.HasPrefix(v.Table, "_gj_") {
				continue
			}
			v.Blocked = isInList(v.Name, blockList)
		}
		if c.Type != "" {
			v.Type = c.Type
		}
		if c.PrimaryKey {
			v.PrimaryKey = true
			v.UniqueKey = true
		}
		if c.NotNull {
			v.NotNull = true
		}
		if c.UniqueKey {
			v.UniqueKey = true
		}
		if c.Array {
			v.Array = true
		}
		if c.FullText {
			v.FullText = true
		}
		if c.FKeySchema != "" {
			v.FKeySchema = c.FKeySchema
		}
		if c.FKeyTable != "" {
			v.FKeyTable = c.FKeyTable
		}
		if c.FKeyCol != "" {
			v.FKeyCol = c.FKeyCol
		}
		if v.FKeySchema == v.Schema && v.FKeyTable == v.Table {
			v.FKRecursive = true
		}
		cmap[k] = v
		i++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning columns: %w", err)
	}

	// Snowflake: enrich the column map with PK/UK/FK info from SHOW commands.
	// INFORMATION_SCHEMA on Snowflake does not expose column-level constraint
	// info (no KEY_COLUMN_USAGE view), so the columns query above leaves
	// primary_key/unique_key/FK fields empty. The SHOW commands are the
	// Snowflake-native source. All errors are non-fatal — accounts without
	// declared constraints, or emulator environments that don't implement the
	// SHOW commands, simply skip this enrichment and fall back to the
	// _gj_fk_metadata overlay (if present) or config-declared relationships.
	if dbtype == "snowflake" {
		enrichSnowflakeFromSHOW(ctx, db, cmap)
	}

	// View PK detection: views lack PK constraints in system catalogs, so the
	// main column query reports primary_key=false for all view columns. We run
	// a supplementary query to trace view columns back to source base table PKs.
	//
	// All paths are gated behind a cheap preflight check ("does this DB have
	// user-visible views?") to avoid running expensive catalog queries when
	// no views exist — the common case for most deployments.
	if hasUserViews(ctx, db, dbtype) {
		// Layer 1: SQL-level tracing via system catalogs (best accuracy).
		// Supported: PostgreSQL, MSSQL, Oracle, MySQL 8.0+.
		// Not supported: MariaDB, SQLite, Snowflake (no system catalog for view column tracing).
		var viewPKStmt string
		var needsNormalize bool
		switch dbtype {
		case "postgres", "":
			viewPKStmt = postgresViewPKsStmt
		case "mssql":
			viewPKStmt = mssqlViewPKsStmt
			needsNormalize = true
		case "oracle":
			viewPKStmt = oracleViewPKsStmt
			needsNormalize = true
		case "mysql":
			viewPKStmt = mysqlViewPKsStmt
		}
		if viewPKStmt != "" {
			qctx2, cancel2 := context.WithTimeout(ctx, introspectionQueryTimeout)
			defer cancel2()
			rows2, err := db.QueryContext(qctx2, viewPKStmt)
			if err == nil {
				defer rows2.Close()
				for rows2.Next() {
					var schema, table, column string
					if err := rows2.Scan(&schema, &table, &column); err != nil {
						continue
					}
					if needsNormalize {
						column = util.ToSnake(column)
						table = strings.ToLower(table)
						schema = strings.ToLower(schema)
					}

					k := schema + ":" + table + ":" + column
					if v, ok := cmap[k]; ok && !v.PrimaryKey {
						v.PrimaryKey = true
						v.UniqueKey = true
						cmap[k] = v
					}
				}
			}
			// Silently ignore errors — falls back to heuristic below
		}

		// Layer 2: Code-level heuristic fallback. For databases without SQL-level
		// view PK detection (MariaDB, SQLite, Snowflake) or when the SQL query fails,
		// infer PKs by matching view columns against base table PKs in the same schema.
		inferViewPKsFromBaseTables(cmap)
	}

	var cols []DBColumn
	for _, c := range cmap {
		cols = append(cols, c)
	}

	return cols, nil
}

// hasUserViews returns true if the database has at least one user-visible view.
// This is a cheap preflight check to avoid running expensive view PK detection
// queries (which may compile every view) when no views exist. Returns false on
// error or for unsupported database types — the view-PK enrichment is non-essential
// and users can override via config.
func hasUserViews(ctx context.Context, db *sql.DB, dbtype string) bool {
	var stmt string
	switch dbtype {
	case "mssql":
		stmt = mssqlHasViewsStmt
	case "postgres", "":
		stmt = `SELECT 1 FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid WHERE c.relkind IN ('v','m') AND n.nspname NOT IN ('pg_catalog','information_schema','_graphjin') LIMIT 1`
	case "mysql":
		stmt = `SELECT 1 FROM information_schema.VIEWS WHERE TABLE_SCHEMA NOT IN ('mysql','information_schema','performance_schema','sys') LIMIT 1`
	case "mariadb":
		stmt = `SELECT 1 FROM information_schema.VIEWS WHERE TABLE_SCHEMA NOT IN ('mysql','information_schema','performance_schema','sys') LIMIT 1`
	case "oracle":
		stmt = `SELECT 1 FROM all_views WHERE owner NOT IN ('SYS','SYSTEM','OUTLN','DBSNMP','APPQOSSYS','XDB','WMSYS','CTXSYS','MDSYS','ORDSYS','ORDDATA') AND ROWNUM = 1`
	case "sqlite":
		stmt = `SELECT 1 FROM sqlite_master WHERE type='view' LIMIT 1`
	default:
		return false
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	if err := db.QueryRowContext(qctx, stmt).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// inferViewPKsFromBaseTables is a universal fallback for view PK detection.
// For any table/view that has NO PK columns, it finds the "best matching"
// base table by counting how many non-PK columns overlap. If exactly one
// base table has the highest overlap AND all its PK columns appear in the
// view, those view columns are marked as PKs.
func inferViewPKsFromBaseTables(cmap map[string]DBColumn) {
	type st struct{ schema, table string }

	// Step 1: Build per-table column sets and PK sets
	tableCols := make(map[st]map[string]bool)
	tablePKs := make(map[st][]string)
	hasPK := make(map[st]bool)

	for _, c := range cmap {
		key := st{c.Schema, c.Table}
		if tableCols[key] == nil {
			tableCols[key] = make(map[string]bool)
		}
		tableCols[key][c.Name] = true
		if c.PrimaryKey {
			hasPK[key] = true
			tablePKs[key] = append(tablePKs[key], c.Name)
		}
	}

	// Step 2: For each table with no PK, find the best matching base table
	for viewKey, viewCols := range tableCols {
		if hasPK[viewKey] {
			continue
		}
		if strings.HasPrefix(viewKey.table, "_gj_") {
			continue
		}

		type candidate struct {
			table   st
			overlap int
			pkCols  []string
		}
		var best candidate
		var tied bool

		for baseKey, basePKCols := range tablePKs {
			if baseKey.schema != viewKey.schema || baseKey == viewKey {
				continue
			}

			// Check if ALL PK columns of this base table appear in the view
			allPKsPresent := true
			for _, pk := range basePKCols {
				if !viewCols[pk] {
					allPKsPresent = false
					break
				}
			}
			if !allPKsPresent {
				continue
			}

			// Count non-PK column overlap
			overlap := 0
			baseCols := tableCols[baseKey]
			for col := range baseCols {
				if !containsStr(basePKCols, col) && viewCols[col] {
					overlap++
				}
			}

			if overlap == 0 {
				continue
			}

			if overlap > best.overlap {
				best = candidate{baseKey, overlap, basePKCols}
				tied = false
			} else if overlap == best.overlap && best.table != baseKey {
				tied = true
			}
		}

		// Only apply if we have an unambiguous winner
		if best.overlap > 0 && !tied {
			for _, pkCol := range best.pkCols {
				k := viewKey.schema + ":" + viewKey.table + ":" + pkCol
				if v, ok := cmap[k]; ok && !v.PrimaryKey {
					v.PrimaryKey = true
					v.UniqueKey = true
					cmap[k] = v
				}
			}
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// inferDefaultSchema picks the schema that contains the most distinct tables
// from the discovered columns. Used as a fallback when the database reports
// an empty default schema (e.g., PostgreSQL with search_path='').
func inferDefaultSchema(cols []DBColumn) string {
	type st struct{ schema, table string }
	seen := make(map[st]bool)
	counts := make(map[string]int)
	for _, c := range cols {
		if c.Schema == "" || strings.HasPrefix(c.Table, "_gj_") {
			continue
		}
		k := st{c.Schema, c.Table}
		if !seen[k] {
			seen[k] = true
			counts[c.Schema]++
		}
	}

	var best string
	var bestCount int
	for schema, n := range counts {
		if n > bestCount {
			best = schema
			bestCount = n
		}
	}
	return best
}

// DiscoverCompositeFKs returns metadata about composite (multi-column) foreign key
// constraints for the given database type.
//
// Composite FK discovery is a best-effort enrichment: if the query errors or
// times out, we return (nil, nil) and let the rest of the schema load normally.
// Single-column FKs (the overwhelmingly common case) come from DiscoverColumns
// and are unaffected. This prevents a slow/broken data-dictionary query from
// failing the whole NewGraphJin init.
func DiscoverCompositeFKs(ctx context.Context, db *sql.DB, dbtype string) ([]CompositeFKInfo, error) {
	var (
		result []CompositeFKInfo
		err    error
	)
	switch dbtype {
	case "postgres", "":
		result, err = discoverCompositeFKsPostgres(ctx, db)
	case "mysql":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMySQL)
	case "mariadb":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMySQL) // identical to MySQL
	case "sqlite":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQuerySQLite)
	case "oracle":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryOracle)
	case "mssql":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMSSQL)
	case "snowflake":
		result, err = discoverCompositeFKsSnowflake(ctx, db)
	default:
		return nil, nil
	}
	if err != nil {
		// Non-fatal: log nothing here (caller has no logger), just drop the
		// enrichment. Single-column FKs are unaffected.
		return nil, nil
	}
	return result, nil
}

// compositeFKQueryMySQL is scoped to the current DATABASE() to avoid scanning
// every schema on the server — MySQL's information_schema views are synthesized
// on demand and are prohibitively slow when unfiltered. The join also matches
// on table_name so constraint-name collisions across tables cannot fan out.
const compositeFKQueryMySQL = `
SELECT kcu.table_schema, kcu.table_name, kcu.constraint_name,
       GROUP_CONCAT(kcu.column_name ORDER BY kcu.ordinal_position) AS local_columns,
       kcu.referenced_table_schema, kcu.referenced_table_name,
       GROUP_CONCAT(kcu.referenced_column_name ORDER BY kcu.ordinal_position) AS fkey_columns
FROM information_schema.key_column_usage kcu
JOIN information_schema.table_constraints tc
  ON kcu.constraint_name = tc.constraint_name
 AND kcu.table_schema = tc.table_schema
 AND kcu.table_name = tc.table_name
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND kcu.table_schema = DATABASE()
GROUP BY kcu.table_schema, kcu.table_name, kcu.constraint_name,
         kcu.referenced_table_schema, kcu.referenced_table_name
HAVING COUNT(*) > 1`

const compositeFKQuerySQLite = `
SELECT 'main' AS schema_name, m.name AS table_name,
       CAST(fk.id AS TEXT) AS constraint_name,
       GROUP_CONCAT(fk."from", ',') AS local_columns,
       'main' AS fkey_schema,
       fk."table" AS fkey_table,
       GROUP_CONCAT(fk."to", ',') AS fkey_columns
FROM sqlite_master m
CROSS JOIN pragma_foreign_key_list(m.name) fk
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' AND m.name NOT LIKE '_gj_%'
GROUP BY m.name, fk.id, fk."table"
HAVING COUNT(*) > 1`

// compositeFKQueryOracle discovers composite foreign keys owned by the current
// session user.
//
// Local side  — user_constraints / user_cons_columns: owns only current-user
// constraints, no security-predicate overhead, 10-100× faster than all_*.
//
// Referenced side — all_constraints / all_cons_columns: the target table may
// live in a different schema (cross-schema FK). These are single indexed
// key-lookups on r_constraint_name, not full scans, so the cost is O(1) per
// FK constraint regardless of how many system schemas exist.
const compositeFKQueryOracle = `
SELECT USER AS owner, uc.table_name, uc.constraint_name,
       LISTAGG(ucc.column_name, ',') WITHIN GROUP (ORDER BY ucc.position) AS local_columns,
       LOWER(r_ac.owner) AS fkey_schema, r_ac.table_name AS fkey_table,
       LISTAGG(r_acc.column_name, ',') WITHIN GROUP (ORDER BY r_acc.position) AS fkey_columns
FROM user_constraints uc
JOIN user_cons_columns ucc
  ON uc.constraint_name = ucc.constraint_name
JOIN all_constraints r_ac
  ON uc.r_constraint_name = r_ac.constraint_name
JOIN all_cons_columns r_acc
  ON r_ac.constraint_name = r_acc.constraint_name
 AND r_ac.owner           = r_acc.owner
 AND ucc.position         = r_acc.position
WHERE uc.constraint_type = 'R'
GROUP BY uc.table_name, uc.constraint_name, r_ac.owner, r_ac.table_name
HAVING COUNT(*) > 1`

// compositeFKQueryMSSQL filters system and role schemas on BOTH sides of the
// FK so the result set is consistent with mssql_columns.sql. sys.tables is
// user-tables-only so sys/INFORMATION_SCHEMA are self-filtered, but CDC
// (Change Data Capture) and audit schemas expose user-visible tables that
// would otherwise leak through.
const compositeFKQueryMSSQL = `
SELECT s.name, t.name, OBJECT_NAME(fkc.constraint_object_id),
       STRING_AGG(c.name, ',') AS local_columns,
       rs.name, rt.name,
       STRING_AGG(rc.name, ',') AS fkey_columns
FROM sys.foreign_key_columns fkc
JOIN sys.columns c ON fkc.parent_object_id = c.object_id AND fkc.parent_column_id = c.column_id
JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
JOIN sys.tables t ON fkc.parent_object_id = t.object_id
JOIN sys.schemas s ON t.schema_id = s.schema_id
JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id
JOIN sys.schemas rs ON rt.schema_id = rs.schema_id
WHERE s.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest', 'cdc',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin',
    'db_backupoperator', 'db_datareader', 'db_datawriter',
    'db_denydatareader', 'db_denydatawriter'
)
AND rs.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest', 'cdc',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin',
    'db_backupoperator', 'db_datareader', 'db_datawriter',
    'db_denydatareader', 'db_denydatawriter'
)
GROUP BY s.name, t.name, fkc.constraint_object_id, rs.name, rt.name
HAVING COUNT(*) > 1`

// compositeFKQuerySnowflake reads composite FKs from the optional
// _gj_fk_metadata overlay table (opt-in declaration for accounts that don't
// want FKs in DDL, and for the DuckDB-backed emulator used in integration
// tests). Real Snowflake composite FKs declared in DDL are discovered via
// SHOW IMPORTED KEYS by discoverCompositeFKsSnowflake below — Snowflake's
// INFORMATION_SCHEMA does not expose KEY_COLUMN_USAGE, so the standard-SQL
// constraint views cannot be joined to column names.
const compositeFKQuerySnowflake = `
SELECT table_schema, table_name,
       table_schema || ':' || table_name || ':' || foreign_table_name AS constraint_name,
       LISTAGG(column_name, ',') WITHIN GROUP (ORDER BY column_name) AS local_columns,
       foreign_table_schema, foreign_table_name,
       LISTAGG(foreign_column_name, ',') WITHIN GROUP (ORDER BY foreign_column_name) AS fkey_columns
FROM _gj_fk_metadata
GROUP BY table_schema, table_name, foreign_table_schema, foreign_table_name
HAVING COUNT(*) > 1`

// discoverCompositeFKsSnowflake combines two Snowflake-native sources for
// composite (multi-column) foreign keys:
//
//  1. SHOW IMPORTED KEYS IN DATABASE — returns per-column FK rows with fk_name,
//     grouped here by constraint name to identify multi-column FKs.
//  2. _gj_fk_metadata overlay — opt-in table read via compositeFKQuerySnowflake.
//
// Both are non-fatal: SHOW fails on the DuckDB-backed emulator; the overlay
// fails when the table doesn't exist in production accounts.
func discoverCompositeFKsSnowflake(ctx context.Context, db *sql.DB) ([]CompositeFKInfo, error) {
	var all []CompositeFKInfo
	seen := make(map[string]bool)

	// 1. SHOW IMPORTED KEYS grouped by fk_name → composite FKs.
	type fkRow struct {
		keySeq  int
		fkCol   string
		pkCol   string
	}
	groups := make(map[string]*CompositeFKInfo)
	rowsByKey := make(map[string][]fkRow)

	querySnowflakeSHOWByName(ctx, db, "SHOW IMPORTED KEYS IN DATABASE",
		[]string{"fk_schema_name", "fk_table_name", "fk_column_name",
			"pk_schema_name", "pk_table_name", "pk_column_name",
			"fk_name", "key_sequence"},
		func(vals []string) {
			fkSchema, fkTable, fkCol := vals[0], vals[1], vals[2]
			pkSchema, pkTable, pkCol := vals[3], vals[4], vals[5]
			fkName := vals[6]
			seq := 0
			fmt.Sscanf(vals[7], "%d", &seq)

			key := fkSchema + ":" + fkTable + ":" + fkName
			if groups[key] == nil {
				groups[key] = &CompositeFKInfo{
					Schema:         fkSchema,
					Table:          fkTable,
					ConstraintName: fkName,
					FKeySchema:     pkSchema,
					FKeyTable:      pkTable,
				}
			}
			rowsByKey[key] = append(rowsByKey[key], fkRow{keySeq: seq, fkCol: fkCol, pkCol: pkCol})
		})

	for key, rs := range rowsByKey {
		if len(rs) < 2 {
			continue
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i].keySeq < rs[j].keySeq })
		info := groups[key]
		for _, r := range rs {
			info.LocalCols = append(info.LocalCols, r.fkCol)
			info.FKeyCols = append(info.FKeyCols, r.pkCol)
		}
		all = append(all, *info)
		seen[info.Schema+":"+info.Table+":"+info.ConstraintName] = true
	}

	// 2. _gj_fk_metadata overlay (skip entries already claimed by SHOW).
	overlay, _ := discoverCompositeFKsCSV(ctx, db, "snowflake", compositeFKQuerySnowflake)
	for _, o := range overlay {
		if !seen[o.Schema+":"+o.Table+":"+o.ConstraintName] {
			all = append(all, o)
		}
	}

	return all, nil
}

func discoverCompositeFKsPostgres(ctx context.Context, db *sql.DB) ([]CompositeFKInfo, error) {
	const query = `
SELECT
	n.nspname AS schema_name,
	c.relname AS table_name,
	co.conname AS constraint_name,
	array_agg(a.attname ORDER BY k.ord) AS local_columns,
	fn.nspname AS fkey_schema,
	fc.relname AS fkey_table,
	array_agg(fa.attname ORDER BY k.ord) AS fkey_columns
FROM pg_constraint co
	JOIN pg_class c ON c.oid = co.conrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	JOIN pg_class fc ON fc.oid = co.confrelid
	JOIN pg_namespace fn ON fn.oid = fc.relnamespace
	CROSS JOIN LATERAL unnest(co.conkey, co.confkey) WITH ORDINALITY AS k(local_attnum, foreign_attnum, ord)
	JOIN pg_attribute a ON a.attrelid = co.conrelid AND a.attnum = k.local_attnum
	JOIN pg_attribute fa ON fa.attrelid = co.confrelid AND fa.attnum = k.foreign_attnum
WHERE co.contype = 'f'
	AND n.nspname NOT IN ('_graphjin', 'information_schema', 'pg_catalog')
	AND array_length(co.conkey, 1) > 1
GROUP BY n.nspname, c.relname, co.conname, fn.nspname, fc.relname`

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, query)
	if err != nil {
		return nil, fmt.Errorf("error fetching composite FKs: %w", err)
	}
	defer rows.Close()

	var result []CompositeFKInfo
	for rows.Next() {
		var info CompositeFKInfo
		var localCols, fkeyCols []string
		if err := rows.Scan(
			&info.Schema, &info.Table, &info.ConstraintName,
			(*pgStringArray)(&localCols),
			&info.FKeySchema, &info.FKeyTable,
			(*pgStringArray)(&fkeyCols),
		); err != nil {
			return nil, fmt.Errorf("error scanning composite FK: %w", err)
		}
		info.LocalCols = localCols
		info.FKeyCols = fkeyCols
		result = append(result, info)
	}
	return result, rows.Err()
}

// discoverCompositeFKsCSV handles composite FK discovery for databases that return
// aggregated columns as comma-separated strings (MySQL, MariaDB, SQLite, Oracle, MSSQL, Snowflake).
func discoverCompositeFKsCSV(ctx context.Context, db *sql.DB, dbtype, query string) ([]CompositeFKInfo, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, query)
	if err != nil {
		return nil, fmt.Errorf("error fetching composite FKs: %w", err)
	}
	defer rows.Close()

	// Oracle/MSSQL: lowercase + snake_case columns to match column-discovery normalization.
	// Snowflake: identifiers preserved in original case (per discovery), only trim
	// whitespace from CSV-split column lists.
	normalize := dbtype == "oracle" || dbtype == "mssql"

	var result []CompositeFKInfo
	for rows.Next() {
		var info CompositeFKInfo
		var localCSV, fkeyCSV string
		if err := rows.Scan(
			&info.Schema, &info.Table, &info.ConstraintName,
			&localCSV,
			&info.FKeySchema, &info.FKeyTable,
			&fkeyCSV,
		); err != nil {
			return nil, fmt.Errorf("error scanning composite FK: %w", err)
		}
		info.LocalCols = strings.Split(localCSV, ",")
		info.FKeyCols = strings.Split(fkeyCSV, ",")

		if normalize {
			info.Schema = strings.ToLower(info.Schema)
			info.Table = strings.ToLower(info.Table)
			info.FKeySchema = strings.ToLower(info.FKeySchema)
			info.FKeyTable = strings.ToLower(info.FKeyTable)
			for i := range info.LocalCols {
				info.LocalCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.LocalCols[i])))
			}
			for i := range info.FKeyCols {
				info.FKeyCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.FKeyCols[i])))
			}
		} else if dbtype == "snowflake" {
			for i := range info.LocalCols {
				info.LocalCols[i] = strings.TrimSpace(info.LocalCols[i])
			}
			for i := range info.FKeyCols {
				info.FKeyCols[i] = strings.TrimSpace(info.FKeyCols[i])
			}
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// pgStringArray implements sql.Scanner for Postgres text[] columns.
type pgStringArray []string

func (a *pgStringArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("pgStringArray: unsupported type %T", src)
	}
	// Parse Postgres array literal: {val1,val2,...}
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return fmt.Errorf("pgStringArray: invalid format %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		*a = nil
		return nil
	}
	*a = strings.Split(inner, ",")
	return nil
}

// DBFunction holds the database function information
type DBFunction struct {
	Comment string
	Schema  string
	Name    string
	Type    string
	Agg     bool
	Inputs  []DBFuncParam
	Outputs []DBFuncParam
}

// DBFuncParam holds the database function parameter information
type DBFuncParam struct {
	ID    int
	Name  string
	Type  string
	Array bool
}

// DiscoverFunctions returns the functions of a database
func DiscoverFunctions(ctx context.Context, db *sql.DB, dbtype string, blockList []string) ([]DBFunction, error) {
	var sqlStmt string

	switch dbtype {
	case "postgres", "":
		sqlStmt = postgresFunctionsStmt
	case "mysql":
		sqlStmt = mysqlFunctionsStmt
	case "mariadb":
		sqlStmt = mariadbFunctionsStmt
	case "sqlite":
		sqlStmt = sqliteFunctionsStmt
	case "oracle":
		sqlStmt = oracleFunctionsStmt
	case "mssql":
		sqlStmt = mssqlFunctionsStmt
	case "snowflake":
		return discoverFunctionsSnowflake(ctx, db)
	case "mongodb":
		// MongoDB doesn't have user-defined functions in the SQL sense
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, mongodb", dbtype)
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching functions: %w", err)
	}
	defer rows.Close()

	var funcs []DBFunction
	fm := make(map[string]int)

	for rows.Next() {
		var fid, fs, fn, ft string
		var pn, pt, pk sql.NullString
		var pid sql.NullInt64

		err = rows.Scan(&fid, &fs, &fn, &ft, &pid, &pn, &pt, &pk)
		if err != nil {
			return nil, err
		}

		if isInList(fn, blockList) {
			continue
		}

		i, ok := fm[fid]
		if !ok {
			funcs = append(funcs, DBFunction{Schema: fs, Name: fn, Type: ft})
			i = len(funcs) - 1
			fm[fid] = i
		}

		pidVal := 0
		if pid.Valid {
			pidVal = int(pid.Int64)
		}
		param := DBFuncParam{ID: pidVal, Name: pn.String, Type: pt.String}

		if strings.HasSuffix(pt.String, "[]") {
			param.Array = true
		}

		switch pk.String {
		case "IN", "in":
			funcs[i].Inputs = append(funcs[i].Inputs, param)
		case "OUT", "out":
			funcs[i].Outputs = append(funcs[i].Outputs, param)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning functions: %w", err)
	}

	return funcs, nil
}

// GetInput returns the input of a function
func (fn *DBFunction) GetInput(name string) (ret DBFuncParam, err error) {
	for _, in := range fn.Inputs {
		if in.Name == name {
			return in, nil
		}
	}
	return ret, fmt.Errorf("function input '%s' not found", name)
}

// Hash returns the hash of the DBInfo object
func (di *DBInfo) Hash() int {
	return di.hash
}

// isInList checks if a value is in a list
func isInList(val string, s []string) bool {
	for _, v := range s {
		regex := fmt.Sprintf("^%s$", v)
		if matched, _ := regexp.MatchString(regex, val); matched {
			return true
		}
	}
	return false
}

// discoverClusteringKeys queries Snowflake's information_schema.tables for
// clustering key metadata. Returns a map of "schema:table" → []column_name.
func discoverClusteringKeys(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, snowflakeClusteringStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching clustering keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var schema, table, clusterExpr string
		if err := rows.Scan(&schema, &table, &clusterExpr); err != nil {
			return nil, fmt.Errorf("error scanning clustering key row: %w", err)
		}
		if keys := ParseClusteringKey(clusterExpr); len(keys) > 0 {
			result[schema+":"+table] = keys
		}
	}
	return result, rows.Err()
}

// autoSetPartitionFromClustering checks if the leading clustering key column
// is a temporal type (date, timestamp, etc.) and, if so, sets it as the
// table's partition key with a default 90-day range filter. This enables
// zero-config partition pruning for Snowflake tables clustered by a date
// column — queries without an explicit filter on the clustering key get a
// `WHERE created_at >= NOW() - 90 days` predicate automatically.
//
// The 60-day default is conservative enough for most workloads while still
// preventing accidental full-table scans. Users can override via config
// (setting PartitionRangeDays to a different value or 0 for warn-only).
func autoSetPartitionFromClustering(t *DBTable) {
	if len(t.ClusteringKeys) == 0 {
		return
	}
	leadingKey := t.ClusteringKeys[0]
	cid, ok := t.GetColumnIndex(leadingKey)
	if !ok {
		return
	}
	if isTemporalType(t.Columns[cid].Type) {
		t.PartitionKey = leadingKey
		t.PartitionRangeDays = 60
	}
}

// isTemporalType returns true if the column type string represents a
// date or timestamp type across common database dialects.
func isTemporalType(colType string) bool {
	t := strings.ToLower(colType)
	switch {
	case strings.Contains(t, "timestamp"):
		return true
	case strings.Contains(t, "datetime"):
		return true
	case t == "date":
		return true
	case strings.HasPrefix(t, "timestamp_"):
		// Snowflake: TIMESTAMP_LTZ, TIMESTAMP_NTZ, TIMESTAMP_TZ
		return true
	default:
		return false
	}
}

// ParseClusteringKey parses Snowflake's clustering key expression into
// a list of normalized column names. Snowflake returns expressions like:
//
//	LINEAR(CREATED_AT, USER_ID)
//	LINEAR(CREATED_AT)
//	(CREATED_AT, USER_ID)
//
// Returns nil for empty or unparseable expressions.
func ParseClusteringKey(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	// Strip optional LINEAR(...) wrapper
	upper := strings.ToUpper(expr)
	if strings.HasPrefix(upper, "LINEAR(") && strings.HasSuffix(expr, ")") {
		expr = expr[7 : len(expr)-1]
	} else if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		// Strip bare parentheses
		expr = expr[1 : len(expr)-1]
	}

	parts := strings.Split(expr, ",")
	var keys []string
	for _, p := range parts {
		col := strings.TrimSpace(p)
		if col == "" {
			continue
		}
		// Preserve identifier case (Snowflake stores unquoted as UPPERCASE).
		// Callers should match against discovered columns case-insensitively
		// (e.g. with strings.EqualFold). Expression-based clustering keys
		// (e.g. CAST(col AS DATE)) will not match any column and are gracefully
		// skipped at lookup time.
		keys = append(keys, col)
	}
	return keys
}

// discoverColumnsSnowflakeSHOW is the last-resort discovery fallback for Snowflake.
// It uses SHOW COLUMNS IN DATABASE which reads from the metadata service and is
// available even when INFORMATION_SCHEMA views are restricted by the Snowflake role.
// PK/UK/FK information is not available via SHOW commands.
func discoverColumnsSnowflakeSHOW(ctx context.Context, db *sql.DB) ([]DBColumn, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, "SHOW COLUMNS IN DATABASE")
	if err != nil {
		return nil, fmt.Errorf("snowflake SHOW COLUMNS fallback failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// SHOW COLUMNS IN DATABASE returns many columns — we need: table_name,
	// schema_name, column_name, data_type. Position varies by Snowflake version;
	// find by column name.
	colIdx := make(map[string]int, len(cols))
	for i, c := range cols {
		colIdx[strings.ToLower(c)] = i
	}

	mustGet := func(name string) (int, error) {
		if idx, ok := colIdx[name]; ok {
			return idx, nil
		}
		return 0, fmt.Errorf("snowflake SHOW COLUMNS: missing expected column %q", name)
	}

	iSchema, err := mustGet("schema_name")
	if err != nil {
		return nil, err
	}
	iTable, err := mustGet("table_name")
	if err != nil {
		return nil, err
	}
	iCol, err := mustGet("column_name")
	if err != nil {
		return nil, err
	}
	iType, err := mustGet("data_type")
	if err != nil {
		return nil, err
	}
	iNull := -1
	if idx, ok := colIdx["is_nullable"]; ok {
		iNull = idx
	} else if idx, ok := colIdx["null?"]; ok {
		iNull = idx
	}

	var result []DBColumn
	for rows.Next() {
		// Scan all columns into interface{} slice
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		toString := func(v interface{}) string {
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		}

		schema := toString(vals[iSchema])
		table := toString(vals[iTable])
		col := toString(vals[iCol])
		colType := strings.ToLower(toString(vals[iType]))

		// Skip INFORMATION_SCHEMA tables
		if strings.EqualFold(schema, "INFORMATION_SCHEMA") {
			continue
		}

		// data_type from SHOW comes as a JSON blob like {"type":"NUMBER","precision":38,"scale":0}
		// Try to extract just the type field.
		if strings.HasPrefix(colType, "{") {
			if idx := strings.Index(colType, `"type":"`); idx >= 0 {
				rest := colType[idx+8:]
				if end := strings.Index(rest, `"`); end >= 0 {
					colType = strings.ToLower(rest[:end])
				}
			}
		}

		notNull := false
		if iNull >= 0 {
			nv := strings.ToLower(toString(vals[iNull]))
			notNull = nv == "no" || nv == "n" || nv == "false"
		}

		result = append(result, DBColumn{
			Schema:  schema,
			Table:   table,
			Name:    col,
			Type:    colType,
			NotNull: notNull,
		})
	}
	return result, rows.Err()
}

// enrichSnowflakeFromSHOW augments the column map with PK/UK/FK information
// discovered via Snowflake-native SHOW commands. Snowflake's INFORMATION_SCHEMA
// doesn't expose KEY_COLUMN_USAGE so constraint info cannot be read from
// standard views; SHOW PRIMARY KEYS / SHOW UNIQUE KEYS / SHOW IMPORTED KEYS are
// the supported path. Also overlays `_gj_fk_metadata` rows when the table
// exists (test emulator and opt-in config-driven FK declarations in prod).
//
// All queries are non-fatal — the enrichment is strictly additive. Accounts
// without declared constraints, restricted roles, or environments where SHOW
// is unsupported (DuckDB-backed emulator) simply skip forward.
func enrichSnowflakeFromSHOW(ctx context.Context, db *sql.DB, cmap map[string]DBColumn) {
	setCol := func(schema, table, column string, apply func(*DBColumn)) {
		k := schema + ":" + table + ":" + column
		if v, ok := cmap[k]; ok {
			apply(&v)
			cmap[k] = v
		}
	}

	// SHOW PRIMARY KEYS IN DATABASE → mark PK columns
	querySnowflakeSHOWByName(ctx, db, "SHOW PRIMARY KEYS IN DATABASE",
		[]string{"schema_name", "table_name", "column_name"},
		func(vals []string) {
			setCol(vals[0], vals[1], vals[2], func(c *DBColumn) {
				c.PrimaryKey = true
				c.UniqueKey = true
			})
		})

	// SHOW UNIQUE KEYS IN DATABASE → mark UNIQUE columns
	querySnowflakeSHOWByName(ctx, db, "SHOW UNIQUE KEYS IN DATABASE",
		[]string{"schema_name", "table_name", "column_name"},
		func(vals []string) {
			setCol(vals[0], vals[1], vals[2], func(c *DBColumn) {
				c.UniqueKey = true
			})
		})

	// SHOW IMPORTED KEYS IN DATABASE → attach FK reference to the local column
	querySnowflakeSHOWByName(ctx, db, "SHOW IMPORTED KEYS IN DATABASE",
		[]string{"fk_schema_name", "fk_table_name", "fk_column_name",
			"pk_schema_name", "pk_table_name", "pk_column_name"},
		func(vals []string) {
			setCol(vals[0], vals[1], vals[2], func(c *DBColumn) {
				c.FKeySchema = vals[3]
				c.FKeyTable = vals[4]
				c.FKeyCol = vals[5]
				if c.FKeySchema == c.Schema && c.FKeyTable == c.Table {
					c.FKRecursive = true
				}
			})
		})

	// _gj_fk_metadata overlay: opt-in table used by the Snowflake emulator
	// (where SHOW IMPORTED KEYS isn't implemented) and by production users who
	// prefer to declare FKs in data rather than DDL.
	enrichSnowflakeFromFKMetadata(ctx, db, cmap)
}

// querySnowflakeSHOWByName runs a SHOW command and projects the named columns
// into the callback (position lookups are tolerant — missing columns are
// empty strings). Errors are swallowed; the caller uses this for best-effort
// enrichment only.
func querySnowflakeSHOWByName(
	ctx context.Context,
	db *sql.DB,
	stmt string,
	wantCols []string,
	onRow func([]string),
) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, stmt)
	if err != nil {
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return
	}
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}
	positions := make([]int, len(wantCols))
	for i, name := range wantCols {
		if p, ok := idx[name]; ok {
			positions[i] = p
		} else {
			positions[i] = -1
		}
	}

	scanBuf := make([]interface{}, len(cols))
	scanPtrs := make([]interface{}, len(cols))
	for i := range scanBuf {
		scanPtrs[i] = &scanBuf[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanPtrs...); err != nil {
			return
		}
		out := make([]string, len(wantCols))
		for i, p := range positions {
			if p < 0 || scanBuf[p] == nil {
				continue
			}
			out[i] = fmt.Sprintf("%v", scanBuf[p])
		}
		onRow(out)
	}
}

// enrichSnowflakeFromFKMetadata reads the optional _gj_fk_metadata overlay
// table and applies its FK rows to the column map. Non-fatal: the table is
// optional. Overlay entries only apply when the SHOW-based enrichment did
// not already set an FK on the same column.
func enrichSnowflakeFromFKMetadata(ctx context.Context, db *sql.DB, cmap map[string]DBColumn) {
	const q = `SELECT table_schema, table_name, column_name,
	              foreign_table_schema, foreign_table_name, foreign_column_name
	       FROM _gj_fk_metadata`

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, q)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var schema, table, column, fkSchema, fkTable, fkCol string
		if err := rows.Scan(&schema, &table, &column, &fkSchema, &fkTable, &fkCol); err != nil {
			return
		}
		k := schema + ":" + table + ":" + column
		v, ok := cmap[k]
		if !ok {
			continue
		}
		if v.FKeyTable != "" {
			// Already set by SHOW IMPORTED KEYS — keep the DDL-declared value.
			continue
		}
		v.FKeySchema = fkSchema
		v.FKeyTable = fkTable
		v.FKeyCol = fkCol
		if v.FKeySchema == v.Schema && v.FKeyTable == v.Table {
			v.FKRecursive = true
		}
		cmap[k] = v
	}
}

// discoverFunctionsSnowflake queries INFORMATION_SCHEMA.FUNCTIONS and parses
// the ARGUMENT_SIGNATURE column to extract typed parameters.
func discoverFunctionsSnowflake(ctx context.Context, db *sql.DB) ([]DBFunction, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, `
		SELECT function_schema, function_name, data_type, argument_signature
		FROM information_schema.functions
		WHERE function_schema NOT IN ('INFORMATION_SCHEMA')
		ORDER BY function_schema, function_name`)
	if err != nil {
		// If INFORMATION_SCHEMA.FUNCTIONS is unavailable, silently return empty.
		return nil, nil //nolint:nilerr
	}
	defer rows.Close()

	var funcs []DBFunction
	for rows.Next() {
		var schema, name, retType, argSig string
		if err := rows.Scan(&schema, &name, &retType, &argSig); err != nil {
			return nil, err
		}

		params := parseSnowflakeArgumentSignature(argSig)
		inputs := make([]DBFuncParam, len(params))
		for i, p := range params {
			inputs[i] = DBFuncParam{ID: i, Name: p.name, Type: p.typ}
		}

		funcs = append(funcs, DBFunction{
			Schema: schema,
			Name:   name,
			Type:   retType,
			Inputs: inputs,
		})
	}
	return funcs, rows.Err()
}

type sfArgParam struct {
	name string
	typ  string
}

// parseSnowflakeArgumentSignature parses Snowflake's ARGUMENT_SIGNATURE strings
// like "(A VARCHAR, B NUMBER(38,0), C VARIANT)" into typed parameters.
// Handles: empty "()", types with parens "NUMBER(38,0)", nested parens, and no-name args.
func parseSnowflakeArgumentSignature(sig string) []sfArgParam {
	sig = strings.TrimSpace(sig)
	if sig == "" || sig == "()" {
		return nil
	}
	// Strip outer parens
	if strings.HasPrefix(sig, "(") && strings.HasSuffix(sig, ")") {
		sig = sig[1 : len(sig)-1]
	}
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return nil
	}

	// Split by commas, respecting paren depth (e.g., NUMBER(38,0))
	var params []sfArgParam
	depth := 0
	start := 0
	for i := 0; i < len(sig); i++ {
		switch sig[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				params = append(params, parseSingleSfArg(sig[start:i]))
				start = i + 1
			}
		}
	}
	params = append(params, parseSingleSfArg(sig[start:]))
	return params
}

func parseSingleSfArg(s string) sfArgParam {
	s = strings.TrimSpace(s)
	// Expected format: "NAME TYPE" or just "TYPE"
	// Split on first space that isn't inside parens
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ', '\t':
			if depth == 0 {
				name := strings.TrimSpace(s[:i])
				typ := strings.TrimSpace(s[i+1:])
				if typ != "" {
					return sfArgParam{name: name, typ: typ}
				}
			}
		}
	}
	// No space found — it's just a type with no name
	return sfArgParam{typ: s}
}
