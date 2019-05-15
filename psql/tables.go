package psql

import (
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/go-pg/pg"
)

type DBSchema struct {
	Tables map[string]*DBTableInfo
	RelMap map[uint64]*DBRel
}

type DBTableInfo struct {
	Name       string
	PrimaryCol string
	TSVCol     string
	Columns    map[string]*DBColumn
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

func NewDBSchema(db *pg.DB) (*DBSchema, error) {
	schema := &DBSchema{
		Tables: make(map[string]*DBTableInfo),
		RelMap: make(map[uint64]*DBRel),
	}

	tables, err := GetTables(db)
	if err != nil {
		return nil, err
	}

	for _, t := range tables {
		cols, err := GetColumns(db, "public", t.Name)
		if err != nil {
			return nil, err
		}

		schema.updateSchema(t, cols)
	}

	return schema, nil
}

func (s *DBSchema) updateSchema(t *DBTable, cols []*DBColumn) {
	// Current table
	ti := &DBTableInfo{
		Name:    t.Name,
		Columns: make(map[string]*DBColumn, len(cols)),
	}

	// Foreign key columns in current table
	var jcols []*DBColumn
	colByID := make(map[int]*DBColumn)

	for i := range cols {
		c := cols[i]
		ti.Columns[strings.ToLower(c.Name)] = cols[i]
		colByID[c.ID] = cols[i]
	}

	ct := strings.ToLower(t.Name)
	s.Tables[ct] = ti

	h := xxhash.New()

	for _, c := range cols {
		switch {
		case c.Type == "tsvector":
			s.Tables[ct].TSVCol = c.Name

		case c.PrimaryKey:
			s.Tables[ct].PrimaryCol = c.Name

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
			s.RelMap[relID(h, ct, ft)] = rel1

			// One-to-many relation between the foreign key table and the
			// the current table
			rel2 := &DBRel{RelOneToMany, "", "", fc.Name, c.Name}
			s.RelMap[relID(h, ft, ct)] = rel2

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
					s.updateSchemaOTMT(h, ct, jcols[i], jcols[n], colByID)
				}
			}
		}
	}
}

func (s *DBSchema) updateSchemaOTMT(
	h *xxhash.Digest,
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
	s.RelMap[relID(h, t1, t2)] = rel1

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	//rel2 := &DBRel{RelOneToManyThrough, ct, col2.Name, fc2.Name}
	rel2 := &DBRel{RelOneToManyThrough, ct, col1.Name, fc1.Name, col2.Name}
	s.RelMap[relID(h, t2, t1)] = rel2
}

type DBTable struct {
	Name string `sql:"name"`
	Type string `sql:"type"`
}

func GetTables(db *pg.DB) ([]*DBTable, error) {
	sqlStmt := `
	SELECT
  c.relname as "name",
  CASE c.relkind WHEN 'r' THEN 'table'
  WHEN 'v' THEN 'view'
  WHEN 'm' THEN 'materialized view'
  WHEN 'f' THEN 'foreign table' END as "type"
FROM pg_catalog.pg_class c
     LEFT JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','v','m','f','')
      AND n.nspname <> 'pg_catalog'
      AND n.nspname <> 'information_schema'
      AND n.nspname !~ '^pg_toast'
  AND pg_catalog.pg_table_is_visible(c.oid);
	`

	var t []*DBTable
	_, err := db.Query(&t, sqlStmt)

	if err != nil {
		return nil, fmt.Errorf("Error fetching tables: %s", err)
	}

	return t, nil
}

type DBColumn struct {
	ID         int    `sql:"id"`
	Name       string `sql:"name"`
	Type       string `sql:"type"`
	NotNull    bool   `sql:"notnull"`
	PrimaryKey bool   `sql:"primarykey"`
	Uniquekey  bool   `sql:"uniquekey"`
	FKeyTable  string `sql:"foreignkey"`
	FKeyColID  []int  `sql:"foreignkey_fieldnum,array"`
}

func GetColumns(db *pg.DB, schema, table string) ([]*DBColumn, error) {
	sqlStmt := `
	SELECT  
    f.attnum AS id,  
    f.attname AS name,  
    f.attnotnull AS notnull,  
    pg_catalog.format_type(f.atttypid,f.atttypmod) AS type,  
    CASE  
        WHEN p.contype = 'p' THEN 't'  
        ELSE 'f'  
    END AS primarykey,  
    CASE  
        WHEN p.contype = 'u' THEN 't'  
        ELSE 'f'
    END AS uniquekey,
    CASE
        WHEN p.contype = 'f' THEN g.relname
    END AS foreignkey,
    CASE
        WHEN p.contype = 'f' THEN p.confkey
    END AS foreignkey_fieldnum
FROM pg_attribute f  
    JOIN pg_class c ON c.oid = f.attrelid  
    JOIN pg_type t ON t.oid = f.atttypid  
    LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum  
    LEFT JOIN pg_namespace n ON n.oid = c.relnamespace  
    LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)  
    LEFT JOIN pg_class AS g ON p.confrelid = g.oid  
WHERE c.relkind = 'r'::char  
    AND n.nspname = $1  -- Replace with Schema name  
    AND c.relname = $2  -- Replace with table name  
    AND f.attnum > 0 ORDER BY id;
	`

	stmt, err := db.Prepare(sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}

	var t []*DBColumn
	_, err = stmt.Query(&t, schema, table)
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %s", err)
	}

	return t, nil
}

func (s *DBSchema) GetTable(table string) (*DBTableInfo, error) {
	t, ok := s.Tables[table]
	if !ok {
		return nil, fmt.Errorf("unknown table '%s'", table)
	}
	return t, nil
}

func relID(h *xxhash.Digest, child, parent string) uint64 {
	h.WriteString(child)
	h.WriteString(parent)
	v := h.Sum64()
	h.Reset()
	return v
}
