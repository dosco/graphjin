package catalog

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const DefaultSchema = "PUBLIC"

type Schema struct {
	DBName        string
	Schema        string
	DiscoveryMode string
	Tables        []*Table
	byName        map[string]*Table
}

type Table struct {
	Name           string
	IsView         bool
	Columns        []*Column
	PrimaryKeys    []string
	ClusteringKeys []string
}

type Column struct {
	Name       string
	Type       string
	DDLType    string
	Default    string
	NotNull    bool
	PrimaryKey bool
	UniqueKey  bool
	Array      bool
	FKeyTable  string
	FKeyColumn string
}

type tableFK struct {
	local []string
	ft    string
	fcols []string
}

// ParseSeed reads the Snowflake fixture DDL and extracts enough catalog
// metadata to satisfy GraphJin's Snowflake schema discovery queries.
func ParseSeed(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSeedBytes(data)
}

func ParseSeedBytes(data []byte) (*Schema, error) {
	s := &Schema{
		DBName:        "DB",
		Schema:        DefaultSchema,
		DiscoveryMode: "information_schema",
		byName:        make(map[string]*Table),
	}
	for _, stmt := range SplitStatements(StripSQLComments(string(data))) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		switch {
		case strings.HasPrefix(upper, "CREATE TABLE "):
			t, err := parseCreateTable(stmt)
			if err != nil {
				return nil, err
			}
			s.addTable(t)
		case strings.HasPrefix(upper, "CREATE VIEW "):
			t := parseCreateView(stmt, s)
			if t != nil {
				s.addTable(t)
			}
		}
	}
	if len(s.Tables) == 0 {
		return nil, fmt.Errorf("snowflake seed parser found no tables")
	}
	return s, nil
}

func (s *Schema) SetDiscoveryMode(mode string) {
	if s == nil {
		return
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "show" {
		s.DiscoveryMode = "show"
		return
	}
	s.DiscoveryMode = "information_schema"
}

func (s *Schema) addTable(t *Table) {
	key := NormIdent(t.Name)
	s.byName[key] = t
	s.Tables = append(s.Tables, t)
}

func (s *Schema) upsertTable(t *Table) {
	if s.byName == nil {
		s.byName = make(map[string]*Table)
	}
	key := NormIdent(t.Name)
	if existing := s.byName[key]; existing != nil {
		*existing = *t
		s.byName[key] = existing
		return
	}
	s.addTable(t)
}

func (s *Schema) removeTable(name string) {
	if s == nil {
		return
	}
	key := NormIdent(name)
	delete(s.byName, key)
	for i, t := range s.Tables {
		if NormIdent(t.Name) == key {
			s.Tables = append(s.Tables[:i], s.Tables[i+1:]...)
			return
		}
	}
}

func (s *Schema) Table(name string) *Table {
	if s == nil {
		return nil
	}
	return s.byName[NormIdent(name)]
}

func (s *Schema) ApplyDDL(sql string) {
	if s == nil {
		return
	}
	for _, stmt := range SplitStatements(StripSQLComments(sql)) {
		stmt = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
		if stmt == "" {
			continue
		}
		s.applyDDLStatement(stmt)
	}
}

func (s *Schema) applyDDLStatement(stmt string) {
	upper := strings.ToUpper(stmt)
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE "):
		t, err := parseCreateTable(stmt)
		if err == nil {
			s.upsertTable(t)
		}
	case strings.HasPrefix(upper, "CREATE OR REPLACE TABLE "):
		rewritten := "CREATE TABLE " + strings.TrimSpace(stmt[len("CREATE OR REPLACE TABLE "):])
		t, err := parseCreateTable(rewritten)
		if err == nil {
			s.upsertTable(t)
		}
	case strings.HasPrefix(upper, "DROP TABLE "):
		if name := parseDropTableName(stmt); name != "" {
			s.removeTable(name)
		}
	case strings.HasPrefix(upper, "ALTER TABLE "):
		s.applyAlterTable(stmt)
	}
}

