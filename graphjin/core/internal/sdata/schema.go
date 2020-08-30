//go:generate stringer -type=RelType -output=./gen_string.go

package sdata

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
	PrimaryCol DBColumn
	TSVCol     DBColumn
	Singular   string
	Plural     string
	Blocked    bool
	Schema     *DBSchema

	fkMultiRef map[string]int
	colMap     map[string]int
	colIDMap   map[int16]int
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
)

type DBRel struct {
	Type    RelType
	Through struct {
		ColL DBColumn
		ColR DBColumn
	}
	Left struct {
		Col DBColumn
	}
	Right struct {
		VTable string
		Col    DBColumn
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
		err := schema.addTableInfo(t, info.Columns[i], aliases)
		if err != nil {
			return nil, err
		}
	}

	for _, t := range schema.t {
		err := schema.addMultiRefs(t)
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

func (s *DBSchema) addTableInfo(
	t DBTable, cols []DBColumn, aliases map[string][]string) error {

	colmap := make(map[string]int, len(cols))
	colidmap := make(map[int16]int, len(cols))
	fkMultiRef := make(map[string]int)

	singular := flect.Singularize(t.Key)
	plural := flect.Pluralize(t.Key)

	ti := DBTableInfo{
		Name:       t.Name,
		Type:       t.Type,
		Columns:    cols,
		Singular:   singular,
		Plural:     plural,
		Blocked:    t.Blocked,
		Schema:     s,
		fkMultiRef: fkMultiRef,
		colMap:     colmap,
		colIDMap:   colidmap,
	}

	for i := range cols {
		c := &cols[i]
		c.Table = t.Name
		c.Ti = &ti

		if c.FKeyTable != "" {
			if _, ok := fkMultiRef[c.FKeyTable]; ok {
				fkMultiRef[c.FKeyTable]++
			} else {
				fkMultiRef[c.FKeyTable] = 1
			}
		}

		switch {
		case c.Type == "tsvector":
			ti.TSVCol = cols[i]

		case c.PrimaryKey:
			ti.PrimaryCol = cols[i]
		}

		colmap[c.Key] = i
		colidmap[c.ID] = i
	}

	s.t[singular] = &ti
	s.t[plural] = &ti

	if al, ok := aliases[t.Key]; ok {
		for i := range al {
			ti1 := ti
			ti1.Singular = flect.Singularize(al[i])
			ti1.Plural = flect.Pluralize(al[i])
			s.t[ti1.Singular] = &ti1
			s.t[ti1.Plural] = &ti1
		}
	}

	return nil
}

func (s *DBSchema) addMultiRefs(ti *DBTableInfo) error {
	// if multiple columns have foreign keys that point
	// to the same table then we need to be smart
	// create a new entry (table) with it's name derived from
	for _, c := range ti.Columns {
		if c.FKeyTable == "" {
			continue
		}

		if v, ok := ti.fkMultiRef[c.FKeyTable]; !ok || v == 1 {
			continue
		}

		var ti *DBTableInfo
		var ok bool

		if ti, ok = s.t[c.FKeyTable]; !ok {
			return fmt.Errorf("tableinfo missing: %s", c.FKeyTable)
		}

		name := getRelName(c.Name)
		k1 := flect.Singularize(name)
		s.t[k1] = ti

		k2 := flect.Pluralize(name)
		s.t[k2] = ti
	}

	return nil
}

func (s *DBSchema) virtualRels(vts []VirtualTable) error {
	for _, vt := range vts {
		s.vt[vt.Name] = &vt

		for _, t := range s.t {
			idCol, ok := t.getColumn(vt.IDColumn)
			if !ok {
				continue
			}

			if _, ok := t.getColumn(vt.TypeColumn); !ok {
				continue
			}

			nt := DBTable{
				ID:   -1,
				Name: vt.Name,
				Key:  strings.ToLower(vt.Name),
				Type: "virtual",
			}

			if err := s.addTableInfo(nt, nil, nil); err != nil {
				return err
			}

			rel := &DBRel{Type: RelPolymorphic}
			rel.Left.Col = idCol

			rcol := DBColumn{
				Name: vt.FKeyColumn,
				Key:  strings.ToLower(vt.FKeyColumn),
				Type: idCol.Type,
			}

			rel.Right.VTable = vt.TypeColumn
			rel.Right.Col = rcol

			if err := s.SetRel(vt.Name, t.Name, rel); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *DBSchema) firstDegreeRels(t DBTable, cols []DBColumn) error {
	ct := t.Key
	cti, ok := s.t[t.Key]
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

		fti, ok := s.t[ft]
		if !ok {
			return fmt.Errorf("invalid foreign key table '%s'", ft)
		}

		childName := ct
		parentName := ft

		if v, ok := cti.fkMultiRef[ft]; ok && v > 1 {
			parentName = getRelName(c.Name)
		}

		// This is an embedded relationship like when a json/jsonb column
		// is exposed as a table
		if c.Name == c.FKeyTable && len(c.FKeyColID) == 0 {
			rel := &DBRel{Type: RelEmbedded}
			rel.Left.Col = cti.PrimaryCol
			rel.Right.Col = c

			if err := s.SetRel(parentName, ct, rel); err != nil {
				return err
			}
			continue
		}

		if len(c.FKeyColID) == 0 {
			continue
		}

		// Foreign key column id
		fcid := c.FKeyColID[0]

		fc, ok := fti.getColumnByID(fcid)
		if !ok {
			return fmt.Errorf("invalid foreign key column id '%d' for table '%s'",
				fcid, fti.Name)
		}

		rel1 := &DBRel{}

		// One-to-many relation between current table and the
		// table in the foreign key
		switch {
		case ct == ft:
			rel1.Type = RelRecursive
			rel1.Right.VTable = "_rcte_" + ct
		case fc.UniqueKey:
			rel1.Type = RelOneToOne
		default:
			rel1.Type = RelOneToMany
		}

		rel1.Left.Col = c
		rel1.Right.Col = fc

		if err := s.SetRel(childName, parentName, rel1); err != nil {
			return err
		}

		if ct == ft {
			continue
		}

		rel2 := &DBRel{}

		// One-to-many reverse relation between the foreign key table and the
		// the current table
		if c.UniqueKey {
			rel2.Type = RelOneToOne
		} else {
			rel2.Type = RelOneToMany
		}

		rel2.Left.Col = fc
		rel2.Right.Col = c

		if err := s.SetRel(parentName, childName, rel2); err != nil {
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

	for _, c := range cols {
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
		// is exposed as a table so skip
		if c.Name == c.FKeyTable && len(c.FKeyColID) == 0 {
			continue
		}

		if len(c.FKeyColID) == 0 {
			continue
		}

		// Foreign key column id
		fcid := c.FKeyColID[0]

		if _, ok := ti.getColumnByID(fcid); !ok {
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

	if t1 == t2 {
		return nil
	}

	childName := getRelName(col1.Name)
	parentName := t2

	if v, ok := ti.fkMultiRef[t2]; ok && v > 1 {
		parentName = getRelName(col2.Name)
	}

	fti1, ok := s.t[t1]
	if !ok {
		return fmt.Errorf("invalid foreign key table '%s'", t1)
	}

	fc1, ok := fti1.getColumnByID(col1.FKeyColID[0])
	if !ok {
		return fmt.Errorf("invalid foreign key column id %d for table '%s'",
			col1.FKeyColID[0], t1)
	}

	fti2, ok := s.t[t2]
	if !ok {
		return fmt.Errorf("invalid foreign key table '%s'", t2)
	}

	fc2, ok := fti2.getColumnByID(col2.FKeyColID[0])
	if !ok {
		return fmt.Errorf("invalid foreign key column id %d for table '%s'",
			col2.FKeyColID[0], t2)
	}

	// One-to-many-through relation between 1nd foreign key table and the
	// 2nd foreign key table
	rel1 := &DBRel{Type: RelOneToManyThrough}
	rel1.Through.ColL = col1
	rel1.Through.ColR = col2

	rel1.Left.Col = fc1
	rel1.Right.Col = fc2

	if err := s.SetRel(childName, parentName, rel1); err != nil {
		return err
	}

	// One-to-many-through relation between 2nd foreign key table and the
	// 1nd foreign key table
	rel2 := &DBRel{Type: RelOneToManyThrough}
	rel2.Through.ColL = col2
	rel2.Through.ColR = col1

	rel2.Left.Col = fc2
	rel2.Right.Col = fc1

	if err := s.SetRel(parentName, childName, rel2); err != nil {
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

func (s *DBSchema) GetTableInfo(selName string) (*DBTableInfo, error) {
	t, ok := s.t[selName]
	if !ok {
		return nil, fmt.Errorf("table: '%s' not found", selName)
	}
	return t, nil
}

func (s *DBSchema) GetTableInfoB(selName string) (*DBTableInfo, error) {
	t, ok := s.t[selName]
	if !ok {
		return nil, fmt.Errorf("table: '%s' not found", selName)
	}
	if t.Blocked {
		return nil, fmt.Errorf("table: '%s' (%s) blocked", t.Name, selName)
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
		ct, err := s.GetTableInfo(child)
		if err != nil {
			return nil, err
		}
		pt, err := s.GetTableInfo(parent)
		if err != nil {
			return nil, err
		}
		rel, ok = s.rm[ct.Name][pt.Name]
		if !ok {
			return nil, fmt.Errorf("relationship: '%s' -> '%s' not found",
				child, parent)
		}
	}
	return rel, nil
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

func (ti *DBTableInfo) getColumnByID(id int16) (DBColumn, bool) {
	var c DBColumn
	if i, ok := ti.colIDMap[id]; ok {
		return ti.Columns[i], true
	}
	return c, false
}

func (ti *DBTableInfo) GetColumnByID(id int16) (DBColumn, error) {
	c, ok := ti.getColumnByID(id)
	if ok {
		return c, nil
	}
	return c, fmt.Errorf("column: '%s.%d'  not found", ti.Name, id)
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

func (s *DBSchema) GetFunctions() map[string]*DBFunction {
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

func (s *DBSchema) DBVersion() int {
	return s.ver
}
