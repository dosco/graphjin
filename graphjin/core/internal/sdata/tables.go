package sdata

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

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
	Database             string
	Columns              []DBColumn
	PrimaryCols          []DBColumn
	PrimaryCol           DBColumn // backward compat: alias for PrimaryCols[0]
	SecondaryCol         DBColumn
	FullText             []DBColumn
	Blocked              bool
	Func                 DBFunction
	ClusteringKeys       []string          // Warehouse clustering key columns (normalized to snake_case)
	ClusteringOrder      map[string]string // Cassandra clustering column -> asc|desc
	PartitionKeys        []string          // Cassandra composite partition key columns (in order)
	AllowFiltering       bool              // Cassandra: per-table ALLOW FILTERING opt-in
	PartitionKey         string            // Partition column name (from config, e.g., "created_at")
	PartitionRangeDays   int               // Default range in days for auto-injected partition filter (0 = warn only)
	PartitionNone        bool
	ImplicitPartitionKey string
	// StrictColumns marks Columns as the table's complete surface: selecting
	// a column not in the list is a compile error instead of the lenient
	// pass-through remote tables get by default. Set by integrations whose
	// column set is closed (filesystem tables); OpenAPI and user-resolver
	// tables leave it false because their registered columns may be partial
	// or absent entirely.
	StrictColumns bool
	colMap        map[string]int

	// Args lists synthetic field-level arguments (used for top-level
	// remote tables that take query/path params as GraphQL args).
	Args []DBColumn
}

// VirtualTable holds the virtual table information
type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
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

// AddColumn adds a column to an existing table and updates the lookup maps.
func (di *DBInfo) AddColumn(schema, table string, c DBColumn) error {
	t, err := di.GetTable(schema, table)
	if err != nil {
		return err
	}
	if _, ok := t.colMap[c.Name]; ok {
		return nil
	}
	c.Schema = schema
	c.Table = table
	c.ID = int32(len(t.Columns) + 1)
	i := len(t.Columns)
	t.Columns = append(t.Columns, c)
	t.colMap[c.Name] = i
	di.colMap[(schema + ":" + table + ":" + c.Name)] = i
	return nil
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
	Comment        string
	ID             int32
	Name           string
	OrigName       string // Original name before normalization (e.g., PascalCase for MSSQL)
	Type           string
	Array          bool
	NotNull        bool
	PrimaryKey     bool
	UniqueKey      bool
	FullText       bool
	FKRecursive    bool
	FKeyDatabase   string // Target database for cross-database FKs (empty = same db)
	FKeySchema     string
	FKeyTable      string
	FKeyCol        string
	FKeyIsUnique   bool // True if FK target column is PK/unique (for correct rel type)
	Blocked        bool
	Table          string
	Schema         string
	Database       string
	Default        string
	Index          bool
	IndexName      string
	FKOnDelete     string
	FKOnUpdate     string
	CodeSQLVirtual string

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

var implicitPartitionCandidates = []string{
	"created_at",
	"event_time",
	"updated_at",
	"timestamp",
	"ingested_at",
}

func resolveImplicitPartitionKey(t *DBTable) string {
	for _, cand := range implicitPartitionCandidates {
		for i := range t.Columns {
			if strings.EqualFold(t.Columns[i].Name, cand) && isTemporalType(t.Columns[i].Type) {
				return t.Columns[i].Name
			}
		}
	}
	return ""
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
