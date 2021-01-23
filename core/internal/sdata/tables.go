package sdata

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

type DBInfo struct {
	Version   int
	Type      string
	Tables    []DBTable
	Columns   [][]DBColumn
	Functions []DBFunction
	VTables   []VirtualTable
	colMap    map[string]*DBColumn
}

type VirtualTable struct {
	Name       string
	IDColumn   string
	TypeColumn string
	FKeyColumn string
}

func GetDBInfo(db *sql.DB, dbtype string, blockList []string) (*DBInfo, error) {
	var err error

	di := &DBInfo{Type: dbtype}
	version := "110000"

	_ = db.QueryRow(`SHOW server_version_num`).Scan(&version)

	di.Version, err = strconv.Atoi(version)
	if err != nil {
		return nil, err
	}

	cols, err := GetColumns(db, dbtype)
	if err != nil {
		return nil, err
	}

	tm := make(map[string][]DBColumn)

	for _, v := range cols {
		v.Blocked = isInList(v.Name, blockList)
		if _, ok := tm[v.Table]; !ok {
			di.Tables = append(di.Tables, DBTable{
				Name:    v.Table,
				Key:     strings.ToLower(v.Table),
				Schema:  v.Schema,
				Blocked: isInList(v.Table, blockList),
			})
		}
		tm[v.Table] = append(tm[v.Table], v)
	}

	di.colMap = make(map[string]*DBColumn)

	for _, t := range di.Tables {
		tc := tm[t.Name]
		di.Columns = append(di.Columns, tc)

		for i, c := range tc {
			di.colMap[(c.Table + c.Name)] = &tc[i]
		}
	}

	di.Functions, err = GetFunctions(db, blockList)
	if err != nil {
		return nil, err
	}

	return di, nil
}

func (di *DBInfo) AddTable(t DBTable, cols []DBColumn) {
	di.Tables = append(di.Tables, t)
	di.Columns = append(di.Columns, cols)

	for i := range cols {
		c := &cols[i]
		di.colMap[(t.Key + c.Key)] = c
	}
}

func (di *DBInfo) GetColumn(table, column string) (*DBColumn, error) {
	c, ok := di.colMap[(table + column)]
	if !ok {
		return nil, fmt.Errorf("column: '%s.%s' not found", table, column)
	}

	return c, nil
}

type DBTable struct {
	Name    string
	Key     string
	Type    string
	Schema  string
	Blocked bool
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

func GetColumns(db *sql.DB, dbtype string) (map[string]DBColumn, error) {
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

		v := cmap[(c.Table + c.Name)]
		if v.Key == "" {
			v = c
			v.Key = strings.ToLower(c.Name)
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
		if c.FKeyTable != "" {
			v.FKeyTable = c.FKeyTable
		}
		if c.FKeySchema != "" {
			v.FKeySchema = c.FKeySchema
		}
		if c.FKeyCol != "" {
			v.FKeyCol = c.FKeyCol
		}
		cmap[(c.Table + c.Name)] = v
	}

	return cmap, nil
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

func GetFunctions(db *sql.DB, blockList []string) ([]DBFunction, error) {
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
