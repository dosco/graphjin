package redshift

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultDBName = "dev"
	DefaultSchema = "public"
)

type Schema struct {
	DBName string
	Schema string
	Tables []*Table
	byName map[string]*Table
}

type Table struct {
	DBName      string
	Schema      string
	Name        string
	IsView      bool
	DistStyle   string
	DistKey     string
	SortKeyType string
	SortKeys    []string
	Columns     []*Column
	PrimaryKeys []string
}

type Column struct {
	Name              string
	Type              string
	DDLType           string
	Default           string
	NotNull           bool
	PrimaryKey        bool
	UniqueKey         bool
	FKeyTable         string
	FKeyColumn        string
	Encoding          string
	DistKey           bool
	SortKey           int
	SortKeyType       string
	Identity          bool
	GeneratedIdentity bool
	Collation         string
}

type tableFK struct {
	local []string
	ft    string
	fcols []string
}

func ParseSeedBytes(data []byte) (*Schema, error) {
	s := &Schema{
		DBName: DefaultDBName,
		Schema: DefaultSchema,
		byName: make(map[string]*Table),
	}
	for _, stmt := range SplitStatements(StripSQLComments(string(data))) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		switch {
		case isCreateTable(upper):
			t, err := parseCreateTable(stmt, s)
			if err != nil {
				return nil, err
			}
			s.addTable(t)
		case strings.HasPrefix(upper, "CREATE VIEW "):
			t := parseCreateView(stmt, s)
			if t != nil {
				s.addTable(t)
			}
		case strings.HasPrefix(upper, "INSERT "):
			continue
		case strings.HasPrefix(upper, "CREATE INDEX ") || strings.HasPrefix(upper, "CREATE UNIQUE INDEX "):
			return nil, fmt.Errorf("redshift emulator: indexes are not supported")
		default:
			return nil, fmt.Errorf("redshift emulator: unsupported statement: %.80s", stmt)
		}
	}
	if len(s.Tables) == 0 {
		return nil, fmt.Errorf("redshift seed parser found no tables")
	}
	return s, nil
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
	case isCreateTable(upper):
		t, err := parseCreateTable(stmt, s)
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

func (s *Schema) addTable(t *Table) {
	if t.DBName == "" {
		t.DBName = s.DBName
	}
	if t.Schema == "" {
		t.Schema = s.Schema
	}
	s.byName[tableKey(t.Schema, t.Name)] = t
	s.Tables = append(s.Tables, t)
}

func (s *Schema) upsertTable(t *Table) {
	if s.byName == nil {
		s.byName = make(map[string]*Table)
	}
	if t.DBName == "" {
		t.DBName = s.DBName
	}
	if t.Schema == "" {
		t.Schema = s.Schema
	}
	key := tableKey(t.Schema, t.Name)
	if existing := s.byName[key]; existing != nil {
		*existing = *t
		s.byName[key] = existing
		return
	}
	s.addTable(t)
}

func (s *Schema) removeTable(name string) {
	schema, table := splitSchemaTable(s.Schema, name)
	key := tableKey(schema, table)
	delete(s.byName, key)
	for i, t := range s.Tables {
		if tableKey(t.Schema, t.Name) == key {
			s.Tables = append(s.Tables[:i], s.Tables[i+1:]...)
			return
		}
	}
}

func (s *Schema) Table(name string) *Table {
	if s == nil {
		return nil
	}
	schema, table := splitSchemaTable(s.Schema, name)
	if t := s.byName[tableKey(schema, table)]; t != nil {
		return t
	}
	for _, t := range s.Tables {
		if NormIdent(t.Name) == NormIdent(table) {
			return t
		}
	}
	return nil
}

func (s *Schema) applyAlterTable(stmt string) {
	rest := strings.TrimSpace(stmt[len("ALTER TABLE "):])
	name, rest := readIdentToken(rest)
	if name == "" {
		return
	}
	t := s.Table(name)
	if t == nil {
		schema, table := splitSchemaTable(s.Schema, name)
		t = &Table{DBName: s.DBName, Schema: schema, Name: table, DistStyle: "AUTO"}
		s.upsertTable(t)
	}
	rest = strings.TrimSpace(rest)
	upper := strings.ToUpper(rest)
	switch {
	case strings.HasPrefix(upper, "ADD COLUMN "):
		_ = s.applyAddColumn(t, strings.TrimSpace(rest[len("ADD COLUMN "):]))
	case strings.HasPrefix(upper, "ADD "):
		_ = s.applyAddColumn(t, strings.TrimSpace(rest[len("ADD "):]))
	case strings.HasPrefix(upper, "DROP COLUMN "):
		col, _ := readIdentToken(strings.TrimSpace(rest[len("DROP COLUMN "):]))
		t.dropColumn(col)
	}
}

