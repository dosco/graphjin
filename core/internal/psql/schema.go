package psql

import (
	"fmt"
	"strings"

	"github.com/gobuffalo/flect"
)

type DBSchema struct {
	ver int
	t   map[string]*DBTableInfo
	rm  map[string]map[string]*DBRel
	vt  map[string]*VirtualTable
	fm  map[string]*DBFunction
}

type DBTableInfo struct {
	Name       string
	Type       string
	IsSingular bool
	Columns    []DBColumn
	PrimaryCol *DBColumn
	TSVCol     *DBColumn
	ColMap     map[string]*DBColumn
	ColIDMap   map[int16]*DBColumn
	Singular   string
	Plural     string
}

type RelType int

const (
	RelOneToOne RelType = iota + 1
	RelOneToMany
	RelOneToManyThrough
	RelPolymorphic
	RelEmbedded
	RelRemote
)

type DBRel struct {
	Type    RelType
	Through struct {
		Table string
		ColL  string
		ColR  string
	}
	Left struct {
		col   *DBColumn
		Table string
		Col   string
		Array bool
	}
	Right struct {
		col   *DBColumn
		Table string
		Col   string
		Array bool
	}
}

func NewDBSchema(info *DBInfo, aliases map[string][]string) (*DBSchema, error) {
	schema := &DBSchema{
		ver: info.Version,
		t:   make(map[string]*DBTableInfo),
		rm:  make(map[string]map[string]*DBRel),
		vt:  make(map[string]*VirtualTable),
		fm:  make(map[string]*DBFunction, len(info.Functions)),
	}

	for i, t := range info.Tables {
		err := schema.addTable(t, info.Columns[i], aliases)
		if err != nil {
			return nil, err
		}
	}

	if err := schema.virtualRels(info.VTables); err != nil {
		return nil, err
	}

	for i, t := range info.Tables {
		err := schema.firstDegreeRels(t, info.Columns[i])
		if err != nil {
			return nil, err
		}
	}

	for i, t := range info.Tables {
		err := schema.secondDegreeRels(t, info.Columns[i])
		if err != nil {
			return nil, err
		}
	}

	for k, f := range info.Functions {
		if len(f.Params) == 1 {
			schema.fm[strings.ToLower(f.Name)] = &info.Functions[k]
		}
	}

	return schema, nil
}

