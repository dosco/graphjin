package psql

import (
	"fmt"
	"strings"

	"github.com/gobuffalo/flect"
	"github.com/jackc/pgx/v4/pgxpool"
)

type DBSchema struct {
	ver int
	t   map[string]*DBTableInfo
	rm  map[string]map[string]*DBRel
}

type DBTableInfo struct {
	Name       string
	Singular   bool
	Columns    []DBColumn
	PrimaryCol *DBColumn
	TSVCol     *DBColumn
	ColMap     map[string]*DBColumn
	ColIDMap   map[int16]*DBColumn
}

type RelType int

const (
	RelOneToMany RelType = iota + 1
	RelOneToManyThrough
	RelRemote
)

type DBRel struct {
	Type    RelType
	Through string
	ColT    string
	Left    struct {
		Col   string
		Array bool
	}
	Right struct {
		Col   string
		Array bool
	}
}

func NewDBSchema(db *pgxpool.Pool,
	info *DBInfo, aliases map[string][]string) (*DBSchema, error) {

	schema := &DBSchema{
		t:  make(map[string]*DBTableInfo),
		rm: make(map[string]map[string]*DBRel),
	}

	for i, t := range info.Tables {
		err := schema.addTable(t, info.Columns[i], aliases)
		if err != nil {
			return nil, err
		}
	}

	for i, t := range info.Tables {
		err := schema.updateRelationships(t, info.Columns[i])
		if err != nil {
			return nil, err
		}
	}

	return schema, nil
}

func (s *DBSchema) addTable(
	t DBTable, cols []DBColumn, aliases map[string][]string) error {

	colmap := make(map[string]*DBColumn, len(cols))
	colidmap := make(map[int16]*DBColumn, len(cols))

	singular := flect.Singularize(t.Key)
	s.t[singular] = &DBTableInfo{
		Name:     t.Name,
		Singular: true,
		Columns:  cols,
		ColMap:   colmap,
		ColIDMap: colidmap,
	}

	plural := flect.Pluralize(t.Key)
	s.t[plural] = &DBTableInfo{
		Name:     t.Name,
		Singular: false,
		Columns:  cols,
		ColMap:   colmap,
		ColIDMap: colidmap,
	}

	if al, ok := aliases[t.Key]; ok {
		for i := range al {
			k1 := flect.Singularize(al[i])
			s.t[k1] = s.t[singular]

			k2 := flect.Pluralize(al[i])
			s.t[k2] = s.t[plural]
		}
	}

	for i := range cols {
		c := &cols[i]

		switch {
		case c.Type == "tsvector":
			s.t[singular].TSVCol = c
			s.t[plural].TSVCol = c

		case c.PrimaryKey:
			s.t[singular].PrimaryCol = c
			s.t[plural].PrimaryCol = c
		}

		colmap[c.Key] = c
		colidmap[c.ID] = c
	}

	return nil
}

func (s *DBSchema) updateRelationships(t DBTable, cols []DBColumn) error {
	jcols := make([]DBColumn, 0, len(cols))
	ct := t.Key
	cti, ok := s.t[ct]
	if !ok {
		return fmt.Errorf("invalid foreign key table '%s'", ct)
	}

	for _, c := range cols {
		if len(c.FKeyTable) == 0 || len(c.FKeyColID) == 0 {
			continue
		}

		// Foreign key column name
		ft := strings.ToLower(c.FKeyTable)
		fcid := c.FKeyColID[0]

		ti, ok := s.t[ft]
		if !ok {
			return fmt.Errorf("invalid foreign key table '%s'", ft)
		}

		fc, ok := ti.ColIDMap[fcid]
		if !ok {
			return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
				fcid, ti.Name)
		}

		// One-to-many relation between current table and the
		// table in the foreign key
		rel1 := &DBRel{Type: RelOneToMany}
		rel1.Left.Col = c.Name
		rel1.Left.Array = c.Array
		rel1.Right.Col = fc.Name
		rel1.Right.Array = fc.Array

		if err := s.SetRel(ct, ft, rel1); err != nil {
			return err
		}

		// One-to-many reverse relation between the foreign key table and the
		// the current table
		rel2 := &DBRel{Type: RelOneToMany}
		rel2.Left.Col = fc.Name
		rel2.Left.Array = fc.Array
		rel2.Right.Col = c.Name
		rel2.Right.Array = c.Array

		if err := s.SetRel(ft, ct, rel2); err != nil {
			return err
		}

		jcols = append(jcols, c)
	}

	// If table contains multiple foreign key columns it's a possible
	// join table for many-to-many relationships or multiple one-to-many
	// relations

	// Below one-to-many relations use the current table as the
	// join table aka through table.
	if len(jcols) > 1 {
		for i := range jcols {
			for n := range jcols {
				if n == i {
					continue
				}
				err := s.updateSchemaOTMT(cti, jcols[i], jcols[n])
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (s *DBSchema) updateSchemaOTMT(
	ti *DBTableInfo, col1, col2 DBColumn) error {

	t1 := strings.ToLower(col1.FKeyTable)
	t2 := strings.ToLower(col2.FKeyTable)

	fc1, ok := s.t[t1].ColIDMap[col1.FKeyColID[0]]
	if !ok {
		return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
			col1.FKeyColID[0], ti.Name)
	}
	fc2, ok := s.t[t2].ColIDMap[col2.FKeyColID[0]]
	if !ok {
		return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
			col2.FKeyColID[0], ti.Name)
	}

	// One-to-many-through relation between 1nd foreign key table and the
	// 2nd foreign key table
	rel1 := &DBRel{Type: RelOneToManyThrough}
	rel1.Through = ti.Name
	rel1.ColT = col2.Name
	rel1.Left.Col = fc2.Name
	rel1.Right.Col = col1.Name
	if err := s.SetRel(t1, t2, rel1); err != nil {
		return err
	}

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	rel2 := &DBRel{Type: RelOneToManyThrough}
	rel2.Through = ti.Name
	rel2.ColT = col1.Name
	rel2.Left.Col = fc1.Name
	rel2.Right.Col = col2.Name
	if err := s.SetRel(t2, t1, rel2); err != nil {
		return err
	}

	return nil
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
