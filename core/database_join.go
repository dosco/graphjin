package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// execDatabaseJoins fetches data from other databases for cross-database relationships.
// This is similar to execRemoteJoin but for in-process database calls rather than HTTP.
func (s *gstate) execDatabaseJoins(c context.Context) (err error) {
	// Find fields that need cross-database joins
	fids, sfmap, err := s.databaseJoinFieldIds()
	if err != nil {
		return
	}

	if len(fids) == 0 {
		return nil
	}

	// Extract the parent IDs from the result data
	from := jsn.Get(s.data, fids)
	if len(from) == 0 {
		return nil // No IDs to join on
	}

	// Execute queries against target databases and get replacement data
	to, err := s.resolveDatabaseJoins(c, from, sfmap)
	if err != nil {
		return
	}

	// Replace placeholders in the result with actual data
	var ob bytes.Buffer
	if err = jsn.Replace(&ob, s.data, from, to); err != nil {
		return
	}
	s.data = ob.Bytes()
	return
}

// resolveDatabaseJoins executes queries against target databases for cross-DB relationships.
func (s *gstate) resolveDatabaseJoins(
	ctx context.Context,
	from []jsn.Field,
	sfmap map[string]*qcode.Select,
) ([]jsn.Field, error) {
	selects := s.cs.st.qc.Selects

	// Replacement data for the marked insertion points
	to := make([]jsn.Field, len(from))

	var wg sync.WaitGroup
	wg.Add(len(from))

	var cerr error
	var cerrMutex sync.Mutex

	for i, id := range from {
		// Use the json key to find the related Select object
		sel, ok := sfmap[string(id.Key)]
		if !ok {
			return nil, fmt.Errorf("invalid database join field key")
		}
		p := selects[sel.ParentID]

		// Get the target database context
		targetDB := sel.Database
		if targetDB == "" {
			targetDB = sel.Ti.Database
		}

		dbCtx, ok := s.gj.databases[targetDB]
		if !ok {
			return nil, fmt.Errorf("database not found: %s", targetDB)
		}

		// Extract parent ID value. Keep the raw JSON token around so a string
		// primary key like "null" is not confused with a JSON null.
		rawIDVal := bytes.TrimSpace(id.Value)
		idVal := jsn.Value(rawIDVal)

		go func(n int, idVal, rawIDVal []byte, sel *qcode.Select, dbCtx *dbContext, parentTable string) {
			defer wg.Done()

			// Handle null/empty parent IDs gracefully
			if len(idVal) == 0 || bytes.Equal(rawIDVal, []byte("null")) {
				to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: []byte("null")}
				return
			}

			ctx1, span := s.gj.spanStart(ctx, "Execute Database Join")
			if span.IsRecording() {
				span.SetAttributesString(
					StringAttr{"join.database", dbCtx.name},
					StringAttr{"join.table", sel.Table},
					StringAttr{"join.parent_table", parentTable},
				)
			}

			b, err := s.executeDatabaseJoinQuery(ctx1, dbCtx, sel, idVal)
			if err != nil {
				cerrMutex.Lock()
				cerr = fmt.Errorf("database join %s.%s: %w", dbCtx.name, sel.Table, err)
				spanErr := cerr
				cerrMutex.Unlock()
				span.Error(spanErr)
			}
			span.End()

			if err != nil {
				return
			}

			// Unwrap root JSON object: {"orders": [...]} -> [...]
			b = jsn.Strip(b, [][]byte{[]byte(sel.Table)})

			to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: b}
		}(i, idVal, rawIDVal, sel, dbCtx, p.Table)
	}

	wg.Wait()
	return to, cerr
}