func parseDropTableName(stmt string) string {
	rest := strings.TrimSpace(stmt[len("DROP TABLE "):])
	if strings.HasPrefix(strings.ToUpper(rest), "IF EXISTS ") {
		rest = strings.TrimSpace(rest[len("IF EXISTS "):])
	}
	name, _ := readIdentToken(rest)
	return name
}

func (s *Schema) applyAlterTable(stmt string) {
	rest := strings.TrimSpace(stmt[len("ALTER TABLE "):])
	name, rest := readIdentToken(rest)
	if name == "" {
		return
	}
	t := s.Table(name)
	if t == nil {
		t = &Table{Name: name}
		s.upsertTable(t)
	}
	rest = strings.TrimSpace(rest)
	upper := strings.ToUpper(rest)
	switch {
	case strings.HasPrefix(upper, "ADD COLUMN "):
		s.applyAddColumn(t, strings.TrimSpace(rest[len("ADD COLUMN "):]))
	case strings.HasPrefix(upper, "ADD "):
		s.applyAddColumn(t, strings.TrimSpace(rest[len("ADD "):]))
	case strings.HasPrefix(upper, "DROP COLUMN "):
		col, _ := readIdentToken(strings.TrimSpace(rest[len("DROP COLUMN "):]))
		t.dropColumn(col)
	}
}

func (s *Schema) applyAddColumn(t *Table, def string) {
	col := parseColumnDef(def)
	if col == nil {
		return
	}
	t.upsertColumn(col)
	if col.PrimaryKey {
		t.PrimaryKeys = appendUnique(t.PrimaryKeys, col.Name)
	}
}

func readIdentToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '"' {
		end := 1
		for end < len(s) {
			if s[end] == '"' {
				return TrimIdent(s[:end+1]), strings.TrimSpace(s[end+1:])
			}
			end++
		}
		return TrimIdent(s), ""
	}
	end := 0
	for end < len(s) && !isDDLSpace(s[end]) {
		end++
	}
	return TrimIdent(s[:end]), strings.TrimSpace(s[end:])
}

func isDDLSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t'
}

func parseCreateTable(stmt string) (*Table, error) {
	open := strings.IndexByte(stmt, '(')
	if open < 0 {
		return nil, fmt.Errorf("CREATE TABLE missing body: %s", stmt)
	}
	close := FindMatchingParen(stmt, open)
	if close < 0 {
		return nil, fmt.Errorf("CREATE TABLE unclosed body: %s", stmt)
	}

	name := strings.TrimSpace(stmt[len("CREATE TABLE "):open])
	name = TrimIdent(name)
	t := &Table{Name: name}

	var fks []tableFK
	for _, item := range SplitTopLevel(stmt[open+1:close], ',') {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		upper := strings.ToUpper(item)
		switch {
		case strings.HasPrefix(upper, "PRIMARY KEY"):
			t.PrimaryKeys = parseParenIdentList(item)
		case strings.HasPrefix(upper, "FOREIGN KEY"):
			if fk, ok := parseTableFK(item); ok {
				fks = append(fks, fk)
			}
		default:
			if col := parseColumnDef(item); col != nil {
				if col.PrimaryKey {
					t.PrimaryKeys = appendUnique(t.PrimaryKeys, col.Name)
				}
				t.Columns = append(t.Columns, col)
			}
		}
	}

	for _, pk := range t.PrimaryKeys {
		if col := t.Column(pk); col != nil {
			col.PrimaryKey = true
			col.UniqueKey = true
			col.NotNull = true
		}
	}
	for _, fk := range fks {
		for i, local := range fk.local {
			if col := t.Column(local); col != nil {
				col.FKeyTable = fk.ft
				if i < len(fk.fcols) {
					col.FKeyColumn = fk.fcols[i]
				}
			}
		}
	}

	if keys := parseClusterBy(stmt[close+1:]); len(keys) != 0 {
		t.ClusteringKeys = keys
	}
	return t, nil
}

