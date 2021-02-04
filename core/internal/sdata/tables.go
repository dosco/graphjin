package sdata

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/gobuffalo/flect"
)

type DBInfo struct {
	Version   int
	Type      string
	Tables    []DBTable
	Functions []DBFunction
	VTables   []VirtualTable
	colMap    map[string]*DBColumn
	tableMap  map[string]*DBTable
}

type DBTable struct {
	Schema       string
	Name         string
	Type         string
	Columns      []DBColumn
	PrimaryCol   DBColumn
	SecondaryCol DBColumn
	FullText     []DBColumn
	Singular     string
	Plural       string
	Blocked      bool
	colMap       map[string]int
}

type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
}

type st struct {
	schema, table string
}

func GetDBInfo(db *sql.DB, dbtype string, blockList []string) (*DBInfo, error) {
	var version string
	_ = db.QueryRow(`SHOW server_version_num`).Scan(&version)

	cols, err := DiscoverColumns(db, dbtype, blockList)
	if err != nil {
		return nil, err
	}

	funcs, err := DiscoverFunctions(db, blockList)
	if err != nil {
		return nil, err
	}

	di := NewDBInfo(dbtype, version, cols, funcs, blockList)

	return di, nil
}

func NewDBInfo(
	dbtype string,
	version string,
	cols []DBColumn,
	funcs []DBFunction,
	blockList []string) *DBInfo {

	di := &DBInfo{
		Type:      dbtype,
		Functions: funcs,
		colMap:    make(map[string]*DBColumn),
		tableMap:  make(map[string]*DBTable),
	}

	if version == "" {
		version = "110000"
	}

	di.Version, _ = strconv.Atoi(version)

	tm := make(map[st][]DBColumn)
	for i := range cols {
		c := cols[i]
		c.Key = strings.ToLower(c.Name)
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = &c

		k1 := st{c.Schema, c.Table}
		tm[k1] = append(tm[k1], c)
	}

	for k, tcols := range tm {
		ti := NewDBTable(k.schema, k.table, "", tcols)
		ti.Blocked = isInList(ti.Name, blockList)
		di.AddTable(ti)
	}
	return di
}

func NewDBTable(schema, name, _type string, cols []DBColumn) DBTable {
	key := strings.ToLower(name)
	singular := flect.Singularize(key)
	plural := flect.Pluralize(key)

	ti := DBTable{
		Schema:   schema,
		Name:     name,
		Type:     _type,
		Columns:  cols,
		Singular: singular,
		Plural:   plural,
		colMap:   make(map[string]int, len(cols)),
	}

	for i := range cols {
		c := &cols[i]

		switch {
		case c.FullText:
			ti.FullText = append(ti.FullText, cols[i])

		case c.PrimaryKey:
			ti.PrimaryCol = cols[i]
		}
		ti.colMap[c.Key] = i
	}
	return ti
}

func (di *DBInfo) AddTable(t DBTable) {
	for i, c := range t.Columns {
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = &t.Columns[i]
		di.colMap[(":" + c.Table + ":" + c.Name)] = &t.Columns[i]
	}

	di.Tables = append(di.Tables, t)
	di.tableMap[(t.Schema + ":" + t.Name)] = &t

	k := (":" + t.Name)
	if _, ok := di.tableMap[k]; !ok {
		di.tableMap[k] = &t
	}
}

func (di *DBInfo) GetColumn(schema, table, column string) (*DBColumn, error) {
	c, ok := di.colMap[(schema + ":" + table + ":" + column)]
	if !ok {
		return nil, fmt.Errorf("column: '%s.%s.%s' not found", schema, table, column)
	}

	return c, nil
}

func (di *DBInfo) GetTable(schema, table string) (*DBTable, error) {
	t, ok := di.tableMap[(schema + ":" + table)]
	if !ok {
		return nil, fmt.Errorf("table: '%s.%s' not found", schema, table)
	}

	return t, nil
}

type DBColumn struct {
	Name       string
	Key        string
	Type       string
	Array      bool
	NotNull    bool
	PrimaryKey bool
	UniqueKey  bool
	FullText   bool
	FKeySchema string
	FKeyTable  string
	FKeyCol    string
	Blocked    bool
	Table      string
	Schema     string
}

func DiscoverColumns(db *sql.DB, dbtype string, blockList []string) ([]DBColumn, error) {
	var sqlStmt string

	switch dbtype {
	case "mysql":
		sqlStmt = mysqlColumnsStmt
	default:
		sqlStmt = postgresColumnsStmt
	}

	rows, err := db.Query(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}
	defer rows.Close()

	cmap := make(map[string]DBColumn)

	for rows.Next() {
		var c DBColumn

		err = rows.Scan(&c.Schema, &c.Table, &c.Name, &c.Type, &c.NotNull, &c.PrimaryKey, &c.UniqueKey, &c.Array, &c.FullText, &c.FKeySchema, &c.FKeyTable, &c.FKeyCol)

		if err != nil {
			return nil, err
		}

		k := (c.Schema + ":" + c.Table + ":" + c.Name)
		v := cmap[k]
		if v.Key == "" {
			v = c
			v.Blocked = isInList(v.Key, blockList)
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
		cmap[k] = v
	}

	var cols []DBColumn
	for _, c := range cmap {
		cols = append(cols, c)
	}

	return cols, nil
}

type DBFunction struct {
	Name   string
	Params []DBFuncParam
}

type DBFuncParam struct {
	ID   int
	Name sql.NullString
	Type string
}

func DiscoverFunctions(db *sql.DB, blockList []string) ([]DBFunction, error) {
	rows, err := db.Query(functionsStmt)
	if err != nil {
		return nil, fmt.Errorf("Error fetching functions: %s", err)
	}
	defer rows.Close()

	var funcs []DBFunction
	fm := make(map[string]int)

	parameterIndex := 1
	for rows.Next() {
		var fn, fid string
		fp := DBFuncParam{}

		err = rows.Scan(&fn, &fid, &fp.Type, &fp.Name, &fp.ID)
		if err != nil {
			return nil, err
		}

		if !fp.Name.Valid {
			fp.Name.String = fmt.Sprintf("%d", parameterIndex)
			fp.Name.Valid = true
		}

		if i, ok := fm[fid]; ok {
			funcs[i].Params = append(funcs[i].Params, fp)
		} else {
			if isInList(fn, blockList) {
				continue
			}
			funcs = append(funcs, DBFunction{Name: fn, Params: []DBFuncParam{fp}})
			fm[fid] = len(funcs) - 1
		}
		parameterIndex++
	}

	return funcs, nil
}

func isInList(val string, s []string) bool {
	for _, v := range s {
		if strings.EqualFold(v, val) {
			return true
		}
	}
	return false
}