// executeDatabaseJoinQuery executes a query against a target database for a cross-DB join.
// It builds a GraphQL sub-query for the child table filtered by the parent ID,
// compiles it using the target database's compilers, and executes it.
func (s *gstate) executeDatabaseJoinQuery(
	ctx context.Context,
	dbCtx *dbContext,
	sel *qcode.Select,
	parentID []byte,
) ([]byte, error) {
	selects := s.cs.st.qc.Selects

	// Build a GraphQL sub-query for the child table
	fkCol := sel.Rel.Left.Col
	subQuery := buildChildGraphQLQuery(sel, selects, fkCol, parentID)

	// Compile QCode using the target database's compiler
	qc, err := dbCtx.qcodeCompiler.Compile(subQuery, nil, s.role, s.r.namespace)
	if err != nil {
		return nil, fmt.Errorf("qcode compile failed: %w", err)
	}

	if dbCtx.nano != nil {
		raw, err := s.renderNanoDBQuery(dbCtx, qc)
		if err == nil && len(raw) == 0 {
			if sel.Singular {
				raw = []byte(`{"` + sel.Table + `": null}`)
			} else {
				raw = []byte(`{"` + sel.Table + `": []}`)
			}
		}
		return raw, err
	}

	// Compile to SQL using the target database's SQL compiler
	var sqlBuf bytes.Buffer
	md, err := dbCtx.psqlCompiler.Compile(&sqlBuf, qc)
	if err != nil {
		return nil, fmt.Errorf("sql compile failed: %w", err)
	}

	// Get a connection from the target database pool
	conn, err := dbCtx.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	// Build argument list
	args, err := s.gj.argList(ctx, md, nil, s.r.requestconfig, false, dbCtx.psqlCompiler)
	if err != nil {
		return nil, fmt.Errorf("failed to build args: %w", err)
	}

	// Execute the query
	var data []byte
	querySQL, queryArgs, err := prepareQueryArgsForDB(dbCtx.dbtype, sqlBuf.String(), args.values)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare query args: %w", err)
	}

	cacheKey := s.dbFragmentKey(ctx, fragmentKindDBJoin, dbCtx.name, querySQL, queryArgs, qc)
	produce := func(c context.Context, useConn *sql.Conn) ([]byte, error) {
		raw, err := scanJSONRow(c, dbCtx.dbtype, useConn, nil, querySQL, queryArgs)
		if err == sql.ErrNoRows {
			if sel.Singular {
				raw = []byte(`{"` + sel.Table + `": null}`)
			} else {
				raw = []byte(`{"` + sel.Table + `": []}`)
			}
		} else if err != nil {
			return nil, fmt.Errorf("query execution failed: %w", err)
		}
		return raw, nil
	}

	if cached, ok := s.fragmentCacheGet(ctx, cacheKey, func() ([]byte, []RowRef, CacheEntryOptions, error) {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), swrRefreshTimeout)
		defer cancel()
		refreshConn, err := dbCtx.db.Conn(c)
		if err != nil {
			return nil, nil, CacheEntryOptions{}, err
		}
		defer refreshConn.Close() //nolint:errcheck
		data, err := produce(c, refreshConn)
		if err != nil {
			return nil, nil, CacheEntryOptions{}, err
		}
		cachedData, refs, err := s.processDBFragmentForCache(dbCtx.name, qc, data)
		return cachedData, refs, CacheEntryOptions{}, err
	}); ok {
		return cached, nil
	}

	start := time.Now()
	data, err = produce(ctx, conn)
	if err != nil {
		return nil, err
	}
	if cachedData, refs, perr := s.processDBFragmentForCache(dbCtx.name, qc, data); perr == nil {
		s.fragmentCacheSet(ctx, cacheKey, cachedData, refs, start, CacheEntryOptions{})
	}

	return data, nil
}

