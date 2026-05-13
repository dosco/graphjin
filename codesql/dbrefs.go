//go:build cgo

package codesql

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type dbRefCandidate struct {
	Database   string
	Schema     string
	Table      string
	Column     string
	RefKind    string
	Confidence float64
	Evidence   string
	SourceText string
	StartByte  int
	EndByte    int
}

type dbRefResolution struct {
	Database  string
	Schema    string
	Table     string
	Column    string
	TableKey  string
	ColumnKey string
	Resolved  bool
	Ambiguous bool
}

type dbRefTarget struct {
	Database string
	Schema   string
	Table    string
	Columns  map[string]string
}

type dbRefResolver struct {
	targets     []dbRefTarget
	byTableName map[string][]dbRefTarget
	byFullTable map[string]dbRefTarget
}

var (
	sqlTableRefRE   = regexp.MustCompile(`(?is)\b(?:from|join|into|update|references)\s+([A-Za-z_][\w$]*(?:(?:\.|:)[A-Za-z_][\w$]*){0,2})`)
	sqlTableStmtRE  = regexp.MustCompile(`(?is)\b(?:create\s+table(?:\s+if\s+not\s+exists)?|alter\s+table|delete\s+from|insert\s+into)\s+([A-Za-z_][\w$]*(?:(?:\.|:)[A-Za-z_][\w$]*){0,2})`)
	sqlColumnRefRE  = regexp.MustCompile(`(?is)\b([A-Za-z_][\w$]*)\.([A-Za-z_][\w$]*)\b`)
	sqlFKRefRE      = regexp.MustCompile(`(?is)\breferences\s+([A-Za-z_][\w$]*(?:(?:\.|:)[A-Za-z_][\w$]*){0,2})\s*\(\s*([A-Za-z_][\w$]*)\s*\)`)
	createTableRE   = regexp.MustCompile(`(?is)\bcreate\s+table(?:\s+if\s+not\s+exists)?\s+([A-Za-z_][\w$]*(?:(?:\.|:)[A-Za-z_][\w$]*){0,2})\s*\((.*?)\)`)
	configNameRE    = regexp.MustCompile(`(?im)^\s*(?:-\s*)?name\s*:\s*['"]?([A-Za-z_][\w$]*)['"]?\s*$`)
	configRelatedRE = regexp.MustCompile(`(?im)^\s*(?:-\s*)?(?:related_to|foreign_key)\s*:\s*['"]?([A-Za-z_][\w$]*(?:(?:\.|:)[A-Za-z_][\w$]*){1,3})['"]?\s*$`)
	goStructRE      = regexp.MustCompile("(?s)type\\s+([A-Za-z_]\\w*)\\s+struct\\s*\\{(.*?)\\}")
	goTagRE         = regexp.MustCompile("`([^`]*)`")
	dbTagRE         = regexp.MustCompile(`\b(?:db|json):"([^",]+)`)
	gormColumnRE    = regexp.MustCompile(`\bgorm:"[^"]*\bcolumn:([^;"]+)`)
)

func newDBRefResolver(targets []DBRefTarget) *dbRefResolver {
	r := &dbRefResolver{
		byTableName: make(map[string][]dbRefTarget),
		byFullTable: make(map[string]dbRefTarget),
	}
	for _, t := range targets {
		if t.DatabaseName == "" || t.TableName == "" {
			continue
		}
		rt := dbRefTarget{
			Database: t.DatabaseName,
			Schema:   t.SchemaName,
			Table:    t.TableName,
			Columns:  make(map[string]string, len(t.Columns)),
		}
		for _, c := range t.Columns {
			rt.Columns[normName(c)] = c
		}
		r.targets = append(r.targets, rt)
		r.byTableName[normName(t.TableName)] = append(r.byTableName[normName(t.TableName)], rt)
		r.byFullTable[fullTableKey(t.DatabaseName, t.SchemaName, t.TableName)] = rt
	}
	return r
}

func (idx *indexer) setDBRefTargets(ctx context.Context, targets []DBRefTarget) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.dbRefResolver = newDBRefResolver(targets)
	if !idx.inferDBRefs {
		return nil
	}
	return idx.linkAllDBRefs(ctx)
}