func (s *DBSchema) addTable(
	t DBTable, cols []DBColumn, aliases map[string][]string) error {

	colmap := make(map[string]*DBColumn, len(cols))
	colidmap := make(map[int16]*DBColumn, len(cols))

	singular := flect.Singularize(t.Key)
	plural := flect.Pluralize(t.Key)

	ts := &DBTableInfo{
		Name:       t.Name,
		Type:       t.Type,
		IsSingular: true,
		Columns:    cols,
		ColMap:     colmap,
		ColIDMap:   colidmap,
		Singular:   singular,
		Plural:     plural,
	}
	s.t[singular] = ts

	tp := &DBTableInfo{
		Name:       t.Name,
		Type:       t.Type,
		IsSingular: false,
		Columns:    cols,
		ColMap:     colmap,
		ColIDMap:   colidmap,
		Singular:   singular,
		Plural:     plural,
	}
	s.t[plural] = tp

	if al, ok := aliases[t.Key]; ok {
		for i := range al {
			k1 := flect.Singularize(al[i])
			s.t[k1] = ts

			k2 := flect.Pluralize(al[i])
			s.t[k2] = tp
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

func (s *DBSchema) virtualRels(vts []VirtualTable) error {
	for _, vt := range vts {
		s.vt[vt.Name] = &vt

		for _, t := range s.t {
			idCol, ok := t.ColMap[vt.IDColumn]
			if !ok {
				continue
			}
			if _, ok = t.ColMap[vt.TypeColumn]; !ok {
				continue
			}

			nt := DBTable{
				ID:   -1,
				Name: vt.Name,
				Key:  strings.ToLower(vt.Name),
				Type: "virtual",
			}

			if err := s.addTable(nt, nil, nil); err != nil {
				return err
			}

			rel := &DBRel{Type: RelPolymorphic}
			rel.Left.col = idCol
			rel.Left.Table = t.Name
			rel.Left.Col = idCol.Name

			rcol := DBColumn{
				Name: vt.FKeyColumn,
				Key:  strings.ToLower(vt.FKeyColumn),
				Type: idCol.Type,
			}

			rel.Right.col = &rcol
			rel.Right.Table = vt.TypeColumn
			rel.Right.Col = rcol.Name

			if err := s.SetRel(vt.Name, t.Name, rel); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *DBSchema) firstDegreeRels(t DBTable, cols []DBColumn) error {
	ct := t.Key
	cti, ok := s.t[ct]
	if !ok {
		return fmt.Errorf("invalid foreign key table '%s'", ct)
	}

	for i := range cols {
		c := cols[i]

		if c.FKeyTable == "" {
			continue
		}

		// Foreign key column name
		ft := strings.ToLower(c.FKeyTable)

		ti, ok := s.t[ft]
		if !ok {
			return fmt.Errorf("invalid foreign key table '%s'", ft)
		}

		// This is an embedded relationship like when a json/jsonb column
		// is exposed as a table
		if c.Name == c.FKeyTable && len(c.FKeyColID) == 0 {
			rel := &DBRel{Type: RelEmbedded}
			rel.Left.col = cti.PrimaryCol
			rel.Left.Table = cti.Name
			rel.Left.Col = cti.PrimaryCol.Name

			rel.Right.col = &c
			rel.Right.Table = ti.Name
			rel.Right.Col = c.Name

			if err := s.SetRel(ft, ct, rel); err != nil {
				return err
			}
			continue
		}

		if len(c.FKeyColID) == 0 {
			continue
		}

		// Foreign key column id
		fcid := c.FKeyColID[0]

		fc, ok := ti.ColIDMap[fcid]
		if !ok {
			return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
				fcid, ti.Name)
		}

		var rel1, rel2 *DBRel

		// One-to-many relation between current table and the
		// table in the foreign key
		if fc.UniqueKey {
			rel1 = &DBRel{Type: RelOneToOne}
		} else {
			rel1 = &DBRel{Type: RelOneToMany}
		}

		rel1.Left.col = &c
		rel1.Left.Table = t.Name
		rel1.Left.Col = c.Name
		rel1.Left.Array = c.Array

		rel1.Right.col = fc
		rel1.Right.Table = c.FKeyTable
		rel1.Right.Col = fc.Name
		rel1.Right.Array = fc.Array

		if err := s.SetRel(ct, ft, rel1); err != nil {
			return err
		}

		// One-to-many reverse relation between the foreign key table and the
		// the current table
		if c.UniqueKey {
			rel2 = &DBRel{Type: RelOneToOne}
		} else {
			rel2 = &DBRel{Type: RelOneToMany}
		}

		rel2.Left.col = fc
		rel2.Left.Table = c.FKeyTable
		rel2.Left.Col = fc.Name
		rel2.Left.Array = fc.Array

		rel2.Right.col = &c
		rel2.Right.Table = t.Name
		rel2.Right.Col = c.Name
		rel2.Right.Array = c.Array

		if err := s.SetRel(ft, ct, rel2); err != nil {
			return err
		}
	}

	return nil
}

func (s *DBSchema) secondDegreeRels(t DBTable, cols []DBColumn) error {
	jcols := make([]DBColumn, 0, len(cols))
	ct := t.Key
	cti, ok := s.t[ct]
	if !ok {
		return fmt.Errorf("invalid foreign key table '%s'", ct)
	}

	for i := range cols {
		c := cols[i]

		if c.FKeyTable == "" {
			continue
		}

		// Foreign key column name
		ft := strings.ToLower(c.FKeyTable)

		ti, ok := s.t[ft]
		if !ok {
			return fmt.Errorf("invalid foreign key table '%s'", ft)
		}

		// This is an embedded relationship like when a json/jsonb column
		// is exposed as a table
		if c.Name == c.FKeyTable && len(c.FKeyColID) == 0 {
			continue
		}

		if len(c.FKeyColID) == 0 {
			continue
		}

		// Foreign key column id
		fcid := c.FKeyColID[0]

		if _, ok := ti.ColIDMap[fcid]; !ok {
			return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
				fcid, ti.Name)
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
	rel1.Through.Table = ti.Name
	rel1.Through.ColL = col1.Name
	rel1.Through.ColR = col2.Name

	rel1.Left.col = fc1
	rel1.Left.Table = col1.FKeyTable
	rel1.Left.Col = fc1.Name

	rel1.Right.col = fc2
	rel1.Right.Table = t2
	rel1.Right.Col = fc2.Name

	if err := s.SetRel(t1, t2, rel1); err != nil {
		return err
	}

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	rel2 := &DBRel{Type: RelOneToManyThrough}
	rel2.Through.Table = ti.Name
	rel2.Through.ColL = col2.Name
	rel2.Through.ColR = col1.Name

	rel2.Left.col = fc2
	rel2.Left.Table = col2.FKeyTable
	rel2.Left.Col = fc2.Name

	rel2.Right.col = fc1
	rel2.Right.Table = t1
	rel2.Right.Col = fc1.Name

	if err := s.SetRel(t2, t1, rel2); err != nil {
		return err
	}

	return nil
}

func (s *DBSchema) GetTableNames() []string {
	var names []string
	for name := range s.t {
		names = append(names, name)
	}
	return names
}

func (s *DBSchema) GetTable(table string) (*DBTableInfo, error) {
	t, ok := s.t[table]
	if !ok {
		return nil, fmt.Errorf("unknown table '%s'", table)
	}
	return t, nil
}

func (s *DBSchema) SetRel(child, parent string, rel *DBRel) error {
	sp := strings.ToLower(flect.Singularize(parent))
	pp := strings.ToLower(flect.Pluralize(parent))

	sc := strings.ToLower(flect.Singularize(child))
	pc := strings.ToLower(flect.Pluralize(child))

	if _, ok := s.rm[sc]; !ok {
		s.rm[sc] = make(map[string]*DBRel)
	}

	if _, ok := s.rm[pc]; !ok {
		s.rm[pc] = make(map[string]*DBRel)
	}

	if _, ok := s.rm[sc][sp]; !ok {
		s.rm[sc][sp] = rel
	}
	if _, ok := s.rm[sc][pp]; !ok {
		s.rm[sc][pp] = rel
	}
	if _, ok := s.rm[pc][sp]; !ok {
		s.rm[pc][sp] = rel
	}
	if _, ok := s.rm[pc][pp]; !ok {
		s.rm[pc][pp] = rel
	}

	return nil
}

func (s *DBSchema) GetRel(child, parent string) (*DBRel, error) {
	rel, ok := s.rm[child][parent]
	if !ok {
		// No relationship found so this time fetch the table info
		// and try again in case child or parent was an alias
		ct, err := s.GetTable(child)
		if err != nil {
			return nil, err
		}
		pt, err := s.GetTable(parent)
		if err != nil {
			return nil, err
		}
		rel, ok = s.rm[ct.Name][pt.Name]
		if !ok {
			return nil, fmt.Errorf("unknown relationship '%s' -> '%s'",
				child, parent)
		}
	}
	return rel, nil
}

func (s *DBSchema) GetFunctions() []*DBFunction {
	var funcs []*DBFunction
	for _, f := range s.fm {
		funcs = append(funcs, f)
	}
	return funcs
}