func (s *Schema) applyAddColumn(t *Table, def string) error {
	col, err := parseColumnDef(def)
	if err != nil || col == nil {
		return err
	}
	t.upsertColumn(col)
	if col.PrimaryKey {
		t.PrimaryKeys = appendUnique(t.PrimaryKeys, col.Name)
	}
	return nil
}

func isCreateTable(upper string) bool {
	return strings.HasPrefix(upper, "CREATE TABLE ") ||
		strings.HasPrefix(upper, "CREATE TEMP TABLE ") ||
		strings.HasPrefix(upper, "CREATE TEMPORARY TABLE ") ||
		strings.HasPrefix(upper, "CREATE LOCAL TEMP TABLE ") ||
		strings.HasPrefix(upper, "CREATE LOCAL TEMPORARY TABLE ")
}

var createTablePrefix = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:LOCAL\s+)?(?:(?:TEMPORARY|TEMP)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

func parseCreateTable(stmt string, schema *Schema) (*Table, error) {
	m := createTablePrefix.FindStringIndex(stmt)
	if m == nil {
		return nil, fmt.Errorf("redshift emulator: unsupported CREATE TABLE: %.80s", stmt)
	}
	open := strings.IndexByte(stmt[m[1]:], '(')
	if open < 0 {
		return nil, fmt.Errorf("CREATE TABLE missing body: %s", stmt)
	}
	open += m[1]
	close := FindMatchingParen(stmt, open)
	if close < 0 {
		return nil, fmt.Errorf("CREATE TABLE unclosed body: %s", stmt)
	}

	dbName, schemaName, tableName := parseQualifiedName(strings.TrimSpace(stmt[m[1]:open]), schema)
	t := &Table{DBName: dbName, Schema: schemaName, Name: tableName, DistStyle: "AUTO"}

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
		case strings.HasPrefix(upper, "UNIQUE"):
			continue
		default:
			col, err := parseColumnDef(item)
			if err != nil {
				return nil, err
			}
			if col != nil {
				if col.PrimaryKey {
					t.PrimaryKeys = appendUnique(t.PrimaryKeys, col.Name)
				}
				t.Columns = append(t.Columns, col)
			}
		}
	}

	parseTableAttributes(t, stmt[close+1:])
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
	return t, nil
}

func parseCreateView(stmt string, schema *Schema) *Table {
	re := regexp.MustCompile(`(?is)^CREATE\s+VIEW\s+([^\s]+)\s+AS\s+SELECT\s+(.+?)\s+FROM\s+([^\s;]+)`)
	m := re.FindStringSubmatch(stmt)
	if m == nil {
		return nil
	}
	dbName, schemaName, name := parseQualifiedName(m[1], schema)
	t := &Table{DBName: dbName, Schema: schemaName, Name: name, IsView: true, DistStyle: "AUTO"}
	source := schema.Table(m[3])
	for _, expr := range SplitTopLevel(m[2], ',') {
		expr = strings.TrimSpace(expr)
		name, src := selectAlias(expr)
		col := &Column{Name: name, Type: "VARCHAR", DDLType: "VARCHAR"}
		if source != nil {
			if sourceCol := source.Column(src); sourceCol != nil {
				*col = *sourceCol
				col.Name = name
			}
		}
		t.Columns = append(t.Columns, col)
	}
	return t
}