func (idx *indexer) inferDBRefsForFile(ctx context.Context, tx *sql.Tx, fileID int64, language, relPath string, data []byte) error {
	if !idx.inferDBRefs {
		return nil
	}
	candidates := inferDBRefCandidates(language, relPath, string(data))
	seen := make(map[string]struct{}, len(candidates))
	for _, cand := range candidates {
		if cand.Table == "" {
			continue
		}
		key := strings.Join([]string{cand.RefKind, cand.Database, cand.Schema, cand.Table, cand.Column, cand.Evidence}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := idx.insertDBRefCandidate(ctx, tx, fileID, cand); err != nil {
			return err
		}
	}
	return nil
}

func inferDBRefCandidates(language, relPath, text string) []dbRefCandidate {
	var out []dbRefCandidate
	lang := strings.ToLower(language)
	if lang == "sql" || looksLikeSQL(text) {
		kind := "sql_string"
		if lang == "sql" {
			kind = "sql_file"
			if strings.Contains(strings.ToLower(relPath), "migration") {
				kind = "migration"
			}
		}
		out = append(out, inferSQLRefs(text, kind)...)
	}
	if lang == "graphql" || strings.HasSuffix(strings.ToLower(relPath), ".gql") || looksLikeGraphQL(text) {
		out = append(out, inferGraphQLRefs(text)...)
	}
	if lang == "config" {
		out = append(out, inferConfigRefs(text)...)
	}
	if lang == "go" {
		out = append(out, inferGoStructTagRefs(text)...)
	}
	return out
}

func inferSQLRefs(text, kind string) []dbRefCandidate {
	var out []dbRefCandidate
	for _, re := range []*regexp.Regexp{sqlTableRefRE, sqlTableStmtRE} {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			if len(m) < 4 {
				continue
			}
			db, schema, table, _ := parseRefIdent(text[m[2]:m[3]])
			out = append(out, dbRefCandidate{
				Database: db, Schema: schema, Table: table,
				RefKind: kind, Confidence: 0.95, Evidence: "sql_table", SourceText: clip(text[m[0]:m[1]], 300),
				StartByte: m[2], EndByte: m[3],
			})
		}
	}
	for _, m := range sqlFKRefRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 6 {
			continue
		}
		db, schema, table, _ := parseRefIdent(text[m[2]:m[3]])
		col := cleanIdent(text[m[4]:m[5]])
		out = append(out, dbRefCandidate{
			Database: db, Schema: schema, Table: table, Column: col,
			RefKind: "migration", Confidence: 1, Evidence: "sql_references", SourceText: clip(text[m[0]:m[1]], 300),
			StartByte: m[2], EndByte: m[5],
		})
	}
	for _, m := range sqlColumnRefRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 6 {
			continue
		}
		table := cleanIdent(text[m[2]:m[3]])
		col := cleanIdent(text[m[4]:m[5]])
		if isSQLNoiseIdentifier(table) || isSQLNoiseIdentifier(col) {
			continue
		}
		out = append(out, dbRefCandidate{
			Table: table, Column: col,
			RefKind: kind, Confidence: 0.85, Evidence: "sql_qualified_column", SourceText: clip(text[m[0]:m[1]], 300),
			StartByte: m[0], EndByte: m[1],
		})
	}
	for _, m := range createTableRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 6 {
			continue
		}
		db, schema, table, _ := parseRefIdent(text[m[2]:m[3]])
		body := text[m[4]:m[5]]
		baseStart := m[4]
		for _, col := range parseCreateTableColumns(body) {
			if col == "" {
				continue
			}
			out = append(out, dbRefCandidate{
				Database: db, Schema: schema, Table: table, Column: col,
				RefKind: "migration", Confidence: 1, Evidence: "create_table_column", SourceText: clip(table+"."+col, 300),
				StartByte: baseStart, EndByte: baseStart + len(body),
			})
		}
	}
	return out
}