// buildChildGraphQLQuery constructs a GraphQL query for a cross-database child table.
// For example: query { orders(where: {user_id: {eq: 42}}) { id total items { name qty } } }
func buildChildGraphQLQuery(sel *qcode.Select, selects []qcode.Select, fkCol sdata.DBColumn, parentID []byte) []byte {
	var buf bytes.Buffer

	buf.WriteString("query { ")
	buf.WriteString(sel.Table)

	// Add WHERE filter on the FK column matching the parent ID
	buf.WriteString("(where: {")
	buf.WriteString(fkCol.Name)
	buf.WriteString(": {eq: ")
	writeGraphQLLiteral(&buf, fkCol, parentID)
	buf.WriteString("}})")

	// Write the requested fields
	buf.WriteString(" { ")
	writeSelectFields(&buf, sel, selects)
	buf.WriteString(" }")

	buf.WriteString(" }")
	return buf.Bytes()
}

func writeGraphQLLiteral(buf *bytes.Buffer, col sdata.DBColumn, val []byte) {
	s := strings.TrimSpace(string(val))
	if s == "" {
		buf.WriteString("null")
		return
	}

	if isGraphQLStringColumnType(col.Type) {
		writeGraphQLStringLiteral(buf, s)
		return
	}

	if isJSONStringLiteral(s) || isBareGraphQLLiteral(s) {
		buf.WriteString(s)
		return
	}

	writeGraphQLStringLiteral(buf, s)
}

func writeGraphQLStringLiteral(buf *bytes.Buffer, s string) {
	if isJSONStringLiteral(s) {
		buf.WriteString(s)
		return
	}

	b, err := json.Marshal(s)
	if err != nil {
		buf.WriteString("null")
		return
	}
	buf.Write(b)
}

func isJSONStringLiteral(s string) bool {
	if len(s) < 2 || s[0] != '"' {
		return false
	}

	var v string
	return json.Unmarshal([]byte(s), &v) == nil
}

func isBareGraphQLLiteral(s string) bool {
	switch s {
	case "null", "true", "false":
		return true
	}

	return isGraphQLNumberLiteral(s)
}

func isGraphQLNumberLiteral(s string) bool {
	if s == "" || strings.HasPrefix(s, "+") {
		return false
	}

	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case i == 0 && r == '-':
		case r == '.' || r == 'e' || r == 'E' || r == '+':
		default:
			return false
		}
	}

	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func isGraphQLStringColumnType(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}

	switch t {
	case "text", "varchar", "char", "character", "character varying",
		"uuid", "uniqueidentifier", "citext", "bpchar", "clob", "nclob",
		"date", "time", "timetz", "timestamp", "timestamptz", "datetime",
		"json", "jsonb", "xml",
		"string":
		return true
	}

	return strings.HasPrefix(t, "varchar") ||
		strings.HasPrefix(t, "nvarchar") ||
		strings.HasPrefix(t, "char") ||
		strings.HasPrefix(t, "nchar") ||
		strings.HasPrefix(t, "text") ||
		strings.HasPrefix(t, "ntext") ||
		strings.HasSuffix(t, "text") ||
		strings.HasPrefix(t, "time") ||
		strings.HasPrefix(t, "timestamp") ||
		strings.HasPrefix(t, "datetime")
}

// writeSelectFields writes the field list for a Select, recursing into children.
func writeSelectFields(buf *bytes.Buffer, sel *qcode.Select, selects []qcode.Select) {
	first := true
	for _, f := range sel.Fields {
		if !first {
			buf.WriteString(" ")
		}
		first = false
		buf.WriteString(f.FieldName)
	}

	// Recurse into child selects (nested relationships within the same target DB)
	for _, cid := range sel.Children {
		csel := &selects[cid]
		// Skip independent cross-database join children. When the parent is
		// already a database-join select, its children are passthrough fields for
		// the target database subquery and must be preserved.
		if sel.SkipRender != qcode.SkipTypeDatabaseJoin &&
			(csel.SkipRender == qcode.SkipTypeDatabaseJoin || csel.SkipRender == qcode.SkipTypeRemote) {
			continue
		}
		if !first {
			buf.WriteString(" ")
		}
		first = false
		buf.WriteString(csel.FieldName)
		buf.WriteString(" { ")
		writeSelectFields(buf, csel, selects)
		buf.WriteString(" }")
	}
}