func parseColumnDef(item string) (*Column, error) {
	name, rest := readIdentToken(item)
	if name == "" || rest == "" {
		return nil, nil
	}
	typeEnd := len(rest)
	for _, kw := range []string{
		"DEFAULT", "IDENTITY", "GENERATED", "ENCODE", "DISTKEY", "SORTKEY",
		"COLLATE", "NOT NULL", "NULL", "UNIQUE", "PRIMARY KEY", "REFERENCES",
	} {
		if idx := indexTopLevelKeyword(rest, kw); idx >= 0 && idx < typeEnd {
			typeEnd = idx
		}
	}
	rawType := strings.TrimSpace(rest[:typeEnd])
	base, ddl, err := normalizeRedshiftType(rawType)
	if err != nil {
		return nil, err
	}
	col := &Column{Name: TrimIdent(name), Type: base, DDLType: ddl}
	attr := rest[typeEnd:]
	upperAttr := strings.ToUpper(attr)
	col.Default = parseColumnDefault(attr)
	col.NotNull = indexTopLevelKeyword(attr, "NOT NULL") >= 0
	col.PrimaryKey = indexTopLevelKeyword(attr, "PRIMARY KEY") >= 0
	col.UniqueKey = indexTopLevelKeyword(attr, "UNIQUE") >= 0
	col.Identity = indexTopLevelKeyword(attr, "IDENTITY") >= 0
	col.GeneratedIdentity = indexTopLevelKeyword(attr, "GENERATED BY DEFAULT AS IDENTITY") >= 0
	col.Encoding = parseKeywordValue(attr, "ENCODE")
	col.Collation = parseKeywordValue(attr, "COLLATE")
	col.DistKey = indexTopLevelKeyword(attr, "DISTKEY") >= 0
	if indexTopLevelKeyword(attr, "SORTKEY") >= 0 {
		col.SortKey = 1
		col.SortKeyType = "COMPOUND"
	}
	if idx := indexTopLevelKeyword(attr, "REFERENCES"); idx >= 0 {
		ref := strings.TrimSpace(attr[idx+len("REFERENCES"):])
		if table, cols, ok := parseReferences(ref); ok {
			col.FKeyTable = table
			if len(cols) != 0 {
				col.FKeyColumn = cols[0]
			}
		}
	}
	if strings.Contains(upperAttr, "PRIMARY KEY") {
		col.NotNull = true
	}
	return col, nil
}

func parseTableAttributes(t *Table, attrs string) {
	if t == nil {
		return
	}
	if distStyle := regexGroup(`(?is)\bDISTSTYLE\s+(AUTO|EVEN|KEY|ALL)\b`, attrs, 1); distStyle != "" {
		t.DistStyle = strings.ToUpper(distStyle)
	}
	if distKey := regexGroup(`(?is)\bDISTKEY\s*\(([^)]*)\)`, attrs, 1); distKey != "" {
		t.DistKey = TrimIdent(distKey)
		t.DistStyle = "KEY"
		if col := t.Column(t.DistKey); col != nil {
			col.DistKey = true
		}
	}
	sortType := strings.ToUpper(regexGroup(`(?is)\b(COMPOUND|INTERLEAVED)\s+SORTKEY\s*\(([^)]*)\)`, attrs, 1))
	sortList := regexGroup(`(?is)\b(?:COMPOUND|INTERLEAVED)?\s*SORTKEY\s*\(([^)]*)\)`, attrs, 1)
	if sortType == "" && sortList != "" {
		sortType = "COMPOUND"
	}
	if sortList != "" {
		t.SortKeyType = sortType
		t.SortKeys = parseIdentList(sortList)
		for i, key := range t.SortKeys {
			if col := t.Column(key); col != nil {
				col.SortKeyType = sortType
				col.SortKey = i + 1
				if sortType == "INTERLEAVED" && i%2 == 0 {
					col.SortKey = -col.SortKey
				}
			}
		}
	}
	var columnSortKeys []string
	for _, c := range t.Columns {
		if c.DistKey {
			t.DistKey = c.Name
			if t.DistStyle == "" || t.DistStyle == "AUTO" {
				t.DistStyle = "KEY"
			}
		}
		if c.SortKey != 0 && !containsIdent(t.SortKeys, c.Name) {
			columnSortKeys = append(columnSortKeys, c.Name)
		}
	}
	if len(t.SortKeys) == 0 && len(columnSortKeys) != 0 {
		t.SortKeyType = "COMPOUND"
		t.SortKeys = columnSortKeys
		for i, key := range t.SortKeys {
			if col := t.Column(key); col != nil {
				col.SortKeyType = "COMPOUND"
				col.SortKey = i + 1
			}
		}
	}
	if t.DistStyle == "" {
		t.DistStyle = "AUTO"
	}
}

