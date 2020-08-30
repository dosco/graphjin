package sdata

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgtype"
)

type DBInfo struct {
	Version   int
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

func GetDBInfo(db *sql.DB, schema string, blockList []string) (*DBInfo, error) {
	di := &DBInfo{}
	var version string

	err := db.QueryRow(`SHOW server_version_num`).Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("error fetching version: %w", err)
	}

	di.Version, err = strconv.Atoi(version)
	if err != nil {
		return nil, err
	}

	di.Tables, err = GetTables(db, schema)
	if err != nil {
		return nil, err
	}

	var tables []string

	for i, t := range di.Tables {
		di.Tables[i].Blocked = isInList(t.Name, blockList)
		tables = append(tables, t.Name)
	}

	cols, err := GetColumns(db, schema, tables)
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

	di.Functions, err = GetFunctions(db, schema, blockList)
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
	c, ok := di.colMap[strings.ToLower(table+column)]
	if !ok {
		return nil, fmt.Errorf("column: %s.%s not found", table, column)
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

func GetTables(db *sql.DB, schema string) ([]DBTable, error) {
	sqlStmt := `
SELECT
	c.relname as "name",
	CASE c.relkind WHEN 'r' THEN 'table'
		WHEN 'v' THEN 'view'
		WHEN 'm' THEN 'materialized view'
		WHEN 'f' THEN 'foreign table' 
	END as "type"
FROM pg_catalog.pg_class c
	LEFT JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','v','m','f','')
	AND n.nspname = $1
	AND pg_catalog.pg_table_is_visible(c.oid);`

	var tables []DBTable

	rows, err := db.Query(sqlStmt, schema)
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
		if t.Key != "schema_migrations" && t.Key != "ar_internal_metadata" {
			tables = append(tables, t)
		}
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
	FKeyTable  string
	FKeyColID  []int16
	fKeyColID  pgtype.Int2Array
	Blocked    bool
	Table      string
}

func GetColumns(db *sql.DB, schema string, tables []string) (map[string][]DBColumn, error) {
	sqlStmt := `
SELECT  
	c.relname as table,
	f.attnum AS id,  
	f.attname AS name,  
	f.attnotnull AS notnull,  
	pg_catalog.format_type(f.atttypid,f.atttypmod) AS type,  
	CASE
	 WHEN f.attndims != 0 THEN true
	 WHEN right(pg_catalog.format_type(f.atttypid,f.atttypmod), 2) = '[]' THEN true
	 ELSE false
	END AS array,
	CASE  
		WHEN p.contype = ('p'::char) THEN true  
		ELSE false 
	END AS primarykey,  
	CASE  
		WHEN p.contype = ('u'::char) THEN true  
		ELSE false
	END AS uniquekey,
	CASE
		WHEN p.contype = ('f'::char) THEN g.relname 
		ELSE ''::text
	END AS foreignkey,
	CASE
		WHEN p.contype = ('f'::char) THEN p.confkey::int2[]
		ELSE ARRAY[]::int2[]
	END AS foreignkey_fieldnum
FROM 
	pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid  
	LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum  
	LEFT JOIN pg_namespace n ON n.oid = c.relnamespace  
	LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)  
	LEFT JOIN pg_class AS g ON p.confrelid = g.oid  
WHERE 
	c.relkind IN ('r', 'v', 'm', 'f')
	AND n.nspname = $1 -- Replace with Schema name  
	AND c.relname IN (` + toList(tables) + `)
	AND f.attnum > 0
	AND f.attisdropped = false
ORDER BY id;`

	rows, err := db.Query(sqlStmt, schema)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}
	defer rows.Close()

	cmap := make(map[string]map[int16]DBColumn, len(tables))

	for rows.Next() {
		var t string
		var c DBColumn

		err = rows.Scan(&t, &c.ID, &c.Name, &c.NotNull, &c.Type, &c.Array, &c.PrimaryKey, &c.UniqueKey, &c.FKeyTable, &c.fKeyColID)
		if err != nil {
			return nil, err
		}

		if _, ok := cmap[t]; !ok {
			cmap[t] = make(map[int16]DBColumn)
		}

		if v, ok := cmap[t][c.ID]; ok {
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
			if c.fKeyColID.Elements != nil {
				v.fKeyColID = c.fKeyColID
				err := v.fKeyColID.AssignTo(&v.FKeyColID)
				if err != nil {
					return nil, err
				}
			}
			cmap[t][c.ID] = v

		} else {
			err := c.fKeyColID.AssignTo(&c.FKeyColID)
			if err != nil {
				return nil, err
			}
			c.Key = strings.ToLower(c.Name)
			if c.PrimaryKey {
				c.UniqueKey = true
			}
			cmap[t][c.ID] = c
		}
	}

	cols := make(map[string][]DBColumn, len(tables))

	for t, v := range cmap {
		for id := range v {
			if _, ok := cols[t]; !ok {
				cols[t] = make([]DBColumn, 0, len(v))
			}
			cols[t] = append(cols[t], v[id])
		}
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

func GetFunctions(db *sql.DB, schema string, blockList []string) ([]DBFunction, error) {
	sqlStmt := `
SELECT 
	routines.routine_name, 
	parameters.specific_name,
	parameters.data_type, 
	parameters.parameter_name,
	parameters.ordinal_position	
FROM 
	information_schema.routines
RIGHT JOIN 
	information_schema.parameters 
	ON (routines.specific_name = parameters.specific_name and parameters.ordinal_position IS NOT NULL)	
WHERE 
	routines.specific_schema = $1
ORDER BY 
	routines.routine_name, parameters.ordinal_position;`

	rows, err := db.Query(sqlStmt, schema)
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
		cols := columns[i]

		for n, c := range cols {
			cm[(t.Key + c.Key)] = &columns[i][n]
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
