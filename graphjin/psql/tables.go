package psql

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
)

type DBInfo struct {
	Version int
	Tables  []DBTable
	Columns [][]DBColumn
	colmap  map[string]map[string]*DBColumn
}

func GetDBInfo(db *pgxpool.Pool) (*DBInfo, error) {
	di := &DBInfo{}

	dbc, err := db.Acquire(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error acquiring connection from pool: %w", err)
	}
	defer dbc.Release()

	var version string

	err = dbc.QueryRow(context.Background(), `SHOW server_version_num`).Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("error fetching version: %w", err)
	}

	di.Version, err = strconv.Atoi(version)
	if err != nil {
		return nil, err
	}

	di.Tables, err = GetTables(dbc)
	if err != nil {
		return nil, err
	}

	di.colmap = make(map[string]map[string]*DBColumn, len(di.Tables))

	for i, t := range di.Tables {
		cols, err := GetColumns(dbc, "public", t.Name)
		if err != nil {
			return nil, err
		}

		di.Columns = append(di.Columns, cols)
		di.colmap[t.Key] = make(map[string]*DBColumn, len(cols))

		for n, c := range di.Columns[i] {
			di.colmap[t.Key][c.Key] = &di.Columns[i][n]
		}
	}

	return di, nil
}

func (di *DBInfo) GetColumn(table, column string) (*DBColumn, bool) {
	v, ok := di.colmap[strings.ToLower(table)][strings.ToLower(column)]
	return v, ok
}

type DBTable struct {
	ID   int
	Name string
	Key  string
	Type string
}

func GetTables(dbc *pgxpool.Conn) ([]DBTable, error) {
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
	AND n.nspname <> ('pg_catalog')
	AND n.nspname <> ('information_schema')
	AND n.nspname !~ ('^pg_toast')
AND pg_catalog.pg_table_is_visible(c.oid);`

	var tables []DBTable

	rows, err := dbc.Query(context.Background(), sqlStmt)
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
	FKeyTable  string
	FKeyColID  []int16
	fKeyColID  pgtype.Int2Array
}

func GetColumns(dbc *pgxpool.Conn, schema, table string) ([]DBColumn, error) {
	sqlStmt := `
SELECT  
	f.attnum AS id,  
	f.attname AS name,  
	f.attnotnull AS notnull,  
	pg_catalog.format_type(f.atttypid,f.atttypmod) AS type,  
	CASE
	 WHEN f.attndims != 0 THEN true
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
		WHEN p.contype = ('f'::char) THEN p.confkey
		ELSE ARRAY[]::int2[]
	END AS foreignkey_fieldnum
FROM pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid  
	LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum  
	LEFT JOIN pg_namespace n ON n.oid = c.relnamespace  
	LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)  
	LEFT JOIN pg_class AS g ON p.confrelid = g.oid  
WHERE c.relkind = ('r'::char)
	AND n.nspname = $1  -- Replace with Schema name  
	AND c.relname = $2  -- Replace with table name  
	AND f.attnum > 0
	AND f.attisdropped = false
ORDER BY id;`

	rows, err := dbc.Query(context.Background(), sqlStmt, schema, table)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}
	defer rows.Close()

	cmap := make(map[int16]DBColumn)

	for rows.Next() {
		c := DBColumn{}
		err = rows.Scan(&c.ID, &c.Name, &c.NotNull, &c.Type, &c.Array, &c.PrimaryKey, &c.UniqueKey, &c.FKeyTable, &c.fKeyColID)
		if err != nil {
			return nil, err
		}

		if v, ok := cmap[c.ID]; ok {
			if c.PrimaryKey {
				v.PrimaryKey = true
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
		} else {
			err := c.fKeyColID.AssignTo(&c.FKeyColID)
			if err != nil {
				return nil, err
			}
			c.Key = strings.ToLower(c.Name)
			cmap[c.ID] = c
		}
	}

	cols := make([]DBColumn, 0, len(cmap))
	for _, v := range cmap {
		cols = append(cols, v)
	}

	return cols, nil
}
