package nanodb

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const DefaultSchema = "main"

type Column struct {
	Name         string
	Type         string
	PrimaryKey   bool
	Unique       bool
	NotNull      bool
	Index        bool
	FullText     bool
	FKeyDatabase string
	FKeySchema   string
	FKeyTable    string
	FKeyColumn   string
	FKeyUnique   bool
}

type Table struct {
	Name    string
	Schema  string
	Columns []Column
	Rows    []Row
}

type Row map[string]any

type Snapshot struct {
	Schema string
	Tables []Table

	tables map[string]*tableData
}

type tableData struct {
	table       Table
	columns     map[string]Column
	primaryKey  string
	rowIDs      []string
	indexes     map[string]map[string][]int
	fullText    []string
	searchIndex map[string]map[int]int
	searchText  []string
}

type DB struct {
	snap atomic.Pointer[Snapshot]
}

func New(snapshot Snapshot) (*DB, error) {
	db := &DB{}
	if err := db.Refresh(snapshot); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) Refresh(snapshot Snapshot) error {
	s, err := normalize(snapshot)
	if err != nil {
		return err
	}
	db.snap.Store(s)
	return nil
}

func (db *DB) Snapshot() *Snapshot {
	if db == nil {
		return nil
	}
	return db.snap.Load()
}

func normalize(snapshot Snapshot) (*Snapshot, error) {
	if strings.TrimSpace(snapshot.Schema) == "" {
		snapshot.Schema = DefaultSchema
	}
	out := &Snapshot{
		Schema: snapshot.Schema,
		Tables: make([]Table, len(snapshot.Tables)),
		tables: make(map[string]*tableData, len(snapshot.Tables)),
	}
	for i, table := range snapshot.Tables {
		if strings.TrimSpace(table.Name) == "" {
			return nil, fmt.Errorf("nanodb table name is required")
		}
		if strings.TrimSpace(table.Schema) == "" {
			table.Schema = snapshot.Schema
		}
		table.Columns = cloneColumns(table.Columns)
		table.Rows = cloneRows(table.Rows)
		td := buildTableData(table)
		key := tableKey(table.Schema, table.Name)
		if _, exists := out.tables[key]; exists {
			return nil, fmt.Errorf("duplicate nanodb table %s.%s", table.Schema, table.Name)
		}
		out.Tables[i] = table
		out.tables[key] = td
	}
	return out, nil
}

