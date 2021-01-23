//go:generate stringer -type=RelType -output=./gen_string.go

package sdata

import (
	"fmt"
	"strings"

	"github.com/gobuffalo/flect"
)

type aliasKey struct {
	name   string
	parent string
}

type DBSchema struct {
	ver int
	typ string
	t   map[string]DBTableInfo
	at  map[aliasKey]string
	rm  map[string][]DBRel
	vt  map[string]VirtualTable
	fm  map[string]DBFunction
}

type DBTableInfo struct {
	Name       string
	Type       string
	IsSingular bool
	IsAlias    bool
	Columns    []DBColumn
	PrimaryCol DBColumn
	FullText   []DBColumn
	Singular   string
	Plural     string
	Blocked    bool
	Schema     *DBSchema

	colMap map[string]int
}

type RelType int

const (
	RelNone RelType = iota
	RelOneToOne
	RelOneToMany
	RelOneToManyThrough
	RelPolymorphic
	RelRecursive
	RelEmbedded
	RelRemote
	RelSkip
)

type DBRelThrough struct {
	Ti   DBTableInfo
	ColL DBColumn
	ColR DBColumn
}

type DBRelLeft struct {
	Ti  DBTableInfo
	Col DBColumn
}

type DBRelRight struct {
	VTable string
	Ti     DBTableInfo
	Col    DBColumn
}

type DBRel struct {
	Type    RelType
	Through DBRelThrough
	Left    DBRelLeft
	Right   DBRelRight
}

func NewDBSchema(info *DBInfo, aliases map[string][]string) (*DBSchema, error) {
	schema := &DBSchema{
		ver: info.Version,
		typ: info.Type,
		t:   make(map[string]DBTableInfo),
		at:  make(map[aliasKey]string),
		rm:  make(map[string][]DBRel),
		vt:  make(map[string]VirtualTable),
		fm:  make(map[string]DBFunction),
	}

	for i, t := range info.Tables {
		err := schema.addTableInfo(t, info.Columns[i], aliases)
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
			schema.fm[strings.ToLower(f.Name)] = info.Functions[k]
		}
	}

	return schema, nil
}

func (s *DBSchema) addTableInfo(
	t DBTable, cols []DBColumn, aliases map[string][]string) error {

	colmap := make(map[string]int, len(cols))

	singular := flect.Singularize(t.Key)
	plural := flect.Pluralize(t.Key)

	ti := DBTableInfo{
		Name:     t.Name,
		Type:     t.Type,
		Columns:  cols,
		Singular: singular,
		Plural:   plural,
		Blocked:  t.Blocked,
		Schema:   s,
		colMap:   colmap,
	}

	for i := range cols {
		c := &cols[i]
		c.Table = t.Name

		switch {
		case c.FullText:
			ti.FullText = append(ti.FullText, cols[i])

		case c.PrimaryKey:
			ti.PrimaryCol = cols[i]
		}

		colmap[c.Key] = i
	}

	ti.IsSingular = true
	s.t[singular] = ti

	ti.IsSingular = false
	s.t[plural] = ti

	if al, ok := aliases[t.Key]; ok {
		for i := range al {
			ti1 := ti
			ti1.Singular = flect.Singularize(al[i])
			ti1.Plural = flect.Pluralize(al[i])

			ti1.IsSingular = true
			s.t[ti1.Singular] = ti1

			ti1.IsSingular = false
			s.t[ti1.Plural] = ti1
		}
	}

	return nil
}