func parseCreateView(stmt string, schema *Schema) *Table {
	re := regexp.MustCompile(`(?is)^CREATE\s+VIEW\s+([^\s]+)\s+AS\s+SELECT\s+(.+?)\s+FROM\s+([^\s;]+)`)
	m := re.FindStringSubmatch(stmt)
	if m == nil {
		return nil
	}
	t := &Table{Name: TrimIdent(m[1]), IsView: true}
	source := schema.Table(m[3])
	for _, expr := range SplitTopLevel(m[2], ',') {
		expr = strings.TrimSpace(expr)
		name, src := selectAlias(expr)
		col := &Column{Name: name, Type: "VARCHAR"}
		if source != nil {
			if sourceCol := source.Column(src); sourceCol != nil {
				col.Type = sourceCol.Type
				col.DDLType = sourceCol.DDLType
				col.NotNull = sourceCol.NotNull
				col.Array = sourceCol.Array
			}
		}
		t.Columns = append(t.Columns, col)
	}
	return t
}

func selectAlias(expr string) (name, src string) {
	fields := strings.Fields(expr)
	if len(fields) >= 3 && strings.EqualFold(fields[len(fields)-2], "AS") {
		return TrimIdent(fields[len(fields)-1]), TrimIdent(fields[0])
	}
	last := fields[len(fields)-1]
	if dot := strings.LastIndexByte(last, '.'); dot >= 0 {
		last = last[dot+1:]
	}
	return TrimIdent(last), TrimIdent(last)
}

func parseColumnDef(item string) *Column {
	fields := strings.Fields(item)
	if len(fields) < 2 {
		return nil
	}
	col := &Column{Name: TrimIdent(fields[0])}
	rest := strings.TrimSpace(item[len(fields[0]):])
	upperRest := strings.ToUpper(rest)

	typeEnd := len(rest)
	for _, kw := range []string{" NOT NULL", " PRIMARY KEY", " UNIQUE", " REFERENCES ", " DEFAULT "} {
		if idx := strings.Index(upperRest, kw); idx >= 0 && idx < typeEnd {
			typeEnd = idx
		}
	}
	rawType := strings.TrimSpace(rest[:typeEnd])
	col.Type = normalizeType(rawType)
	col.DDLType = normalizeDDLType(rawType)
	col.Default = parseColumnDefault(rest)
	col.NotNull = strings.Contains(upperRest, " NOT NULL")
	col.PrimaryKey = strings.Contains(upperRest, " PRIMARY KEY")
	col.UniqueKey = strings.Contains(upperRest, " UNIQUE")
	col.Array = strings.EqualFold(col.Type, "ARRAY") ||
		strings.HasPrefix(strings.ToUpper(col.Type), "ARRAY<") ||
		strings.HasSuffix(col.Type, "[]")

	if idx := strings.Index(upperRest, " REFERENCES "); idx >= 0 {
		ref := strings.TrimSpace(rest[idx+len(" REFERENCES "):])
		if table, cols, ok := parseReferences(ref); ok {
			col.FKeyTable = table
			if len(cols) != 0 {
				col.FKeyColumn = cols[0]
			}
		}
	}
	return col
}

func parseColumnDefault(rest string) string {
	upper := strings.ToUpper(rest)
	idx := strings.Index(upper, " DEFAULT ")
	if idx < 0 {
		return ""
	}
	def := strings.TrimSpace(rest[idx+len(" DEFAULT "):])
	upperDef := strings.ToUpper(def)
	end := len(def)
	for _, kw := range []string{" NOT NULL", " PRIMARY KEY", " UNIQUE", " REFERENCES "} {
		if idx := strings.Index(upperDef, kw); idx >= 0 && idx < end {
			end = idx
		}
	}
	return strings.TrimSpace(def[:end])
}

func parseTableFK(item string) (tableFK, bool) {
	var out tableFK
	open := strings.IndexByte(item, '(')
	close := -1
	if open >= 0 {
		close = FindMatchingParen(item, open)
	}
	if open < 0 || close < 0 {
		return out, false
	}
	out.local = parseIdentList(item[open+1 : close])
	refIdx := strings.Index(strings.ToUpper(item[close+1:]), "REFERENCES")
	if refIdx < 0 {
		return out, false
	}
	ref := strings.TrimSpace(item[close+1+refIdx+len("REFERENCES"):])
	table, cols, ok := parseReferences(ref)
	if !ok {
		return out, false
	}
	out.ft = table
	out.fcols = cols
	return out, true
}