func inferGraphQLRefs(text string) []dbRefCandidate {
	tokens := graphqlTokens(text)
	var out []dbRefCandidate
	depth := 0
	rootTable := ""
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch tok.Value {
		case "{":
			depth++
			continue
		case "}":
			depth--
			if depth < 2 {
				rootTable = ""
			}
			continue
		}
		if !tok.IsIdent || gqlKeyword(tok.Value) {
			continue
		}
		next := nextGraphQLSig(tokens, i+1)
		if depth == 1 && next == "{" {
			rootTable = tok.Value
			out = append(out, dbRefCandidate{
				Table: tok.Value, RefKind: "graphql", Confidence: 1, Evidence: "graphql_root_field",
				SourceText: tok.Value, StartByte: tok.Start, EndByte: tok.End,
			})
			continue
		}
		if depth == 2 && rootTable != "" && next != "{" && prevGraphQLSig(tokens, i-1) != ":" {
			out = append(out, dbRefCandidate{
				Table: rootTable, Column: tok.Value, RefKind: "graphql", Confidence: 0.95, Evidence: "graphql_field",
				SourceText: rootTable + "." + tok.Value, StartByte: tok.Start, EndByte: tok.End,
			})
		}
	}
	return out
}

func inferConfigRefs(text string) []dbRefCandidate {
	var out []dbRefCandidate
	for _, m := range configNameRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 {
			continue
		}
		table := cleanIdent(text[m[2]:m[3]])
		out = append(out, dbRefCandidate{
			Table: table, RefKind: "config", Confidence: 0.75, Evidence: "config_name",
			SourceText: table, StartByte: m[2], EndByte: m[3],
		})
	}
	for _, m := range configRelatedRE.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 4 {
			continue
		}
		db, schema, table, col := parseRefIdent(text[m[2]:m[3]])
		out = append(out, dbRefCandidate{
			Database: db, Schema: schema, Table: table, Column: col,
			RefKind: "config", Confidence: 0.95, Evidence: "config_related_to", SourceText: clip(text[m[0]:m[1]], 300),
			StartByte: m[2], EndByte: m[3],
		})
	}
	return out
}

func inferGoStructTagRefs(text string) []dbRefCandidate {
	var out []dbRefCandidate
	for _, sm := range goStructRE.FindAllStringSubmatchIndex(text, -1) {
		if len(sm) < 6 {
			continue
		}
		table := pluralize(snakeCase(text[sm[2]:sm[3]]))
		body := text[sm[4]:sm[5]]
		bodyStart := sm[4]
		for _, tm := range goTagRE.FindAllStringSubmatchIndex(body, -1) {
			tagText := body[tm[2]:tm[3]]
			for _, col := range tagColumns(tagText) {
				if col == "" || col == "-" {
					continue
				}
				out = append(out, dbRefCandidate{
					Table: table, Column: col, RefKind: "struct_tag", Confidence: 0.75, Evidence: "go_struct_tag",
					SourceText: clip(tagText, 300), StartByte: bodyStart + tm[2], EndByte: bodyStart + tm[3],
				})
			}
		}
	}
	return out
}

func tagColumns(tagText string) []string {
	var cols []string
	for _, m := range dbTagRE.FindAllStringSubmatch(tagText, -1) {
		if len(m) > 1 {
			cols = append(cols, cleanIdent(m[1]))
		}
	}
	for _, m := range gormColumnRE.FindAllStringSubmatch(tagText, -1) {
		if len(m) > 1 {
			cols = append(cols, cleanIdent(m[1]))
		}
	}
	return cols
}

func (idx *indexer) insertDBRefCandidate(ctx context.Context, tx *sql.Tx, fileID int64, cand dbRefCandidate) error {
	resolution := idx.dbRefResolver.resolve(cand)
	startRow, startCol := byteRowCol(cand.SourceText, 0)
	if cand.StartByte >= 0 {
		// Recompute against the whole file through SQL after insertion would be
		// expensive; row/col here are best-effort and corrected for direct tests.
		startRow, startCol = 0, 0
	}
	endRow, endCol := startRow, startCol+(cand.EndByte-cand.StartByte)
	symbolID := idx.nearestSymbolID(ctx, tx, fileID, cand.StartByte)
	_, err := tx.ExecContext(ctx, `INSERT INTO code_db_refs(file_id, symbol_id, database_name, schema_name, table_name, column_name,
	  table_key, column_key, ref_kind, confidence, evidence, source_text, start_byte, end_byte, start_row, start_col, end_row, end_col, resolved, ambiguous)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fileID, symbolID, resolution.Database, resolution.Schema, resolution.Table, resolution.Column,
		resolution.TableKey, resolution.ColumnKey, cand.RefKind, cand.Confidence, cand.Evidence, cand.SourceText,
		cand.StartByte, cand.EndByte, startRow, startCol, endRow, endCol, resolution.Resolved, resolution.Ambiguous)
	return err
}

func (idx *indexer) nearestSymbolID(ctx context.Context, tx *sql.Tx, fileID int64, offset int) any {
	if offset < 0 {
		return nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM code_symbols
	  WHERE file_id = ? AND start_byte <= ? AND end_byte >= ?
	  ORDER BY (end_byte - start_byte) ASC LIMIT 1`, fileID, offset, offset).Scan(&id)
	if err != nil {
		return nil
	}
	return id
}