func (s *DBSchema) virtualRels(vts []VirtualTable) error {
	for _, vt := range vts {
		s.vt[vt.Name] = vt

		for _, t := range s.t {
			idCol, ok := t.getColumn(vt.IDColumn)
			if !ok {
				continue
			}

			if _, ok := t.getColumn(vt.TypeColumn); !ok {
				continue
			}

			nt := DBTable{
				Name: vt.Name,
				Key:  strings.ToLower(vt.Name),
				Type: "virtual",
			}

			if err := s.addTableInfo(nt, nil, nil); err != nil {
				return err
			}

			rel := DBRel{Type: RelPolymorphic}
			rel.Left.Ti = t
			rel.Left.Col = idCol

			rcol := DBColumn{
				Name: vt.FKeyColumn,
				Key:  strings.ToLower(vt.FKeyColumn),
				Type: idCol.Type,
			}

			rel.Right.VTable = vt.TypeColumn
			rel.Right.Ti = t
			rel.Right.Col = rcol

			if err := s.SetRel(vt.Name, t.Name, rel, false); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *DBSchema) firstDegreeRels(t DBTable, cols []DBColumn) error {
	cti, ok := s.t[t.Name]
	if !ok {
		return fmt.Errorf("table not found %s", t.Name)
	}

	for i := range cols {
		c := cols[i]

		if c.FKeyTable == "" {
			continue
		}

		fti, ok := s.t[c.FKeyTable]
		if !ok {
			return fmt.Errorf("invalid foreign key table '%s'", c.FKeyTable)
		}

		pn1 := c.FKeyTable
		pn2 := getRelName(c.Name)

		// This is an embedded relationship like when a json/jsonb column
		// is exposed as a table
		if c.Name == c.FKeyTable && c.FKeyCol == "" {
			rel := DBRel{Type: RelEmbedded}
			rel.Left.Col = cti.PrimaryCol
			rel.Right.Col = c

			if err := s.SetRel(pn2, cti.Name, rel, true); err != nil {
				return err
			}
			continue
		}

		if c.FKeyCol == "" {
			continue
		}

		fc, ok := fti.getColumn(c.FKeyCol)
		if !ok {
			return fmt.Errorf("foreign key column '%s.%s'",
				c.FKeyTable, c.FKeyCol)
		}

		rel1 := DBRel{}

		// One-to-many relation between current table and the
		// table in the foreign key
		switch {
		case cti.Name == c.FKeyTable:
			rel1.Type = RelRecursive
			rel1.Right.VTable = "_rcte_" + t.Name
		case fc.UniqueKey:
			rel1.Type = RelOneToOne
		default:
			rel1.Type = RelOneToMany
		}

		rel1.Left.Ti = cti
		rel1.Left.Col = c
		rel1.Right.Ti = fti
		rel1.Right.Col = fc

		if err := s.SetRel(cti.Name, pn1, rel1, false); err != nil {
			return err
		}

		if cti.Name == c.FKeyTable {
			continue
		}

		rel2 := DBRel{}

		// One-to-many reverse relation between the foreign key table and the
		// the current table
		if c.UniqueKey {
			rel2.Type = RelOneToOne
		} else {
			rel2.Type = RelOneToMany
		}

		rel2.Left.Ti = fti
		rel2.Left.Col = fc
		rel2.Right.Ti = cti
		rel2.Right.Col = c

		if err := s.SetRel(pn1, cti.Name, rel2, false); err != nil {
			return err
		}
		if err := s.SetRel(pn2, cti.Name, rel2, true); err != nil {
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
		return fmt.Errorf("table not found: %s", ct)
	}

	for _, c := range cols {
		if c.FKeyTable == "" {
			continue
		}

		fti, ok := s.t[c.FKeyTable]
		if !ok {
			return fmt.Errorf("foreign key table not found: %s", c.FKeyTable)
		}

		// This is an embedded relationship like when a json/jsonb column
		// is exposed as a table so skip
		if c.Name == c.FKeyTable && c.FKeyCol == "" {
			continue
		}

		if c.FKeyCol == "" {
			continue
		}

		if _, ok := fti.getColumn(c.FKeyCol); !ok {
			return fmt.Errorf("foreign key column not found: %s.%s",
				c.FKeyTable, c.FKeyCol)
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
	ti DBTableInfo, col1, col2 DBColumn) error {

	ft1 := strings.ToLower(col1.FKeyTable)
	ft2 := strings.ToLower(col2.FKeyTable)

	if ft1 == ft2 {
		return nil
	}

	cn1 := getRelName(col1.Name)
	cn2 := getRelName(col2.Name)

	fti1, ok := s.t[ft1]
	if !ok {
		return fmt.Errorf("foreign key table not found: %s", ft1)
	}

	fc1, ok := fti1.getColumn(col1.FKeyCol)
	if !ok {
		return fmt.Errorf("foreign key column not found: %s.%s",
			ft1, col1.FKeyCol)
	}

	fti2, ok := s.t[ft2]
	if !ok {
		return fmt.Errorf("foreign key table not found: %s", ft2)
	}

	fc2, ok := fti2.getColumn(col2.FKeyCol)
	if !ok {
		return fmt.Errorf("foreign key column not found: %s.%s",
			ft2, col2.FKeyCol)
	}

	// One-to-many-through relation between 1nd foreign key table and the
	// 2nd foreign key table
	rel1 := DBRel{Type: RelOneToManyThrough}
	rel1.Through.Ti = ti
	rel1.Through.ColL = col1
	rel1.Through.ColR = col2

	rel1.Left.Ti = fti1
	rel1.Left.Col = fc1
	rel1.Right.Ti = fti2
	rel1.Right.Col = fc2

	if err := s.SetRel(ft1, ft2, rel1, false); err != nil {
		return err
	}
	if err := s.SetRel(cn1, cn2, rel1, true); err != nil {
		return err
	}

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	rel2 := DBRel{Type: RelOneToManyThrough}
	rel1.Through.Ti = ti
	rel2.Through.ColL = col2
	rel2.Through.ColR = col1

	rel2.Left.Ti = fti2
	rel2.Left.Col = fc2
	rel2.Right.Ti = fti1
	rel2.Right.Col = fc1

	if err := s.SetRel(ft2, ft1, rel2, false); err != nil {
		return err
	}
	if err := s.SetRel(cn2, cn1, rel2, true); err != nil {
		return err
	}

	return nil
}

// func (s *DBSchema) addAlias(name, parent string, ti DBTableInfo) {
// 	if name == ti.Plural || name == ti.Singular {
// 		return
// 	}

// 	ns := strings.ToLower(flect.Singularize(name))
// 	np := strings.ToLower(flect.Pluralize(name))

// 	if ns != np {
// 		s.at[aliasKey{ns, parent}] = ti.Singular
// 		s.at[aliasKey{np, parent}] = ti.Plural
// 	} else {
// 		s.at[aliasKey{np, parent}] = ti.Plural
// 	}
// }

func (s *DBSchema) GetTableNames() []string {
	var names []string
	for name := range s.t {
		names = append(names, name)
	}
	return names
}

func (s *DBSchema) GetAliases(parent string) []string {
	var names []string
	for ak := range s.at {
		if ak.parent == parent {
			names = append(names, ak.name)
		}
	}
	return names
}

func (s *DBSchema) GetAliasTable(name, parent string) (string, bool) {
	v, ok := s.at[aliasKey{name, parent}]
	return v, ok
}

func (s *DBSchema) getTableInfo(name, parent string, blocking bool) (DBTableInfo, error) {
	t, ok := s.t[name]
	if ok {
		if blocking && t.Blocked {
			return t, fmt.Errorf("table: '%s' (%s) blocked", t.Name, name)
		}
		return t, nil
	}

	if parent != "" {
		at, ok := s.at[aliasKey{name, parent}]
		if ok {
			t, ok := s.t[at]
			if ok {
				if blocking && t.Blocked {
					return t, fmt.Errorf("table blocked: %s (alias: %s, parent: %s)", t.Name, name, parent)
				}
				t.IsAlias = true
				return t, nil
			}
		}
	}
	return t, fmt.Errorf("table not found: %s (parent: %s)", name, parent)
}

func (s *DBSchema) GetTableInfo(name, parent string) (DBTableInfo, error) {
	return s.getTableInfo(name, parent, false)
}

func (s *DBSchema) GetTableInfoB(name, parent string) (DBTableInfo, error) {
	return s.getTableInfo(name, parent, true)

}

func (s *DBSchema) SetRel(child, parent string, rel DBRel, alias bool) error {
	// if ok, err := s.relExists(child, parent); ok {
	// 	return nil
	// } else if err != nil {
	// 	return err
	// }

	sp := strings.ToLower(flect.Singularize(parent))
	pp := strings.ToLower(flect.Pluralize(parent))

	sc := strings.ToLower(flect.Singularize(child))
	pc := strings.ToLower(flect.Pluralize(child))

	s.rm[(sc + sp)] = append(s.rm[(sc+sp)], rel)
	s.rm[(sc + pp)] = append(s.rm[(sc+pp)], rel)
	s.rm[(pc + sp)] = append(s.rm[(pc+sp)], rel)
	s.rm[(pc + pp)] = append(s.rm[(pc+pp)], rel)

	// Todo: Maybe a graph ds would be better
	// s.rm[(sp + sc)] = append(s.rm[(sp+sc)], rel)
	// s.rm[(pp + sc)] = append(s.rm[(pp+sc)], rel)
	// s.rm[(sp + pc)] = append(s.rm[(sp+pc)], rel)
	// s.rm[(pp + pc)] = append(s.rm[(pp+pc)], rel)

	if alias && (sc != rel.Left.Ti.Singular || pc != rel.Left.Ti.Plural) {
		s.at[aliasKey{sc, sp}] = rel.Left.Ti.Singular
		s.at[aliasKey{sc, pp}] = rel.Left.Ti.Singular
		s.at[aliasKey{pc, sp}] = rel.Left.Ti.Plural
		s.at[aliasKey{pc, pp}] = rel.Left.Ti.Plural
	}

	return nil
}

func (s *DBSchema) GetRel(child, parent, through string) (DBRel, error) {
	var rel DBRel

	rels, ok := s.rm[(child + parent)]
	if !ok || len(rels) == 0 {
		return rel, fmt.Errorf("relationship: '%s' -> '%s' not found",
			child, parent)
	}

	if len(through) != 0 {
		for _, v := range rels {
			if v.Through.ColL.Table == through {
				return v, nil
			}
		}
	}

	return rels[0], nil
}

func (ti *DBTableInfo) ColumnExists(name string) bool {
	_, ok := ti.colMap[name]
	return ok
}

func (ti *DBTableInfo) getColumn(name string) (DBColumn, bool) {
	var c DBColumn
	if i, ok := ti.colMap[name]; ok {
		return ti.Columns[i], true
	}
	return c, false
}

func (ti *DBTableInfo) GetColumn(name string) (DBColumn, error) {
	c, ok := ti.getColumn(name)
	if ok {
		return c, nil
	}
	return c, fmt.Errorf("column: '%s.%s' not found", ti.Name, name)
}

func (ti *DBTableInfo) GetColumnB(name string) (DBColumn, error) {
	c, err := ti.GetColumn(name)
	if err != nil {
		return c, err
	}
	if c.Blocked {
		return c, fmt.Errorf("column: '%s.%s' blocked", ti.Name, name)
	}
	return c, nil
}

func (s *DBSchema) GetFunctions() map[string]DBFunction {
	return s.fm
}

func getRelName(colName string) string {
	cn := strings.ToLower(colName)

	if strings.HasSuffix(cn, "_id") {
		return colName[:len(colName)-3]
	}

	if strings.HasSuffix(cn, "_ids") {
		return colName[:len(colName)-4]
	}

	if strings.HasPrefix(cn, "id_") {
		return colName[3:]
	}

	if strings.HasPrefix(cn, "ids_") {
		return colName[4:]
	}

	return colName
}

func (s *DBSchema) Type() string {
	return s.typ
}

func (s *DBSchema) DBVersion() int {
	return s.ver
}