// databaseJoinFieldIds finds fields that require cross-database joins.
func (s *gstate) databaseJoinFieldIds() ([][]byte, map[string]*qcode.Select, error) {
	if s.cs == nil || s.cs.st.qc == nil {
		return nil, nil, nil
	}

	selects := s.cs.st.qc.Selects

	// List of keys to extract from the db json response
	fm := make([][]byte, 0)

	// Mapping between extracted key and Select object
	sm := make(map[string]*qcode.Select)

	for i, sel := range selects {
		if sel.SkipRender != qcode.SkipTypeDatabaseJoin {
			continue
		}

		// The placeholder field name that was inserted during SQL generation
		placeholderKey := fmt.Sprintf("__%s_db_join", sel.FieldName)
		fm = append(fm, []byte(placeholderKey))
		sm[placeholderKey] = &selects[i]
	}

	return fm, sm, nil
}

// countDatabaseJoins returns the number of cross-database joins in a QCode.
func countDatabaseJoins(qc *qcode.QCode) int32 {
	var count int32
	for _, sel := range qc.Selects {
		if sel.SkipRender == qcode.SkipTypeDatabaseJoin {
			count++
		}
	}
	return count
}

// groupSelectsByDatabase groups root-level selects by their target database.
// This is used for parallel execution of queries to different databases.
type dbGroup struct {
	database string
	selects  []int32 // Indices into QCode.Selects
}

func (s *gstate) groupSelectsByDatabase() []dbGroup {
	if s.cs == nil || s.cs.st.qc == nil {
		return nil
	}

	qc := s.cs.st.qc
	byDB := make(map[string][]int32)

	// Group root selects by database
	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		db := sel.Database
		if db == "" {
			db = sel.Ti.Database
		}
		if db == "" {
			db = s.gj.defaultDB
		}
		byDB[db] = append(byDB[db], rootID)
	}

	// Convert map to slice
	groups := make([]dbGroup, 0, len(byDB))
	for db, sels := range byDB {
		groups = append(groups, dbGroup{database: db, selects: sels})
	}

	return groups
}

// dbResult holds the result from executing a query against one database.
type dbResult struct {
	database       string
	data           json.RawMessage
	fragmentHits   int64
	fragmentMisses int64
	err            error
}

// mergeRootResults merges results from multiple databases into a single JSON response.
// Root-level results are JSON objects that need to be combined.
func (s *gstate) mergeRootResults(results []dbResult) error {
	// Check for errors first
	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("database %s: %w", r.database, r.err)
		}
	}

	if len(results) == 0 {
		return nil
	}

	if len(results) == 1 {
		s.data = results[0].data
		return nil
	}

	// Merge multiple JSON objects into one
	// Each result is a JSON object like {"users": [...]}
	// We want to combine them into {"users": [...], "orders": [...], ...}
	merged := make(map[string]json.RawMessage)

	for _, r := range results {
		if len(r.data) == 0 {
			continue
		}

		var obj map[string]json.RawMessage
		if err := json.Unmarshal(r.data, &obj); err != nil {
			return fmt.Errorf("failed to parse result from %s: %w", r.database, err)
		}

		for k, v := range obj {
			if _, exists := merged[k]; exists {
				return fmt.Errorf("duplicate key '%s' in multi-database result", k)
			}
			merged[k] = v
		}
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("failed to marshal merged result: %w", err)
	}

	s.data = data
	return nil
}

// isMultiDB returns true if the engine is configured for multiple databases.
func (gj *graphjinEngine) isMultiDB() bool {
	return len(gj.databases) > 1
}