func parseReferences(ref string) (string, []string, bool) {
	open := strings.IndexByte(ref, '(')
	close := -1
	if open >= 0 {
		close = FindMatchingParen(ref, open)
	}
	if open < 0 || close < 0 {
		return "", nil, false
	}
	return TrimIdent(strings.TrimSpace(ref[:open])), parseIdentList(ref[open+1 : close]), true
}

func parseParenIdentList(s string) []string {
	open := strings.IndexByte(s, '(')
	close := -1
	if open >= 0 {
		close = FindMatchingParen(s, open)
	}
	if open < 0 || close < 0 {
		return nil
	}
	return parseIdentList(s[open+1 : close])
}

func parseIdentList(s string) []string {
	parts := SplitTopLevel(s, ',')
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = TrimIdent(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseClusterBy(s string) []string {
	upper := strings.ToUpper(s)
	idx := strings.Index(upper, "CLUSTER BY")
	if idx < 0 {
		return nil
	}
	rest := s[idx+len("CLUSTER BY"):]
	open := strings.IndexByte(rest, '(')
	close := -1
	if open >= 0 {
		close = FindMatchingParen(rest, open)
	}
	if open < 0 || close < 0 {
		return nil
	}
	return parseIdentList(rest[open+1 : close])
}

func (t *Table) Column(name string) *Column {
	key := NormIdent(name)
	for _, c := range t.Columns {
		if NormIdent(c.Name) == key {
			return c
		}
	}
	return nil
}

func (t *Table) upsertColumn(col *Column) {
	key := NormIdent(col.Name)
	for i, existing := range t.Columns {
		if NormIdent(existing.Name) == key {
			t.Columns[i] = col
			return
		}
	}
	t.Columns = append(t.Columns, col)
}

func (t *Table) dropColumn(name string) {
	key := NormIdent(name)
	for i, col := range t.Columns {
		if NormIdent(col.Name) == key {
			t.Columns = append(t.Columns[:i], t.Columns[i+1:]...)
			break
		}
	}
	var pks []string
	for _, pk := range t.PrimaryKeys {
		if NormIdent(pk) != key {
			pks = append(pks, pk)
		}
	}
	t.PrimaryKeys = pks
}

func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "VARCHAR"
	}
	if idx := strings.IndexByte(t, '('); idx > 0 {
		t = t[:idx]
	}
	return strings.ToUpper(TrimIdent(t))
}

func normalizeDDLType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "VARCHAR"
	}
	return strings.ToUpper(t)
}

func NormIdent(s string) string {
	return strings.ToLower(TrimIdent(s))
}

func TrimIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"[]")
	if dot := strings.LastIndexByte(s, '.'); dot >= 0 {
		s = s[dot+1:]
	}
	return strings.Trim(s, "`\"[]")
}

func appendUnique(in []string, val string) []string {
	key := NormIdent(val)
	for _, existing := range in {
		if NormIdent(existing) == key {
			return in
		}
	}
	return append(in, val)
}

func StripSQLComments(sql string) string {
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch == '\'' {
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}
		if !inSingle && ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			if i < len(sql) {
				b.WriteByte(sql[i])
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func SplitStatements(sql string) []string {
	var parts []string
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch == '\'' {
			inSingle = !inSingle
		}
		if ch == ';' && !inSingle {
			parts = append(parts, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	if strings.TrimSpace(b.String()) != "" {
		parts = append(parts, b.String())
	}
	return parts
}

func SplitTopLevel(s string, sep byte) []string {
	var parts []string
	var b strings.Builder
	depth := 0
	inSingle := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			inSingle = !inSingle
		case '(':
			if !inSingle {
				depth++
			}
		case ')':
			if !inSingle && depth > 0 {
				depth--
			}
		}
		if ch == sep && depth == 0 && !inSingle {
			parts = append(parts, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	parts = append(parts, b.String())
	return parts
}

func FindMatchingParen(s string, open int) int {
	depth := 0
	inSingle := false
	for i := open; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			inSingle = !inSingle
			continue
		}
		if inSingle {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
