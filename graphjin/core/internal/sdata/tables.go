package sdata

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/util"
	"golang.org/x/sync/errgroup"
)

// DBInfo holds the database schema information
type DBInfo struct {
	Type    string
	Version int
	Schema  string
	Name    string

	Tables        []DBTable
	Functions     []DBFunction
	VTables       []VirtualTable  `json:"-"`
	CompositeFKs  []CompositeFKInfo `json:"-"`
	colMap        map[string]int
	tableMap      map[string]int
	hash          int
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
	Database     string
	Columns      []DBColumn
	PrimaryCols  []DBColumn
	PrimaryCol   DBColumn // backward compat: alias for PrimaryCols[0]
	SecondaryCol DBColumn
	FullText     []DBColumn
	Blocked        bool
	Func           DBFunction
	ClusteringKeys    []string // Snowflake clustering key columns (normalized to snake_case)
	PartitionKey      string   // Partition column name (from config, e.g., "created_at")
	PartitionRangeDays int     // Default range in days for auto-injected partition filter (0 = warn only)
	colMap            map[string]int
}

// VirtualTable holds the virtual table information
type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
}

// GetDBInfo returns the database schema information
func GetDBInfo(
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	var dbVersion int
	var dbSchema, dbName string
	var cols []DBColumn
	var funcs []DBFunction
	var compositeFKs []CompositeFKInfo

	g := errgroup.Group{}

	g.Go(func() error {
		var row *sql.Row

		switch dbType {
		case "postgres", "":
			row = db.QueryRow(postgresInfo)
		case "mysql":
			row = db.QueryRow(mysqlInfo)
		case "mariadb":
			row = db.QueryRow(mariadbInfo)
		case "sqlite":
			row = db.QueryRow(sqliteInfo)
		case "oracle":
			row = db.QueryRow(oracleInfo)
		case "mssql":
			row = db.QueryRow(mssqlInfo)
		case "snowflake":
			row = db.QueryRow(snowflakeInfo)
		case "mongodb":
			// MongoDB returns info via the driver's introspection
			row = db.QueryRow(mongodbInfo)
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
		if cols, err = DiscoverColumns(db, dbType, blockList); err != nil {
			return err
		}

		if funcs, err = DiscoverFunctions(db, dbType, blockList); err != nil {
			return err
		}
		if compositeFKs, err = DiscoverCompositeFKs(db, dbType); err != nil {
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
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
		if ck, err := discoverClusteringKeys(db); err == nil {
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
func (t *DBTable) GetColumnIndex(name string) (int, bool) {
	if t.colMap == nil {
		return 0, false
	}
	i, ok := t.colMap[name]
	return i, ok
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

	cid, ok := t.colMap[column]
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
	Comment     string
	ID          int32
	Name        string
	OrigName    string // Original name before normalization (e.g., PascalCase for MSSQL)
	Type        string
	Array       bool
	NotNull     bool
	PrimaryKey  bool
	UniqueKey   bool
	FullText    bool
	FKRecursive  bool
	FKeyDatabase string // Target database for cross-database FKs (empty = same db)
	FKeySchema   string
	FKeyTable    string
	FKeyCol      string
	FKeyIsUnique bool   // True if FK target column is PK/unique (for correct rel type)
	Blocked     bool
	Table       string
	Schema      string
	Database    string
	Default     string
	Index       bool
	IndexName   string
	FKOnDelete  string
	FKOnUpdate  string

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
func DiscoverColumns(db *sql.DB, dbtype string, blockList []string) ([]DBColumn, error) {
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

	rows, err := db.Query(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
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

		if dbtype == "sqlite" || dbtype == "oracle" || dbtype == "mssql" || dbtype == "snowflake" {
			c.Name = util.ToSnake(c.Name)
			c.Table = strings.ToLower(c.Table)
			c.Schema = strings.ToLower(c.Schema)
			c.Type = strings.ToLower(c.Type)
			c.FKeyTable = strings.ToLower(c.FKeyTable)
			c.FKeySchema = strings.ToLower(c.FKeySchema)
			c.FKeyCol = util.ToSnake(c.FKeyCol)
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

	// For MSSQL, run a supplementary query to detect PKs for views.
	// Views lack sys.indexes entries, so the main query reports primary_key=0
	// for all view columns. This uses sys.dm_exec_describe_first_result_set
	// to trace view columns back to their source base table PKs.
	if dbtype == "mssql" {
		rows2, err := db.Query(mssqlViewPKsStmt)
		if err == nil {
			defer rows2.Close()
			for rows2.Next() {
				var schema, table, column string
				if err := rows2.Scan(&schema, &table, &column); err != nil {
					continue
				}
				column = util.ToSnake(column)
				table = strings.ToLower(table)
				schema = strings.ToLower(schema)

				k := schema + ":" + table + ":" + column
				if v, ok := cmap[k]; ok && !v.PrimaryKey {
					v.PrimaryKey = true
					v.UniqueKey = true
					cmap[k] = v
				}
			}
		}
		// Silently ignore errors — falls back to config-based override
	}

	var cols []DBColumn
	for _, c := range cmap {
		cols = append(cols, c)
	}

	return cols, nil
}

// DiscoverCompositeFKs returns metadata about composite (multi-column) foreign key
// constraints for the given database type.
func DiscoverCompositeFKs(db *sql.DB, dbtype string) ([]CompositeFKInfo, error) {
	switch dbtype {
	case "postgres", "":
		return discoverCompositeFKsPostgres(db)
	case "mysql":
		return discoverCompositeFKsCSV(db, dbtype, compositeFKQueryMySQL)
	case "mariadb":
		return discoverCompositeFKsCSV(db, dbtype, compositeFKQueryMySQL) // identical to MySQL
	case "sqlite":
		return discoverCompositeFKsCSV(db, dbtype, compositeFKQuerySQLite)
	case "oracle":
		return discoverCompositeFKsCSV(db, dbtype, compositeFKQueryOracle)
	case "mssql":
		return discoverCompositeFKsCSV(db, dbtype, compositeFKQueryMSSQL)
	case "snowflake":
		// Snowflake uses a custom _gj_fk_metadata table that may not exist
		result, err := discoverCompositeFKsCSV(db, dbtype, compositeFKQuerySnowflake)
		if err != nil {
			return nil, nil // non-fatal: table may not exist
		}
		return result, nil
	default:
		return nil, nil
	}
}

const compositeFKQueryMySQL = `
SELECT kcu.table_schema, kcu.table_name, kcu.constraint_name,
       GROUP_CONCAT(kcu.column_name ORDER BY kcu.ordinal_position) AS local_columns,
       kcu.referenced_table_schema, kcu.referenced_table_name,
       GROUP_CONCAT(kcu.referenced_column_name ORDER BY kcu.ordinal_position) AS fkey_columns
FROM information_schema.key_column_usage kcu
JOIN information_schema.table_constraints tc
  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND kcu.table_schema NOT IN ('_graphjin', 'information_schema', 'performance_schema', 'mysql', 'sys')
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

const compositeFKQueryOracle = `
SELECT ac.owner, ac.table_name, ac.constraint_name,
       LISTAGG(acc.column_name, ',') WITHIN GROUP (ORDER BY acc.position) AS local_columns,
       r_ac.owner AS fkey_schema, r_ac.table_name AS fkey_table,
       LISTAGG(r_acc.column_name, ',') WITHIN GROUP (ORDER BY r_acc.position) AS fkey_columns
FROM all_constraints ac
JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
JOIN all_constraints r_ac ON ac.r_constraint_name = r_ac.constraint_name AND ac.r_owner = r_ac.owner
JOIN all_cons_columns r_acc ON r_ac.constraint_name = r_acc.constraint_name AND r_ac.owner = r_acc.owner
  AND acc.position = r_acc.position
WHERE ac.constraint_type = 'R'
  AND ac.owner NOT IN ('_GRAPHJIN', 'SYS', 'SYSTEM')
GROUP BY ac.owner, ac.table_name, ac.constraint_name, r_ac.owner, r_ac.table_name
HAVING COUNT(*) > 1`

const compositeFKQueryMSSQL = `
SELECT s.name, t.name, OBJECT_NAME(fkc.constraint_object_id),
       STRING_AGG(c.name, ',') WITHIN GROUP (ORDER BY fkc.constraint_column_id) AS local_columns,
       rs.name, rt.name,
       STRING_AGG(rc.name, ',') WITHIN GROUP (ORDER BY fkc.constraint_column_id) AS fkey_columns
FROM sys.foreign_key_columns fkc
JOIN sys.columns c ON fkc.parent_object_id = c.object_id AND fkc.parent_column_id = c.column_id
JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
JOIN sys.tables t ON fkc.parent_object_id = t.object_id
JOIN sys.schemas s ON t.schema_id = s.schema_id
JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id
JOIN sys.schemas rs ON rt.schema_id = rs.schema_id
GROUP BY s.name, t.name, fkc.constraint_object_id, rs.name, rt.name
HAVING COUNT(*) > 1`

const compositeFKQuerySnowflake = `
SELECT table_schema, table_name,
       table_schema || ':' || table_name || ':' || foreign_table_name AS constraint_name,
       LISTAGG(column_name, ',') WITHIN GROUP (ORDER BY column_name) AS local_columns,
       foreign_table_schema, foreign_table_name,
       LISTAGG(foreign_column_name, ',') WITHIN GROUP (ORDER BY foreign_column_name) AS fkey_columns
FROM _gj_fk_metadata
GROUP BY table_schema, table_name, foreign_table_schema, foreign_table_name
HAVING COUNT(*) > 1`

func discoverCompositeFKsPostgres(db *sql.DB) ([]CompositeFKInfo, error) {
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

	rows, err := db.Query(query)
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
func discoverCompositeFKsCSV(db *sql.DB, dbtype, query string) ([]CompositeFKInfo, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("error fetching composite FKs: %w", err)
	}
	defer rows.Close()

	normalize := dbtype == "oracle" || dbtype == "mssql" || dbtype == "snowflake"

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
func DiscoverFunctions(db *sql.DB, dbtype string, blockList []string) ([]DBFunction, error) {
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
		// Snowflake emulator does not expose information_schema.functions consistently.
		// Return no discovered functions for now.
		return nil, nil
	case "mongodb":
		// MongoDB doesn't have user-defined functions in the SQL sense
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, mongodb", dbtype)
	}

	rows, err := db.Query(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching functions: %s", err)
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
func discoverClusteringKeys(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query(snowflakeClusteringStmt)
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
		// Normalize: snake_case first (to split PascalCase), then lowercase.
		// Snowflake typically returns UPPER_CASE identifiers, but this order
		// also handles the unlikely PascalCase edge case correctly.
		// Note: expression-based clustering keys (e.g., CAST(col AS DATE))
		// will not match any column and are gracefully skipped.
		col = strings.ToLower(util.ToSnake(col))
		keys = append(keys, col)
	}
	return keys
}
