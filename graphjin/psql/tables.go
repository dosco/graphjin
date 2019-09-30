package psql

import (
	"context"
	"fmt"
	"strings"

	"github.com/gobuffalo/flect"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
)

type DBTable struct {
	Name string
	Type string
}

func GetTables(dbc *pgxpool.Conn) ([]*DBTable, error) {
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
	AND n.nspname <> 'pg_catalog'
	AND n.nspname <> 'information_schema'
	AND n.nspname !~ '^pg_toast'
AND pg_catalog.pg_table_is_visible(c.oid);`

	var tables []*DBTable

	rows, err := dbc.Query(context.Background(), sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("Error fetching tables: %s", err)
	}
	defer rows.Close()

	for rows.Next() {
		t := DBTable{}
		err = rows.Scan(&t.Name, &t.Type)
		if err != nil {
			return nil, err
		}
		tables = append(tables, &t)
	}

	return tables, nil
}

type DBColumn struct {
	ID         int
	Name       string
	Type       string
	NotNull    bool
	PrimaryKey bool
	UniqueKey  bool
	FKeyTable  string
	FKeyColID  []int
	fKeyColID  pgtype.Int2Array
}

func GetColumns(dbc *pgxpool.Conn, schema, table string) ([]*DBColumn, error) {
	sqlStmt := `
SELECT  
	f.attnum AS id,  
	f.attname AS name,  
	f.attnotnull AS notnull,  
	pg_catalog.format_type(f.atttypid,f.atttypmod) AS type,  
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

	cmap := make(map[int]*DBColumn)

	for rows.Next() {
		c := DBColumn{}
		err = rows.Scan(&c.ID, &c.Name, &c.NotNull, &c.Type, &c.PrimaryKey, &c.UniqueKey,
			&c.FKeyTable, &c.fKeyColID)
		if err != nil {
			return nil, err
		}
		c.fKeyColID.AssignTo(&c.FKeyColID)

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
		} else {
			cmap[c.ID] = &c
		}
	}

	cols := make([]*DBColumn, 0, len(cmap))
	for _, v := range cmap {
		cols = append(cols, v)
	}

	return cols, nil
}

type DBSchema struct {
	t  map[string]*DBTableInfo
	rm map[string]map[string]*DBRel
	al map[string]struct{}
}

type DBTableInfo struct {
	Name        string
	Singular    bool
	PrimaryCol  string
	TSVCol      string
	Columns     map[string]*DBColumn
	ColumnNames []string
}

type RelType int

const (
	RelBelongTo RelType = iota + 1
	RelOneToMany
	RelOneToManyThrough
	RelRemote
)

type DBRel struct {
	Type    RelType
	Through string
	ColT    string
	Col1    string
	Col2    string
}

func NewDBSchema(db *pgxpool.Pool, aliases map[string][]string) (*DBSchema, error) {
	schema := &DBSchema{
		t:  make(map[string]*DBTableInfo),
		rm: make(map[string]map[string]*DBRel),
		al: make(map[string]struct{}),
	}

	dbc, err := db.Acquire(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error acquiring connection from pool")
	}
	defer dbc.Release()

	tables, err := GetTables(dbc)
	if err != nil {
		return nil, err
	}

	for _, t := range tables {
		cols, err := GetColumns(dbc, "public", t.Name)
		if err != nil {
			return nil, err
		}

		schema.updateSchema(t, cols, aliases)
	}

	return schema, nil
}

