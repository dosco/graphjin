package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
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

		// Extract parent ID value
		idVal := jsn.Value(id.Value)
		if len(idVal) == 0 {
			return nil, fmt.Errorf("invalid database join field id")
		}

		go func(n int, idVal []byte, sel *qcode.Select, dbCtx *dbContext, parentTable string) {
			defer wg.Done()

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

			// Filter to only requested fields if specified
			var ob bytes.Buffer
			if len(sel.Fields) != 0 {
				err = jsn.Filter(&ob, b, fieldsToList(sel.Fields))
				if err != nil {
					cerrMutex.Lock()
					cerr = fmt.Errorf("database join %s: %w", sel.Table, err)
					cerrMutex.Unlock()
					return
				}
			} else {
				ob.WriteString("null")
			}

			to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: ob.Bytes()}
		}(i, idVal, sel, dbCtx, p.Table)
	}

	wg.Wait()
	return to, cerr
}

// executeDatabaseJoinQuery executes a query against a target database for a cross-DB join.
func (s *gstate) executeDatabaseJoinQuery(
	ctx context.Context,
	dbCtx *dbContext,
	sel *qcode.Select,
	parentID []byte,
) ([]byte, error) {
	// Build the query for the child table with the parent ID filter
	// This is a simplified version - in a full implementation, we would
	// compile a proper QCode and SQL for the child query

	// For now, we'll use a simple approach: query the child table
	// filtered by the foreign key column
	var data []byte

	// Get a connection from the target database pool
	conn, err := dbCtx.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Build and execute query
	// Note: This is a placeholder - the actual implementation would need to:
	// 1. Build a proper QCode for the child query
	// 2. Compile it to SQL using the target database's compiler
	// 3. Execute with proper parameter handling

	// For the initial implementation, we return the ID as a placeholder
	// The full implementation would compile and execute the actual query
	data = parentID

	return data, nil
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
	database string
	data     json.RawMessage
	err      error
}

// executeMultiDBRoots executes root-level queries to different databases in parallel
// and merges the results.
func (s *gstate) executeMultiDBRoots(c context.Context) error {
	groups := s.groupSelectsByDatabase()

	if len(groups) <= 1 {
		// Single database or no groups - use standard execution
		return nil
	}

	var wg sync.WaitGroup
	results := make([]dbResult, len(groups))

	for i, group := range groups {
		wg.Add(1)
		go func(idx int, g dbGroup) {
			defer wg.Done()

			dbCtx, ok := s.gj.databases[g.database]
			if !ok {
				results[idx] = dbResult{
					database: g.database,
					err:      fmt.Errorf("database not found: %s", g.database),
				}
				return
			}

			// Execute query against this database
			// Note: This would need to compile a subset QCode for just this database's roots
			ctx1, span := s.gj.spanStart(c, "Execute Multi-DB Root")
			span.SetAttributesString(StringAttr{"query.database", g.database})

			data, err := s.executeForDatabase(ctx1, dbCtx, g.selects)
			span.End()

			results[idx] = dbResult{
				database: g.database,
				data:     data,
				err:      err,
			}
		}(i, group)
	}

	wg.Wait()
	return s.mergeRootResults(results)
}

// executeForDatabase executes a query against a specific database for the given selects.
func (s *gstate) executeForDatabase(
	ctx context.Context,
	dbCtx *dbContext,
	selectIDs []int32,
) (json.RawMessage, error) {
	// This is a placeholder - the full implementation would:
	// 1. Build a new QCode with only the specified selects
	// 2. Compile to SQL using dbCtx's compiler
	// 3. Execute against dbCtx's connection pool
	// 4. Return the JSON result

	conn, err := dbCtx.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection for %s: %w", dbCtx.name, err)
	}
	defer conn.Close()

	// Placeholder: return empty object
	return json.RawMessage(`{}`), nil
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

// getDBContext returns the database context for a given database name.
// If name is empty, returns the default database context.
func (gj *graphjinEngine) getDBContext(name string) (*dbContext, error) {
	if name == "" {
		name = gj.defaultDB
	}

	if gj.databases == nil || len(gj.databases) == 0 {
		// Single database mode - return nil to use legacy path
		return nil, nil
	}

	dbCtx, ok := gj.databases[name]
	if !ok {
		return nil, fmt.Errorf("database not found: %s", name)
	}

	return dbCtx, nil
}

// isMultiDB returns true if the engine is configured for multiple databases.
func (gj *graphjinEngine) isMultiDB() bool {
	return gj.databases != nil && len(gj.databases) > 0
}

// getConnection returns a database connection for the given database name.
// For single-database mode, it returns a connection from the default pool.
// For multi-database mode, it returns a connection from the specified database.
func (gj *graphjinEngine) getConnection(ctx context.Context, dbName string) (*sql.Conn, error) {
	if !gj.isMultiDB() {
		return gj.db.Conn(ctx)
	}

	dbCtx, err := gj.getDBContext(dbName)
	if err != nil {
		return nil, err
	}
	if dbCtx == nil {
		return gj.db.Conn(ctx)
	}

	return dbCtx.db.Conn(ctx)
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

			ctx1, span := s.gj.spanStart(c, "Execute Parallel Root")
			span.SetAttributesString(StringAttr{"query.database", db})
			defer span.End()

			data, err := s.executeForDatabaseRoots(ctx1, db, fields)
			if err != nil {
				span.Error(err)
			}

			results[idx] = dbResult{
				database: db,
				data:     data,
				err:      err,
			}
		}(i, dbName, rootFields)
		i++
	}

	wg.Wait()
	return s.mergeRootResults(results)
}

// executeForDatabaseRoots builds a sub-query for the specified root fields,
// compiles it using the target database's compilers, and executes it.
func (s *gstate) executeForDatabaseRoots(ctx context.Context, dbName string, rootFields []string) (json.RawMessage, error) {
	// Get database context
	var db *sql.DB
	var qcodeCompiler *qcode.Compiler
	var psqlCompiler *psql.Compiler

	if dbName == "_default" || dbName == s.gj.defaultDB || dbName == "" {
		// Use default compilers and connection
		db = s.gj.db
		qcodeCompiler = s.gj.qcodeCompiler
		psqlCompiler = s.gj.psqlCompiler
	} else {
		dbCtx, ok := s.gj.GetDatabase(dbName)
		if !ok {
			return nil, fmt.Errorf("database not found: %s", dbName)
		}
		db = dbCtx.db
		qcodeCompiler = dbCtx.qcodeCompiler
		psqlCompiler = dbCtx.psqlCompiler
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
	defer conn.Close()

	// Build argument list
	args, err := s.gj.argList(ctx, md, vars, s.r.requestconfig, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build args for %s: %w", dbName, err)
	}

	// Execute query
	var data []byte
	row := conn.QueryRowContext(ctx, sqlBuf.String(), args.values...)
	if err := row.Scan(&data); err != nil {
		if err == sql.ErrNoRows {
			return json.RawMessage(`{}`), nil
		}
		return nil, fmt.Errorf("query execution failed for %s: %w", dbName, err)
	}

	// Handle encryption if needed
	dhash := sha256.Sum256(data)
	data, err = encryptValues(data, s.gj.printFormat, decPrefix, dhash[:], s.gj.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encryption failed for %s: %w", dbName, err)
	}

	return json.RawMessage(data), nil
}