func normalizeRedshiftType(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "VARCHAR", "VARCHAR", nil
	}
	upper := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
	if strings.Contains(upper, "[]") || strings.HasPrefix(upper, "ARRAY") {
		return "", "", fmt.Errorf("redshift emulator: unsupported PostgreSQL array type %q", raw)
	}
	base := baseType(upper)
	switch base {
	case "JSON":
		return "", "", fmt.Errorf("redshift emulator: unsupported PostgreSQL JSON type; use SUPER")
	case "SERIAL", "BIGSERIAL", "SMALLSERIAL":
		return "", "", fmt.Errorf("redshift emulator: unsupported PostgreSQL serial type %q; use IDENTITY", raw)
	case "BYTEA", "UUID", "MONEY", "XML", "HSTORE":
		return "", "", fmt.Errorf("redshift emulator: unsupported PostgreSQL type %q", raw)
	}
	return base, upper, nil
}

func baseType(upper string) string {
	switch {
	case strings.HasPrefix(upper, "DOUBLE PRECISION"):
		return "DOUBLE PRECISION"
	case strings.HasPrefix(upper, "CHARACTER VARYING"), strings.HasPrefix(upper, "VARCHAR"), strings.HasPrefix(upper, "NVARCHAR"):
		return "VARCHAR"
	case strings.HasPrefix(upper, "CHARACTER"), strings.HasPrefix(upper, "CHAR("), upper == "CHAR", strings.HasPrefix(upper, "NCHAR"):
		return "CHAR"
	case strings.HasPrefix(upper, "TIMESTAMP WITH TIME ZONE"), strings.HasPrefix(upper, "TIMESTAMPTZ"):
		return "TIMESTAMPTZ"
	case strings.HasPrefix(upper, "TIMESTAMP WITHOUT TIME ZONE"), strings.HasPrefix(upper, "TIMESTAMP"):
		return "TIMESTAMP"
	case strings.HasPrefix(upper, "TIME WITH TIME ZONE"), strings.HasPrefix(upper, "TIMETZ"):
		return "TIMETZ"
	case strings.HasPrefix(upper, "TIME WITHOUT TIME ZONE"), strings.HasPrefix(upper, "TIME"):
		return "TIME"
	case strings.HasPrefix(upper, "DECIMAL"), strings.HasPrefix(upper, "NUMERIC"):
		return "DECIMAL"
	case strings.HasPrefix(upper, "INTERVAL YEAR TO MONTH"):
		return "INTERVAL YEAR TO MONTH"
	case strings.HasPrefix(upper, "INTERVAL DAY TO SECOND"):
		return "INTERVAL DAY TO SECOND"
	}
	if idx := strings.IndexByte(upper, '('); idx > 0 {
		upper = upper[:idx]
	}
	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return "VARCHAR"
	}
	return fields[0]
}

func parseColumnDefault(rest string) string {
	idx := indexTopLevelKeyword(rest, "DEFAULT")
	if idx < 0 {
		return ""
	}
	upper := strings.ToUpper(rest)
	if strings.Contains(upper[:idx], "GENERATED BY") && strings.HasPrefix(upper[idx:], "DEFAULT AS IDENTITY") {
		return ""
	}
	def := strings.TrimSpace(rest[idx+len("DEFAULT"):])
	end := len(def)
	for _, kw := range []string{"ENCODE", "DISTKEY", "SORTKEY", "COLLATE", "NOT NULL", "NULL", "UNIQUE", "PRIMARY KEY", "REFERENCES"} {
		if idx := indexTopLevelKeyword(def, kw); idx >= 0 && idx < end {
			end = idx
		}
	}
	return strings.TrimSpace(def[:end])
}

func parseKeywordValue(rest, keyword string) string {
	idx := indexTopLevelKeyword(rest, keyword)
	if idx < 0 {
		return ""
	}
	val, _ := readIdentToken(strings.TrimSpace(rest[idx+len(keyword):]))
	return strings.ToUpper(TrimIdent(val))
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
	refIdx := indexTopLevelKeyword(item[close+1:], "REFERENCES")
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
	return TrimQualifiedIdent(strings.TrimSpace(ref[:open])), parseIdentList(ref[open+1 : close]), true
}