func (idx *indexer) linkAllDBRefs(ctx context.Context) error {
	if idx.dbRefResolver == nil {
		return nil
	}
	rows, err := idx.db.QueryContext(ctx, `SELECT id, database_name, schema_name, table_name, column_name, ref_kind, confidence, evidence, source_text, start_byte, end_byte
	  FROM code_db_refs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type update struct {
		id  int64
		res dbRefResolution
	}
	var updates []update
	for rows.Next() {
		var id int64
		var cand dbRefCandidate
		if err := rows.Scan(&id, &cand.Database, &cand.Schema, &cand.Table, &cand.Column, &cand.RefKind, &cand.Confidence, &cand.Evidence, &cand.SourceText, &cand.StartByte, &cand.EndByte); err != nil {
			return err
		}
		updates = append(updates, update{id: id, res: idx.dbRefResolver.resolve(cand)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, up := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE code_db_refs
		  SET database_name = ?, schema_name = ?, table_name = ?, column_name = ?, table_key = ?, column_key = ?, resolved = ?, ambiguous = ?
		  WHERE id = ?`, up.res.Database, up.res.Schema, up.res.Table, up.res.Column, up.res.TableKey, up.res.ColumnKey, up.res.Resolved, up.res.Ambiguous, up.id); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	return tx.Commit()
}

func (r *dbRefResolver) resolve(cand dbRefCandidate) dbRefResolution {
	res := dbRefResolution{
		Database: cand.Database,
		Schema:   cand.Schema,
		Table:    cand.Table,
		Column:   cand.Column,
	}
	if r == nil || len(r.targets) == 0 || cand.Table == "" {
		return res
	}
	matches := r.matchTables(cand)
	if len(matches) == 0 {
		return res
	}
	if len(matches) > 1 {
		res.Ambiguous = true
		return res
	}
	t := matches[0]
	res.Database = t.Database
	res.Schema = t.Schema
	res.Table = t.Table
	res.TableKey = metadataTableKey(t.Database, t.Schema, t.Table)
	if cand.Column == "" {
		res.Resolved = true
		return res
	}
	col, ok := t.Columns[normName(cand.Column)]
	if !ok {
		return res
	}
	res.Column = col
	res.ColumnKey = metadataColumnKey(t.Database, t.Schema, t.Table, col)
	res.Resolved = true
	return res
}

func (r *dbRefResolver) matchTables(cand dbRefCandidate) []dbRefTarget {
	table := normName(cand.Table)
	if cand.Database != "" {
		if cand.Schema != "" {
			if t, ok := r.byFullTable[fullTableKey(cand.Database, cand.Schema, cand.Table)]; ok {
				return []dbRefTarget{t}
			}
			return nil
		}
		var matches []dbRefTarget
		for _, t := range r.byTableName[table] {
			if normName(t.Database) == normName(cand.Database) {
				matches = append(matches, t)
			}
		}
		return matches
	}
	if cand.Schema != "" {
		var matches []dbRefTarget
		for _, t := range r.byTableName[table] {
			if normName(t.Schema) == normName(cand.Schema) {
				matches = append(matches, t)
			}
		}
		return matches
	}
	return r.byTableName[table]
}

