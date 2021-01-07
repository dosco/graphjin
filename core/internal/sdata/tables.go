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

	di.Tables, err = GetTables(db)
	if err != nil {
		return nil, err
	}

	var tables []string

	for i, t := range di.Tables {
		di.Tables[i].Blocked = isInList(t.Name, blockList)
		tables = append(tables, t.Name)
	}

	cols, err := GetColumns(db, dbtype, tables)
	if err != nil {
		return nil, err
	}

	for _, t := range tables {
		c := cols[t]
		for i := range c {
			c[i].Blocked = isInList(c[i].Name, blockList)
		}
		di.Columns = append(di.Columns, c)
	}
	di.colMap = newColMap(di.Tables, di.Columns)

	di.Functions, err = GetFunctions(db, blockList)
	if err != nil {
		return nil, err
	}

	return di, nil
}

func (di *DBInfo) AddTable(t DBTable, cols []DBColumn) {
	t.ID = di.Tables[len(di.Tables)-1].ID

	di.Tables = append(di.Tables, t)

	for i := range cols {
		cols[i].ID = int16(i)
		c := &cols[i]
		di.colMap[(t.Key + c.Key)] = c
	}
	di.Columns = append(di.Columns, cols)
}

func (di *DBInfo) GetColumn(table, column string) (*DBColumn, error) {
	c, ok := di.colMap[(table + column)]
	if !ok {
		return nil, fmt.Errorf("column: '%s.%s' not found", table, column)
	}

	return c, nil
}

type DBTable struct {
	ID      int
	Name    string
	Key     string
	Type    string
	Blocked bool
}

func GetTables(db *sql.DB) ([]DBTable, error) {
	//	t.table_schema NOT IN ('information_schema', 'pg_catalog')

	sqlStmt := `
SELECT
	t.table_name as "name",
	t.table_type as "type"
FROM 
	information_schema.tables t
WHERE
	t.table_schema NOT IN ('information_schema', 'pg_catalog')
	AND t.table_name NOT IN ('schema_version');`

	var tables []DBTable

	rows, err := db.Query(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("Error fetching tables: %s", err)
	}
	defer rows.Close()

	for i := 0; rows.Next(); i++ {
		t := DBTable{ID: i}
		err = rows.Scan(&t.Name, &t.Type)
		if err != nil {
			return nil, err
		}
		t.Key = strings.ToLower(t.Name)
		tables = append(tables, t)
	}

	return tables, nil
}

type DBColumn struct {
	ID         int16
	Name       string
	Key        string
	Type       string
	Array      bool
	NotNull    bool
	PrimaryKey bool
	UniqueKey  bool
	FKeySchema string
	FKeyTable  string
	FKeyCol    string
	Blocked    bool
	Table      string
}

func GetColumns(db *sql.DB, dbtype string, tables []string) (
	map[string][]DBColumn, error) {
	cols := make(map[string][]DBColumn, len(tables))

	if len(tables) == 0 {
		return cols, nil
	}

	var sqlStmt string

	switch dbtype {
	case "mysql":
		sqlStmt = mysqlColumnInfo
	default:
		sqlStmt = postgresColumnInfo
	}

	rows, err := db.Query(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}
	defer rows.Close()

	cmap := make(map[string]DBColumn)

	for rows.Next() {
		var c DBColumn

		err = rows.Scan(&c.Table, &c.Name, &c.Type, &c.NotNull, &c.Array, &c.PrimaryKey, &c.UniqueKey, &c.FKeySchema, &c.FKeyTable, &c.FKeyCol)
		if err != nil {
			return nil, err
		}

		v, _ := cmap[(c.Table + c.Name)]
		if v.Key == "" {
			v = c
			v.Key = strings.ToLower(c.Name)
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

	for _, v := range cmap {
		cols[v.Table] = append(cols[v.Table], v)
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

func GetFunctions(db *sql.DB, blockList []string) ([]DBFunction, error) {
	sqlStmt := `
SELECT 
	r.routine_name as func_name, 
	p.specific_name as func_id,
	p.data_type as func_type, 
	p.parameter_name as param_name,
	p.ordinal_position	as param_id
FROM 
	information_schema.routines r
RIGHT JOIN 
	information_schema.parameters p
	ON (r.specific_name = p.specific_name and p.ordinal_position IS NOT NULL)	
WHERE 
	p.specific_schema NOT IN ('information_schema', 'pg_catalog')
ORDER BY 
	r.routine_name, p.ordinal_position;`

	rows, err := db.Query(sqlStmt)
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

func newColMap(tables []DBTable, columns [][]DBColumn) map[string]*DBColumn {
	cm := make(map[string]*DBColumn, len(tables))

	for i, t := range tables {
		for n, c := range columns[i] {
			cm[(t.Name + c.Name)] = &columns[i][n]
		}
	}

	return cm
}

func toList(s []string) string {
	var sb strings.Builder
	for i := range s {
		if i != 0 {
			sb.WriteString(",'" + s[i] + "'")
		} else {
			sb.WriteString("'" + s[i] + "'")
		}
	}
	return sb.String()
}

func isInList(val string, s []string) bool {
	for _, v := range s {
		if strings.EqualFold(v, val) {
			return true
		}
	}
	return false
}