func parseDropTableName(stmt string) string {
	rest := strings.TrimSpace(stmt[len("DROP TABLE "):])
	if strings.HasPrefix(strings.ToUpper(rest), "IF EXISTS ") {
		rest = strings.TrimSpace(rest[len("IF EXISTS "):])
	}
	name, _ := readIdentToken(rest)
	return name
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

func parseQualifiedName(raw string, schema *Schema) (dbName, schemaName, tableName string) {
	dbName = DefaultDBName
	schemaName = DefaultSchema
	if schema != nil {
		dbName = schema.DBName
		schemaName = schema.Schema
	}
	parts := splitQualified(raw)
	switch len(parts) {
	case 0:
		tableName = ""
	case 1:
		tableName = parts[0]
	case 2:
		schemaName = parts[0]
		tableName = parts[1]
	default:
		dbName = parts[len(parts)-3]
		schemaName = parts[len(parts)-2]
		tableName = parts[len(parts)-1]
	}
	return dbName, schemaName, tableName
}

func splitSchemaTable(defaultSchema, raw string) (string, string) {
	parts := splitQualified(raw)
	switch len(parts) {
	case 0:
		return defaultSchema, ""
	case 1:
		return defaultSchema, parts[0]
	default:
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}

func splitQualified(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ";")
	var parts []string
	var b strings.Builder
	inDouble := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch ch {
		case '"':
			inDouble = !inDouble
			b.WriteByte(ch)
		case '.':
			if inDouble {
				b.WriteByte(ch)
				continue
			}
			parts = append(parts, TrimIdent(b.String()))
			b.Reset()
		default:
			b.WriteByte(ch)
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		parts = append(parts, TrimIdent(b.String()))
	}
	return parts
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

func tableKey(schema, table string) string {
	return NormIdent(schema) + "." + NormIdent(table)
}

func containsIdent(vals []string, val string) bool {
	key := NormIdent(val)
	for _, v := range vals {
		if NormIdent(v) == key {
			return true
		}
	}
	return false
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

func regexGroup(pattern, text string, group int) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) <= group {
		return ""
	}
	return strings.TrimSpace(m[group])
}

func charMaxLength(raw string) any {
	upper := strings.ToUpper(raw)
	if !strings.Contains(upper, "CHAR") && !strings.Contains(upper, "TEXT") {
		return nil
	}
	if strings.Contains(upper, "MAX") {
		if strings.Contains(upper, "CHAR") && !strings.Contains(upper, "VARCHAR") && !strings.Contains(upper, "VARYING") {
			return int64(4096)
		}
		return int64(65535)
	}
	if n, ok := firstParenInt(upper); ok {
		return int64(n)
	}
	if strings.Contains(upper, "TEXT") {
		return int64(256)
	}
	if strings.Contains(upper, "VARCHAR") || strings.Contains(upper, "VARYING") {
		return int64(256)
	}
	return int64(1)
}

func numericPrecisionScale(raw, base string) (any, any) {
	switch base {
	case "SMALLINT":
		return int64(16), int64(0)
	case "INTEGER", "INT", "INT4":
		return int64(32), int64(0)
	case "BIGINT", "INT8":
		return int64(64), int64(0)
	case "REAL", "FLOAT4":
		return int64(24), nil
	case "DOUBLE PRECISION", "FLOAT", "FLOAT8":
		return int64(53), nil
	case "DECIMAL", "NUMERIC":
		if p, s, ok := decimalPrecisionScale(raw); ok {
			return int64(p), int64(s)
		}
		return int64(18), int64(0)
	default:
		return nil, nil
	}
}

func decimalPrecisionScale(raw string) (int, int, bool) {
	open := strings.IndexByte(raw, '(')
	close := -1
	if open >= 0 {
		close = strings.IndexByte(raw[open+1:], ')')
	}
	if open < 0 || close < 0 {
		return 0, 0, false
	}
	close += open + 1
	parts := strings.Split(raw[open+1:close], ",")
	if len(parts) == 0 {
		return 0, 0, false
	}
	p, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	var s int
	if len(parts) > 1 {
		s, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return 0, 0, false
		}
	}
	return p, s, true
}

func firstParenInt(raw string) (int, bool) {
	open := strings.IndexByte(raw, '(')
	close := -1
	if open >= 0 {
		close = strings.IndexByte(raw[open+1:], ')')
	}
	if open < 0 || close < 0 {
		return 0, false
	}
	close += open + 1
	n, err := strconv.Atoi(strings.TrimSpace(raw[open+1 : close]))
	return n, err == nil
}