func parseRefIdent(s string) (database, schema, table, column string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		database = cleanIdent(s[:i])
		s = s[i+1:]
	}
	parts := strings.Split(s, ".")
	for i := range parts {
		parts[i] = cleanIdent(parts[i])
	}
	switch len(parts) {
	case 1:
		table = parts[0]
	case 2:
		table, column = parts[0], parts[1]
	case 3:
		schema, table, column = parts[0], parts[1], parts[2]
	default:
		table = cleanIdent(s)
	}
	return
}

func parseCreateTableColumns(body string) []string {
	var out []string
	for _, part := range strings.Split(body, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) < 2 {
			continue
		}
		name := cleanIdent(fields[0])
		if name == "" || isSQLNoiseIdentifier(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func looksLikeSQL(s string) bool {
	low := strings.ToLower(s)
	strong := []string{"select ", " from ", " join ", "insert into", "update ", "delete from", "create table", "alter table", "references "}
	for _, token := range strong {
		if strings.Contains(low, token) {
			return true
		}
	}
	return false
}

func looksLikeGraphQL(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(s, "{") && (strings.Contains(low, "query") || strings.Contains(low, "mutation") || strings.Contains(low, "subscription"))
}

func cleanIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'[]")
	if i := strings.IndexAny(s, " \t\r\n("); i >= 0 {
		s = s[:i]
	}
	return s
}

func normName(s string) string {
	return strings.ToLower(cleanIdent(s))
}

func fullTableKey(database, schema, table string) string {
	return normName(database) + ":" + normName(schema) + "." + normName(table)
}

func metadataTableKey(database, schema, table string) string {
	return database + ":" + schema + "." + table
}

func metadataColumnKey(database, schema, table, column string) string {
	return metadataTableKey(database, schema, table) + "." + column
}

func isSQLNoiseIdentifier(s string) bool {
	switch strings.ToLower(s) {
	case "select", "where", "and", "or", "on", "as", "set", "values", "constraint", "primary", "foreign", "unique", "check", "not", "null", "default", "current", "new", "old", "fmt", "log", "time":
		return true
	default:
		return false
	}
}

type gqlToken struct {
	Value   string
	Start   int
	End     int
	IsIdent bool
}

func graphqlTokens(s string) []gqlToken {
	var out []gqlToken
	for i := 0; i < len(s); {
		r := rune(s[i])
		if unicode.IsSpace(r) || r == ',' {
			i++
			continue
		}
		if isIdentStart(r) {
			start := i
			i++
			for i < len(s) && isIdentPart(rune(s[i])) {
				i++
			}
			out = append(out, gqlToken{Value: s[start:i], Start: start, End: i, IsIdent: true})
			continue
		}
		if strings.ContainsRune("{}():", r) {
			out = append(out, gqlToken{Value: string(r), Start: i, End: i + 1})
		}
		i++
	}
	return out
}

func nextGraphQLSig(tokens []gqlToken, i int) string {
	paren := 0
	for ; i < len(tokens); i++ {
		v := tokens[i].Value
		if v == "(" {
			paren++
			continue
		}
		if v == ")" {
			if paren > 0 {
				paren--
			}
			continue
		}
		if paren > 0 {
			continue
		}
		if v == ":" && i+1 < len(tokens) {
			continue
		}
		return v
	}
	return ""
}

func prevGraphQLSig(tokens []gqlToken, i int) string {
	for ; i >= 0; i-- {
		return tokens[i].Value
	}
	return ""
}

func gqlKeyword(s string) bool {
	switch s {
	case "query", "mutation", "subscription", "fragment", "on", "true", "false", "null":
		return true
	default:
		return false
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func pluralize(s string) string {
	if strings.HasSuffix(s, "s") {
		return s
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		return strings.TrimSuffix(s, "y") + "ies"
	}
	return s + "s"
}

func byteRowCol(s string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s) {
		offset = len(s)
	}
	row, col := 0, 0
	for i := 0; i < offset; i++ {
		if s[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	return row, col
}

func sortTargets(targets []DBRefTarget) {
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].DatabaseName != targets[j].DatabaseName {
			return targets[i].DatabaseName < targets[j].DatabaseName
		}
		if targets[i].SchemaName != targets[j].SchemaName {
			return targets[i].SchemaName < targets[j].SchemaName
		}
		return targets[i].TableName < targets[j].TableName
	})
}
