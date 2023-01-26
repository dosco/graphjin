package sdata

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"
)

type DBInfo struct {
	Type    string
	Version int
	Schema  string
	Name    string

	Tables    []DBTable
	Functions []DBFunction
	VTables   []VirtualTable `json:"-"`
	colMap    map[string]int
	tableMap  map[string]int
	hash      int
}

type DBTable struct {
	Comment      string
	Schema       string
	Name         string
	Type         string
	Columns      []DBColumn
	PrimaryCol   DBColumn
	SecondaryCol DBColumn
	FullText     []DBColumn
	Blocked      bool
	Func         DBFunction
	colMap       map[string]int
}

type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
}

func GetDBInfo(
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	var dbVersion int
	var dbSchema, dbName string
	var cols []DBColumn
	var funcs []DBFunction

	g := errgroup.Group{}

	g.Go(func() error {
		var row *sql.Row

		switch dbType {
		case "mysql":
			row = db.QueryRow(mysqlInfo)
		default:
			row = db.QueryRow(postgresInfo)
		}

		if err := row.Scan(&dbVersion, &dbSchema, &dbName); err != nil {
			return err
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

	return di, nil
}

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
		schema string
		table  string
	}

	tm := make(map[st][]DBColumn)
	for i, c := range cols {
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = i

		k := st{c.Schema, c.Table}
		tm[k] = append(tm[k], c)
	}

	for k, tcols := range tm {
		ti := NewDBTable(k.schema, k.table, "", tcols)
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

func NewDBTable(schema, name, _type string, cols []DBColumn) DBTable {
	ti := DBTable{
		Schema:  schema,
		Name:    name,
		Type:    _type,
		Columns: cols,
		colMap:  make(map[string]int, len(cols)),
	}

	for i, c := range cols {
		cols[i].Schema = schema
		cols[i].Table = name

		switch {
		case c.FullText:
			ti.FullText = append(ti.FullText, c)

		case c.PrimaryKey:
			ti.PrimaryCol = c

		}
		ti.colMap[c.Name] = i
	}
	return ti
}

func (di *DBInfo) AddTable(t DBTable) {
	for i, c := range t.Columns {
		di.colMap[(c.Schema + ":" + c.Table + ":" + c.Name)] = i
	}

	i := len(di.Tables)
	di.Tables = append(di.Tables, t)
	di.tableMap[(t.Schema + ":" + t.Name)] = i
}

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

func (di *DBInfo) GetTable(schema, table string) (*DBTable, error) {
	tid, ok := di.tableMap[(schema + ":" + table)]
	if !ok {
		return nil, fmt.Errorf("table: '%s.%s' not found", schema, table)
	}

	return &di.Tables[tid], nil
}

type DBColumn struct {
	Comment     string
	ID          int32
	Name        string
	Type        string
	Array       bool
	NotNull     bool
	PrimaryKey  bool
	UniqueKey   bool
	FullText    bool
	FKRecursive bool
	FKeySchema  string
	FKeyTable   string
	FKeyCol     string
	Blocked     bool
	Table       string
	Schema      string
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

		if err != nil {
			return nil, err
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

	var cols []DBColumn
	for _, c := range cmap {
		cols = append(cols, c)
	}

	return cols, nil
}

type DBFunction struct {
	Comment string
	Schema  string
	Name    string
	Type    string
	Agg     bool
	Inputs  []DBFuncParam
	Outputs []DBFuncParam
}

type DBFuncParam struct {
	ID    int
	Name  string
	Type  string
	Array bool
}

func DiscoverFunctions(db *sql.DB, dbtype string, blockList []string) ([]DBFunction, error) {
	var sqlStmt string

	switch dbtype {
	case "mysql":
		sqlStmt = mysqlFunctionsStmt
	default:
		sqlStmt = postgresFunctionsStmt
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
		var pn, pt, pk string
		var pid int

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
		param := DBFuncParam{ID: pid, Name: pn, Type: pt}

		if strings.HasSuffix(pt, "[]") {
			param.Array = true
		}

		switch pk {
		case "IN", "in":
			funcs[i].Inputs = append(funcs[i].Inputs, param)
		case "OUT", "out":
			funcs[i].Outputs = append(funcs[i].Outputs, param)
		}
	}

	return funcs, nil
}

func (fn *DBFunction) GetInput(name string) (ret DBFuncParam, err error) {
	for _, in := range fn.Inputs {
		if in.Name == name {
			return in, nil
		}
	}
	return ret, fmt.Errorf("function input '%s' not found", name)
}

func (di *DBInfo) Hash() int {
	return di.hash
}

func isInList(val string, s []string) bool {
	for _, v := range s {
		regex := fmt.Sprintf("^%s$", v)
		if matched, _ := regexp.MatchString(regex, val); matched {
			return true
		}
	}
	return false
}