func (s *DBSchema) updateSchema(
	t *DBTable,
	cols []*DBColumn,
	aliases map[string][]string) {

	// Foreign key columns in current table
	colByID := make(map[int]*DBColumn)
	columns := make(map[string]*DBColumn, len(cols))
	colNames := make([]string, 0, len(cols))

	for i := range cols {
		c := cols[i]
		name := strings.ToLower(c.Name)
		columns[name] = c
		colNames = append(colNames, name)
		colByID[c.ID] = c
	}

	singular := strings.ToLower(flect.Singularize(t.Name))
	s.t[singular] = &DBTableInfo{
		Name:        t.Name,
		Singular:    true,
		Columns:     columns,
		ColumnNames: colNames,
	}

	plural := strings.ToLower(flect.Pluralize(t.Name))
	s.t[plural] = &DBTableInfo{
		Name:        t.Name,
		Singular:    false,
		Columns:     columns,
		ColumnNames: colNames,
	}

	ct := strings.ToLower(t.Name)

	if al, ok := aliases[ct]; ok {
		for i := range al {
			k1 := flect.Singularize(al[i])
			s.t[k1] = s.t[singular]

			k2 := flect.Pluralize(al[i])
			s.t[k2] = s.t[plural]

			s.al[k1] = struct{}{}
			s.al[k2] = struct{}{}
		}
	}

	jcols := make([]*DBColumn, 0, len(cols))

	for _, c := range cols {
		switch {
		case c.Type == "tsvector":
			s.t[singular].TSVCol = c.Name
			s.t[plural].TSVCol = c.Name

		case c.PrimaryKey:
			s.t[singular].PrimaryCol = c.Name
			s.t[plural].PrimaryCol = c.Name

		case len(c.FKeyTable) != 0:
			if len(c.FKeyColID) == 0 {
				continue
			}

			// Foreign key column name
			ft := strings.ToLower(c.FKeyTable)
			fc, ok := colByID[c.FKeyColID[0]]
			if !ok {
				continue
			}

			// Belongs-to relation between current table and the
			// table in the foreign key
			rel1 := &DBRel{RelBelongTo, "", "", c.Name, fc.Name}
			s.SetRel(ct, ft, rel1)

			// One-to-many relation between the foreign key table and the
			// the current table
			rel2 := &DBRel{RelOneToMany, "", "", fc.Name, c.Name}
			s.SetRel(ft, ct, rel2)

			jcols = append(jcols, c)
		}
	}

	// If table contains multiple foreign key columns it's a possible
	// join table for many-to-many relationships or multiple one-to-many
	// relations

	// Below one-to-many relations use the current table as the
	// join table aka through table.
	if len(jcols) > 1 {
		for i := range jcols {
			for n := range jcols {
				if n != i {
					s.updateSchemaOTMT(ct, jcols[i], jcols[n], colByID)
				}
			}
		}
	}
}

func (s *DBSchema) updateSchemaOTMT(
	ct string,
	col1, col2 *DBColumn,
	colByID map[int]*DBColumn) {

	t1 := strings.ToLower(col1.FKeyTable)
	t2 := strings.ToLower(col2.FKeyTable)

	fc1, ok := colByID[col1.FKeyColID[0]]
	if !ok {
		return
	}
	fc2, ok := colByID[col2.FKeyColID[0]]
	if !ok {
		return
	}

	// One-to-many-through relation between 1nd foreign key table and the
	// 2nd foreign key table
	//rel1 := &DBRel{RelOneToManyThrough, ct, fc1.Name, col1.Name}
	rel1 := &DBRel{RelOneToManyThrough, ct, col2.Name, fc2.Name, col1.Name}
	s.SetRel(t1, t2, rel1)

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	//rel2 := &DBRel{RelOneToManyThrough, ct, col2.Name, fc2.Name}
	rel2 := &DBRel{RelOneToManyThrough, ct, col1.Name, fc1.Name, col2.Name}
	s.SetRel(t2, t1, rel2)
}

func (s *DBSchema) GetTable(table string) (*DBTableInfo, error) {
	t, ok := s.t[table]
	if !ok {
		return nil, fmt.Errorf("unknown table '%s'", table)
	}
	return t, nil
}

func (s *DBSchema) SetRel(child, parent string, rel *DBRel) error {
	sc := strings.ToLower(flect.Singularize(child))
	pc := strings.ToLower(flect.Pluralize(child))

	if _, ok := s.rm[sc]; !ok {
		s.rm[sc] = make(map[string]*DBRel)
	}

	if _, ok := s.rm[pc]; !ok {
		s.rm[pc] = make(map[string]*DBRel)
	}

	sp := strings.ToLower(flect.Singularize(parent))
	pp := strings.ToLower(flect.Pluralize(parent))

	s.rm[sc][sp] = rel
	s.rm[sc][pp] = rel
	s.rm[pc][sp] = rel
	s.rm[pc][pp] = rel

	return nil
}

func (s *DBSchema) GetRel(child, parent string) (*DBRel, error) {
	rel, ok := s.rm[child][parent]
	if !ok {
		return nil, fmt.Errorf("unknown relationship '%s' -> '%s'",
			child, parent)
	}
	return rel, nil
}

func (s *DBSchema) IsAlias(name string) bool {
	_, ok := s.al[name]
	return ok
}