func buildTableData(table Table) *tableData {
	td := &tableData{
		table:       table,
		columns:     make(map[string]Column, len(table.Columns)),
		indexes:     make(map[string]map[string][]int),
		searchIndex: make(map[string]map[int]int),
		searchText:  make([]string, len(table.Rows)),
	}
	for _, col := range table.Columns {
		name := strings.TrimSpace(col.Name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(col.Type) == "" {
			col.Type = "text"
		}
		td.columns[name] = col
		if col.PrimaryKey && td.primaryKey == "" {
			td.primaryKey = name
		}
		if col.PrimaryKey || col.Unique || col.Index || col.FKeyTable != "" || col.FKeyDatabase != "" {
			td.indexes[name] = make(map[string][]int)
		}
		if col.FullText {
			td.fullText = append(td.fullText, name)
		}
	}
	if td.primaryKey == "" {
		if _, ok := td.columns["id"]; ok {
			td.primaryKey = "id"
		}
	}
	td.rowIDs = make([]string, len(table.Rows))
	for i, row := range table.Rows {
		if td.primaryKey != "" {
			td.rowIDs[i] = valueKey(row[td.primaryKey])
		}
		for col := range td.indexes {
			key := valueKey(row[col])
			td.indexes[col][key] = append(td.indexes[col][key], i)
		}
		text := td.rowSearchText(row)
		td.searchText[i] = text
		for _, token := range tokenize(text) {
			rows := td.searchIndex[token]
			if rows == nil {
				rows = make(map[int]int)
				td.searchIndex[token] = rows
			}
			rows[i]++
		}
	}
	return td
}

func (td *tableData) rowSearchText(row Row) string {
	if len(td.fullText) == 0 {
		return ""
	}
	var b strings.Builder
	for _, col := range td.fullText {
		if b.Len() != 0 {
			b.WriteByte(' ')
		}
		b.WriteString(valueKey(row[col]))
	}
	return b.String()
}

func (s *Snapshot) Table(schema, name string) (Table, bool) {
	td := s.tableData(schema, name)
	if td == nil {
		return Table{}, false
	}
	return td.table, true
}

func (s *Snapshot) Rows(schema, name string) ([]Row, bool) {
	td := s.tableData(schema, name)
	if td == nil {
		return nil, false
	}
	return td.table.Rows, true
}

func (s *Snapshot) SearchRank(schema, table string, row Row, query string) float64 {
	td := s.tableData(schema, table)
	if td == nil {
		return 0
	}
	return td.searchRank(row, query)
}

func (td *tableData) searchRank(row Row, query string) float64 {
	if strings.TrimSpace(query) == "" {
		return 0
	}
	idx := -1
	if td.primaryKey != "" {
		key := valueKey(row[td.primaryKey])
		for i, id := range td.rowIDs {
			if id == key {
				idx = i
				break
			}
		}
	} else {
		for i := range td.table.Rows {
			if sameRow(td.table.Rows[i], row) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return 0
	}
	score := 0
	for _, token := range tokenize(query) {
		if rows := td.searchIndex[token]; rows != nil {
			score += rows[idx]
		}
	}
	query = strings.TrimSpace(query)
	if query != "" {
		for col := range td.columns {
			if strings.EqualFold(valueKey(row[col]), query) {
				score += 100
			}
		}
	}
	return float64(score)
}

func sameRow(a, b Row) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if valueKey(av) != valueKey(b[k]) {
			return false
		}
	}
	return true
}

func (s *Snapshot) tableData(schema, name string) *tableData {
	if s == nil {
		return nil
	}
	if strings.TrimSpace(schema) == "" {
		schema = s.Schema
	}
	return s.tables[tableKey(schema, name)]
}

func (s *Snapshot) DBInfo(name string) *sdata.DBInfo {
	if s == nil {
		return sdata.NewDBInfo("nanodb", 0, DefaultSchema, name, nil, nil, nil)
	}
	var cols []sdata.DBColumn
	for _, table := range s.Tables {
		schema := table.Schema
		if schema == "" {
			schema = s.Schema
		}
		for i, col := range table.Columns {
			typ := strings.TrimSpace(col.Type)
			if typ == "" {
				typ = "text"
			}
			fkSchema := col.FKeySchema
			if fkSchema == "" {
				fkSchema = s.Schema
			}
			cols = append(cols, sdata.DBColumn{
				ID:           int32(i),
				Schema:       schema,
				Table:        table.Name,
				Name:         col.Name,
				Type:         typ,
				PrimaryKey:   col.PrimaryKey,
				UniqueKey:    col.Unique,
				NotNull:      col.NotNull,
				FullText:     col.FullText,
				Index:        col.Index,
				FKeyDatabase: col.FKeyDatabase,
				FKeySchema:   fkSchema,
				FKeyTable:    col.FKeyTable,
				FKeyCol:      col.FKeyColumn,
				FKeyIsUnique: col.FKeyUnique,
				Database:     name,
			})
		}
	}
	return sdata.NewDBInfo("nanodb", 0, s.Schema, name, cols, nil, nil)
}

func tableKey(schema, name string) string {
	return schema + ":" + name
}

func cloneColumns(in []Column) []Column {
	out := make([]Column, len(in))
	copy(out, in)
	return out
}

func cloneRows(in []Row) []Row {
	out := make([]Row, len(in))
	for i, row := range in {
		cp := make(Row, len(row))
		for k, v := range row {
			cp[k] = v
		}
		out[i] = cp
	}
	return out
}

func valueKey(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tok := strings.ToLower(b.String())
		b.Reset()
		if len(tok) < 2 {
			return
		}
		out = append(out, tok)
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	sort.Strings(out)
	return out
}