// buildDatabaseQuery creates a new GraphQL query containing only the specified root fields.
// It parses the original query, filters to include only the given fields, and reconstructs
// a valid GraphQL query string.
func (s *gstate) buildDatabaseQuery(rootFields []string) ([]byte, error) {
	op, err := graph.Parse(s.r.query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	// Build a set of allowed root field names
	allowed := make(map[string]bool, len(rootFields))
	for _, f := range rootFields {
		allowed[f] = true
	}

	// Find the root field IDs we want to keep
	keepFieldIDs := make(map[int32]bool)
	for _, f := range op.Fields {
		if f.ParentID == -1 && allowed[f.Name] {
			keepFieldIDs[f.ID] = true
			// Also mark all descendants
			markDescendants(op.Fields, f.ID, keepFieldIDs)
		}
	}

	// Reconstruct query with only the selected fields
	var buf bytes.Buffer

	// Write operation type and name
	switch op.Type {
	case graph.OpQuery:
		buf.WriteString("query")
	case graph.OpMutate:
		buf.WriteString("mutation")
	case graph.OpSub:
		buf.WriteString("subscription")
	}

	if op.Name != "" {
		buf.WriteString(" ")
		buf.WriteString(op.Name)
	}

	// Write variable definitions if any
	if len(op.VarDef) > 0 {
		buf.WriteString("(")
		for i, v := range op.VarDef {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString("$")
			buf.WriteString(v.Name)
			// We don't have type info, so we'll rely on the compiler to infer
		}
		buf.WriteString(")")
	}

	buf.WriteString(" { ")

	// Write the selected root fields
	writeFieldsRecursive(&buf, op.Fields, -1, keepFieldIDs)

	buf.WriteString(" }")

	return buf.Bytes(), nil
}

// markDescendants recursively marks all child field IDs
func markDescendants(fields []graph.Field, parentID int32, keep map[int32]bool) {
	for _, f := range fields {
		if f.ParentID == parentID {
			keep[f.ID] = true
			markDescendants(fields, f.ID, keep)
		}
	}
}

// writeFieldsRecursive writes fields that match the keepFieldIDs set
func writeFieldsRecursive(buf *bytes.Buffer, fields []graph.Field, parentID int32, keepFieldIDs map[int32]bool) {
	first := true
	for _, f := range fields {
		if f.ParentID != parentID || !keepFieldIDs[f.ID] {
			continue
		}

		if !first {
			buf.WriteString(" ")
		}
		first = false

		// Write alias if present
		if f.Alias != "" {
			buf.WriteString(f.Alias)
			buf.WriteString(": ")
		}

		// Write field name
		buf.WriteString(f.Name)

		// Write arguments if present
		if len(f.Args) > 0 {
			buf.WriteString("(")
			for i, arg := range f.Args {
				if i > 0 {
					buf.WriteString(", ")
				}
				buf.WriteString(arg.Name)
				buf.WriteString(": ")
				writeNode(buf, arg.Val)
			}
			buf.WriteString(")")
		}

		// Check if this field has children
		hasChildren := false
		for _, child := range fields {
			if child.ParentID == f.ID && keepFieldIDs[child.ID] {
				hasChildren = true
				break
			}
		}

		if hasChildren {
			buf.WriteString(" { ")
			writeFieldsRecursive(buf, fields, f.ID, keepFieldIDs)
			buf.WriteString(" }")
		}
	}
}

// writeNode writes a Node value to the buffer
func writeNode(buf *bytes.Buffer, n *graph.Node) {
	if n == nil {
		buf.WriteString("null")
		return
	}

	switch n.Type {
	case graph.NodeStr:
		buf.WriteString("\"")
		buf.WriteString(strings.ReplaceAll(n.Val, "\"", "\\\""))
		buf.WriteString("\"")
	case graph.NodeNum, graph.NodeBool:
		buf.WriteString(n.Val)
	case graph.NodeVar:
		buf.WriteString("$")
		buf.WriteString(n.Val)
	case graph.NodeLabel:
		buf.WriteString(n.Val)
	case graph.NodeList:
		buf.WriteString("[")
		for i, child := range n.Children {
			if i > 0 {
				buf.WriteString(", ")
			}
			writeNode(buf, child)
		}
		buf.WriteString("]")
	case graph.NodeObj:
		buf.WriteString("{")
		for i, child := range n.Children {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(child.Name)
			buf.WriteString(": ")
			writeNode(buf, child)
		}
		buf.WriteString("}")
	default:
		buf.WriteString(n.Val)
	}
}

// executeParallelRoots executes root-level queries to different databases in parallel
// and merges the results. This is called when a single GraphQL query references
// tables from multiple databases.
func (s *gstate) executeParallelRoots(c context.Context) error {
	if !s.multiDB || len(s.dbGroups) == 0 {
		return fmt.Errorf("executeParallelRoots called without multi-DB configuration")
	}

	var wg sync.WaitGroup
	results := make([]dbResult, len(s.dbGroups))

	i := 0
	for dbName, rootFields := range s.dbGroups {
		wg.Add(1)
		go func(idx int, db string, fields []string) {
			defer wg.Done()
			rootState := s.cloneForDatabaseRoot(db)

			ctx1, span := s.gj.spanStart(c, "Execute Parallel Root")
			span.SetAttributesString(StringAttr{"query.database", db})
			defer span.End()

			data, err := rootState.executeForDatabaseRoots(ctx1, db, fields)
			if err != nil {
				span.Error(err)
			}

			results[idx] = dbResult{
				database:       db,
				data:           data,
				fragmentHits:   rootState.fragmentHits.Load(),
				fragmentMisses: rootState.fragmentMisses.Load(),
				err:            err,
			}
		}(i, dbName, rootFields)
		i++
	}

	wg.Wait()
	for _, result := range results {
		if result.fragmentHits != 0 {
			s.fragmentHits.Add(result.fragmentHits)
		}
		if result.fragmentMisses != 0 {
			s.fragmentMisses.Add(result.fragmentMisses)
		}
	}
	return s.mergeRootResults(results)
}

// executeForDatabaseRoots builds a sub-query for the specified root fields,
// compiles it using the target database's compilers, and executes it.
func (s *gstate) executeForDatabaseRoots(ctx context.Context, dbName string, rootFields []string) (json.RawMessage, error) {
	// Get database context
	var db *sql.DB
	var qcodeCompiler *qcode.Compiler
	var psqlCompiler *psql.Compiler

	dbCtx, ok := s.gj.GetDatabase(dbName)
	if !ok {
		return nil, fmt.Errorf("database not found: %s", dbName)
	}
	db = dbCtx.db
	qcodeCompiler = dbCtx.qcodeCompiler
	psqlCompiler = dbCtx.psqlCompiler

	// Block mutations on read-only databases (absolute, independent of roles)
	if s.r.operation == qcode.QTMutation {
		if dbConf, ok := s.gj.conf.Databases[dbName]; ok && dbConf.ReadOnly {
			return nil, fmt.Errorf("mutations blocked: database %s is read-only", dbName)
		}
	}

	// Build a sub-query with only this database's root fields
	subQuery, err := s.buildDatabaseQuery(rootFields)
	if err != nil {
		return nil, fmt.Errorf("failed to build sub-query for %s: %w", dbName, err)
	}

	// Compile QCode
	var vars map[string]json.RawMessage
	if len(s.r.aschema) != 0 {
		vars = s.r.aschema
	} else {
		vars = s.vmap
	}

	qc, err := qcodeCompiler.Compile(subQuery, vars, s.role, s.r.namespace)
	if err != nil {
		return nil, fmt.Errorf("qcode compile failed for %s: %w", dbName, err)
	}

	if dbCtx.nano != nil {
		raw, err := s.renderNanoDBQuery(dbCtx, qc)
		if err != nil {
			return nil, fmt.Errorf("nanodb query failed for %s: %w", dbName, err)
		}
		encrypted, _, err := encryptResultFragment(raw, s.gj.printFormat, s.gj.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encryption failed for %s: %w", dbName, err)
		}
		return encrypted, nil
	}

	// Compile SQL
	var sqlBuf bytes.Buffer
	md, err := psqlCompiler.Compile(&sqlBuf, qc)
	if err != nil {
		return nil, fmt.Errorf("sql compile failed for %s: %w", dbName, err)
	}
	_ = md // metadata not used for now

	// Get connection
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection for %s: %w", dbName, err)
	}
	defer conn.Close() //nolint:errcheck

	// Build argument list
	args, err := s.gj.argList(ctx, md, vars, s.r.requestconfig, false, psqlCompiler)
	if err != nil {
		return nil, fmt.Errorf("failed to build args for %s: %w", dbName, err)
	}

	// Execute query
	var data []byte
	querySQL, queryArgs, err := prepareQueryArgsForDB(dbCtx.dbtype, sqlBuf.String(), args.values)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare query args for %s: %w", dbName, err)
	}

	cacheKey := s.dbFragmentKey(ctx, fragmentKindDBRoot, dbName, querySQL, queryArgs, qc)
	produce := func(c context.Context, useConn *sql.Conn) ([]byte, error) {
		raw, err := scanJSONRow(c, dbCtx.dbtype, useConn, nil, querySQL, queryArgs)
		if err == sql.ErrNoRows {
			return json.RawMessage(`{}`), nil
		}
		if err != nil {
			return nil, fmt.Errorf("query execution failed for %s: %w", dbName, err)
		}
		encrypted, _, err := encryptResultFragment(raw, s.gj.printFormat, s.gj.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encryption failed for %s: %w", dbName, err)
		}
		return encrypted, nil
	}

	if cached, ok := s.fragmentCacheGet(ctx, cacheKey, func() ([]byte, []RowRef, CacheEntryOptions, error) {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), swrRefreshTimeout)
		defer cancel()
		refreshConn, err := db.Conn(c)
		if err != nil {
			return nil, nil, CacheEntryOptions{}, err
		}
		defer refreshConn.Close() //nolint:errcheck
		data, err := produce(c, refreshConn)
		if err != nil {
			return nil, nil, CacheEntryOptions{}, err
		}
		cachedData, refs, err := s.processDBFragmentForCache(dbName, qc, data)
		return cachedData, refs, CacheEntryOptions{}, err
	}); ok {
		return s.resolveDatabaseRootRemotes(ctx, dbName, qc, cached)
	}

	start := time.Now()
	data, err = produce(ctx, conn)
	if err != nil {
		return nil, err
	}
	if cachedData, refs, perr := s.processDBFragmentForCache(dbName, qc, data); perr == nil {
		s.fragmentCacheSet(ctx, cacheKey, cachedData, refs, start, CacheEntryOptions{})
	}

	return s.resolveDatabaseRootRemotes(ctx, dbName, qc, data)
}

func (s *gstate) resolveDatabaseRootRemotes(
	ctx context.Context,
	dbName string,
	qc *qcode.QCode,
	data []byte,
) (json.RawMessage, error) {
	if qc == nil || qc.Remotes == 0 || len(data) == 0 {
		return json.RawMessage(data), nil
	}

	sub := gstate{
		gj:        s.gj,
		r:         cloneGraphqlReq(s.r),
		cs:        &cstate{st: stmt{qc: qc}},
		data:      injectRemoteMarkers(data, qc),
		role:      s.role,
		database:  dbName,
		skipCache: s.skipCache,
	}
	err := sub.execRemoteJoin(ctx)
	if hits := sub.fragmentHits.Load(); hits != 0 {
		s.fragmentHits.Add(hits)
	}
	if misses := sub.fragmentMisses.Load(); misses != 0 {
		s.fragmentMisses.Add(misses)
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(sub.data), nil
}
