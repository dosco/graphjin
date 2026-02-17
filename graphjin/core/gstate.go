package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

type gstate struct {
	gj    *graphjinEngine
	r     GraphqlReq
	cs    *cstate
	vmap  map[string]json.RawMessage
	data  []byte
	dhash [sha256.Size]byte
	role  string
	verrs []qcode.ValidErr
	// database is the target database name for multi-database support.
	// Empty string means default database (backward compatible single-DB mode).
	database string
	// multiDB is true when the query spans multiple databases and requires
	// parallel execution with result merging.
	multiDB bool
	// dbGroups maps database names to their root field names for multi-DB queries.
	// Only populated when multiDB is true.
	dbGroups map[string][]string

	// Cache-related fields
	cacheKey     string    // Cache key for this query
	queryStarted time.Time // When query started (for race condition detection)
	cacheHit     bool      // True if response was served from cache
	skipCache    bool      // True if caching should be skipped for this query
}

type cstate struct {
	sync.Once
	st  stmt
	err error
}

type stmt struct {
	role string
	roc  *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func newGState(c context.Context, gj *graphjinEngine, r GraphqlReq) (s gstate, err error) {
	s.gj = gj
	s.r = r

	if v, ok := c.Value(UserRoleKey).(string); ok {
		s.role = v
	} else {
		switch c.Value(UserIDKey).(type) {
		case string, int:
			s.role = "user"
		default:
			s.role = "anon"
		}
	}

	// convert variable json to a go map also decrypted encrypted values
	if len(r.vars) != 0 {
		var vars json.RawMessage
		vars, err = decryptValues(r.vars, decPrefix, s.gj.encryptionKey)
		if err != nil {
			return
		}

		s.vmap = make(map[string]json.RawMessage, 5)
		if err = json.Unmarshal(vars, &s.vmap); err != nil {
			return
		}
	}
	return
}

func (s *gstate) compile() (err error) {
	if !s.gj.prodSec {
		err = s.compileQueryForRole()
		return
	}

	// In production mode and compile and cache the result
	// In production mode the query is derived from the allow list
	err = s.compileQueryForRoleOnce()
	return
}

func (s *gstate) compileQueryForRoleOnce() (err error) {
	val, loaded := s.gj.queries.LoadOrStore(s.key(), &cstate{})
	s.cs = val.(*cstate)

	if !loaded {
		s.cs.Do(func() {
			s.cs.err = s.compileQueryForRole()
		})
	}

	err = s.cs.err
	return
}

func (s *gstate) compileQueryForRole() (err error) {
	st := stmt{role: s.role}

	var ok bool
	if st.roc, ok = s.gj.roles[s.role]; !ok {
		err = fmt.Errorf(`roles '%s' not defined in c.gj.config`, s.role)
		return
	}

	var vars map[string]json.RawMessage
	if len(s.r.aschema) != 0 { // compile in prod (once)
		vars = s.r.aschema
	} else { // compiling in dev
		vars = s.vmap
	}

	// Multi-DB mode: check if query spans multiple databases
	if s.gj.isMultiDB() {
		roots := s.extractAllRootFields()
		if len(roots) > 0 {
			byDB := s.groupRootsByDatabase(roots)

			// Multiple databases - mark for parallel execution
			if len(byDB) > 1 {
				s.multiDB = true
				s.dbGroups = byDB
				// Store role info for parallel execution
				if s.cs == nil {
					s.cs = &cstate{st: st}
				} else {
					s.cs.st = st
				}
				// Compilation will be done per-database in executeParallelRoots
				return nil
			}

			// Single database - use that database's compilers
			for db := range byDB {
				if db != s.gj.defaultDB {
					dbCtx, found := s.gj.GetDatabase(db)
					if !found {
						err = fmt.Errorf("database not found: %s", db)
						return
					}
					return s.compileForDatabase(st, vars, dbCtx)
				}
			}
		}
	}

	// Default path: compile with default (primary) database's compilers
	pdb := s.gj.primaryDB()
	return s.compileWithCompilers(st, vars, pdb.qcodeCompiler, pdb.psqlCompiler, "")
}

// compileForDatabase compiles the query using a specific database's compilers.
func (s *gstate) compileForDatabase(st stmt, vars map[string]json.RawMessage, dbCtx *dbContext) (err error) {
	s.database = dbCtx.name
	return s.compileWithCompilers(st, vars, dbCtx.qcodeCompiler, dbCtx.psqlCompiler, dbCtx.name)
}

// compileWithCompilers performs the actual compilation with the given compilers.
func (s *gstate) compileWithCompilers(st stmt, vars map[string]json.RawMessage, qcc *qcode.Compiler, pc *psql.Compiler, dbName string) (err error) {
	if st.qc, err = qcc.Compile(
		s.r.query,
		vars,
		s.role,
		s.r.namespace); err != nil {
		return
	}

	var w bytes.Buffer
	if st.md, err = pc.Compile(&w, st.qc); err != nil {
		return
	}

	st.sql = w.String()
	s.database = dbName

	if s.cs == nil {
		s.cs = &cstate{st: st}
	} else {
		s.cs.st = st
	}

	return
}

// extractAllRootFields parses the GraphQL query to extract all root field names
// without performing schema validation. Uses the graph parser for robust parsing.
func (s *gstate) extractAllRootFields() []string {
	op, err := graph.Parse(s.r.query)
	if err != nil {
		return nil
	}

	var roots []string
	for _, f := range op.Fields {
		// Root fields have ParentID == -1
		if f.ParentID == -1 && f.Type != graph.FieldKeyword {
			roots = append(roots, f.Name)
		}
	}
	return roots
}

// groupRootsByDatabase maps root field names to their target databases.
// Returns a map of database name to list of root field names.
// After normalization, gj.defaultDB is always set and all tables have Database set.
func (s *gstate) groupRootsByDatabase(roots []string) map[string][]string {
	byDB := make(map[string][]string)

	for _, root := range roots {
		db := s.gj.defaultDB

		// Look up the table's database from config
		for _, t := range s.gj.conf.Tables {
			if t.Name == root && t.Database != "" {
				db = t.Database
				break
			}
		}
		byDB[db] = append(byDB[db], root)
	}
	return byDB
}


// getTargetDBCtx returns the dbContext for the target database.
// If s.database is set, returns that database's context.
// Otherwise returns the default database context.
func (s *gstate) getTargetDBCtx() *dbContext {
	name := s.database
	if name == "" {
		name = s.gj.defaultDB
	}
	ctx, _ := s.gj.GetDatabase(name)
	return ctx
}

// getTargetPsqlCompiler returns the psql compiler for the target database.
func (s *gstate) getTargetPsqlCompiler() *psql.Compiler {
	return s.getTargetDBCtx().psqlCompiler
}

// getTargetDB returns the *sql.DB for the target database.
// If s.database is set (non-default database), returns that database's connection.
// Otherwise returns the default database connection.
func (s *gstate) getTargetDB() *sql.DB {
	return s.getTargetDBCtx().db
}

func (s *gstate) compileAndExecuteWrapper(c context.Context) (err error) {
	// Record query start time for cache race condition detection
	s.queryStarted = time.Now()

	// Try cache lookup for queries (before compilation)
	if s.gj.responseCache != nil && s.r.operation == qcode.QTQuery {
		if s.tryCacheGet(c) {
			return nil
		}
	}

	// Check for multi-database queries BEFORE compilation
	// This is done by parsing root fields without schema validation
	if s.gj.isMultiDB() {
		roots := s.extractAllRootFields()
		if len(roots) > 0 {
			byDB := s.groupRootsByDatabase(roots)
			if len(byDB) > 1 {
				// Multi-database parallel execution path
				s.multiDB = true
				s.dbGroups = byDB
				if err = s.executeParallelRoots(c); err != nil {
					return
				}
				return
			}
		}
	}

	// Single database execution path (handles compilation internally)
	if err = s.compileAndExecute(c); err != nil {
		return
	}

	if s.gj.conf.Debug {
		s.debugLogStmt()
	}

	if len(s.data) == 0 {
		return
	}

	cs := s.cs

	// Handle remote joins (HTTP calls to external APIs)
	if cs.st.qc.Remotes != 0 {
		if err = s.execRemoteJoin(c); err != nil {
			return
		}
	}

	// Handle cross-database joins (in-process calls to other databases)
	if s.gj.isMultiDB() && countDatabaseJoins(cs.st.qc) > 0 {
		if err = s.execDatabaseJoins(c); err != nil {
			return
		}
	}

	// Cache the response for queries, or invalidate cache for mutations
	if s.gj.responseCache != nil {
		if s.r.operation == qcode.QTQuery && !s.skipCache {
			s.tryCacheSet(c)
		} else if s.r.operation != qcode.QTQuery {
			s.invalidateCache(c)
		}
	}

	return
}

func (s *gstate) compileAndExecute(c context.Context) (err error) {
	if s.gj.conf.MockDB {
		// compile query for the role
		if err = s.compile(); err != nil {
			return
		}

		// set default variables
		s.setDefaultVars()

		// execute query
		err = s.executeMock(c)
		return
	}

	var defaultConn *sql.Conn

	// For ABAC, we need to execute role query first using default database
	if s.role == "user" && s.gj.abacEnabled && s.tx() == nil {
		c1, span1 := s.gj.spanStart(c, "Get Default Connection for ABAC")
		defer span1.End()

		err = retryOperation(c1, func() (err1 error) {
			defaultConn, err1 = s.gj.primaryDB().db.Conn(c1)
			return
		})
		if err != nil {
			span1.Error(err)
			return
		}
		defer defaultConn.Close() //nolint:errcheck

		if err = s.executeRoleQuery(c, defaultConn); err != nil {
			return
		}
	}

	// Compile query for the role (this also determines target database for multi-DB)
	if err = s.compile(); err != nil {
		return
	}

	// set default variables
	s.setDefaultVars()

	var conn *sql.Conn

	if s.tx() == nil {
		// get a database connection from the target database
		c1, span1 := s.gj.spanStart(c, "Get Connection")
		defer span1.End()

		db := s.getTargetDB()
		err = retryOperation(c1, func() (err1 error) {
			conn, err1 = db.Conn(c1)
			return
		})
		if err != nil {
			span1.Error(err)
			return
		}
		defer conn.Close() //nolint:errcheck
	}

	// set the local user id on the connection if needed
	if s.gj.conf.SetUserID {
		c1, span2 := s.gj.spanStart(c, "Set Local User ID")
		defer span2.End()

		err = retryOperation(c1, func() (err1 error) {
			return s.setLocalUserID(c1, conn)
		})
		if err != nil {
			span2.Error(err)
			return
		}
	}

	// execute query
	err = s.execute(c, conn)
	return
}

func (s *gstate) setDefaultVars() {
	if vlen := len(s.cs.st.qc.Vars); vlen != 0 && s.vmap == nil {
		s.vmap = make(map[string]json.RawMessage, vlen)
	}


	for _, v := range s.cs.st.qc.Vars {
		s.vmap[v.Name] = v.Val
	}
}

func (s *gstate) execute(c context.Context, conn *sql.Conn) (err error) {



	if err = s.validateAndUpdateVars(c); err != nil {
		return
	}

	var args args
	if args, err = s.argList(c); err != nil {
		return
	}

	cs := s.cs

    // Use Dialect to check for multi-statement scripts (e.g., SQLite)
    dialect := s.getTargetPsqlCompiler().GetDialect()
    parts := dialect.SplitQuery(cs.st.sql)

    if len(parts) > 1 {
        // Multi-statement script execution
        c1, span := s.gj.spanStart(c, "Execute Script")
        defer span.End()

        argIdx := 0
        		for i, stmt := range parts {
			// Count parameters (?) in this statement to slice arguments
			nParams := strings.Count(stmt, "?")
			var stmtArgs []interface{}
			
			if nParams > 0 {
				if argIdx+nParams > len(args.values) {
					span.Error(fmt.Errorf("script: not enough arguments for statement %d", i))
					return fmt.Errorf("script: not enough arguments")
				}
				stmtArgs = args.values[argIdx : argIdx+nParams]
				argIdx += nParams
			}

			upperStmt := strings.ToUpper(strings.TrimSpace(stmt))

			isReturning := strings.Contains(upperStmt, "RETURNING")
			isSelect := (strings.HasPrefix(upperStmt, "SELECT") && !strings.Contains(upperStmt, " INTO ")) || strings.HasPrefix(upperStmt, "WITH")
			
			// Check for @gj_ids hint
			gjIdsHint := strings.Index(stmt, "-- @gj_ids=")
			var gjIdsKey string
			if gjIdsHint != -1 {
				// Parse key: -- @gj_ids=users_0;
				remainder := stmt[gjIdsHint+11:]
				if idx := strings.Index(remainder, ";"); idx != -1 {
					gjIdsKey = strings.TrimSpace(remainder[:idx])
				} else {
					gjIdsKey = strings.TrimSpace(remainder)
				}
			}


			if gjIdsKey != "" {
				// Bulk Capture Path for SQLite (handles RETURNING and SELECT)
				var rows *sql.Rows
				var err1 error
				if tx := s.tx(); tx != nil {
					rows, err1 = tx.QueryContext(c1, stmt, stmtArgs...)
				} else {
					err1 = retryOperation(c1, func() (err2 error) {
						rows, err2 = conn.QueryContext(c1, stmt, stmtArgs...)
						return
					})
				}
				if err1 != nil {
					err = err1 // Propagate error
				} else {
					defer rows.Close() //nolint:errcheck

					var ids []string
					
					for rows.Next() {
						var b []byte
						if err = rows.Scan(&b); err != nil {
							return err
						}
						// b is JSON object from RETURNING json_object(...)
						
						// Parse ID from JSON
						var rowMap map[string]interface{}
						if err = json.Unmarshal(b, &rowMap); err != nil {
							return err
						}
						
						if idVal, ok := rowMap["id"]; ok {
							ids = append(ids, fmt.Sprintf("%v", idVal))
						}
					}
					
					if err = rows.Err(); err != nil {
						return err
					}
					
					// Note: We do NOT set s.data here - the final SELECT will set the response
					// We only capture IDs into _gj_ids for the scoping CTE
					
					// Insert captured IDs into _gj_ids
					if len(ids) > 0 {
						var ib strings.Builder
						ib.WriteString(`INSERT OR IGNORE INTO _gj_ids (k, id) VALUES `)
						for k, id := range ids {
							if k > 0 {
								ib.WriteString(", ")
							}
							ib.WriteString(fmt.Sprintf("('%s', %s)", gjIdsKey, id))
						}
						insertSQL := ib.String()

						if tx := s.tx(); tx != nil {
							_, err = tx.ExecContext(c1, insertSQL)
						} else {
							_, err = conn.ExecContext(c1, insertSQL)
						}
					}
				}
			} else if isReturning || isSelect {
                // Statement returns data (e.g. INSERT ... RETURNING or SELECT ...)
                var row *sql.Row
                if tx := s.tx(); tx != nil {
                    row = tx.QueryRowContext(c1, stmt, stmtArgs...)
                    err = row.Scan(&s.data)
                } else {
                    err = retryOperation(c1, func() (err1 error) {
                        row = conn.QueryRowContext(c1, stmt, stmtArgs...)
                        return row.Scan(&s.data)
                    })
                }

            } else {
                // Intermediate statement: Use Exec
                if tx := s.tx(); tx != nil {
                    _, err = tx.ExecContext(c1, stmt, stmtArgs...)
                } else {
                    err = retryOperation(c1, func() (err1 error) {
                        _, err1 = conn.ExecContext(c1, stmt, stmtArgs...)
                        return
                    })
                }
            }

            if err != nil {
                 if err != sql.ErrNoRows {
                    span.Error(err)
                 }
                 return
            }
        }
        
        if err == nil {
            s.dhash = sha256.Sum256(s.data)
            s.data, err = encryptValues(s.data,
                s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
        }
        return
    }

    // Standard Single-Statement Execution
	c1, span := s.gj.spanStart(c, "Execute Query")
	defer span.End()

	var row *sql.Row
	if tx := s.tx(); tx != nil {
		row = tx.QueryRowContext(c1, cs.st.sql, args.values...)
		err = row.Scan(&s.data)
	} else {
		err = retryOperation(c1, func() (err1 error) {
			row = conn.QueryRowContext(c1, cs.st.sql, args.values...)
			return row.Scan(&s.data)
		})
	}

	if err != nil && err != sql.ErrNoRows {
		span.Error(err)
	}

	if span.IsRecording() {
		attrs := []StringAttr{
			{"query.namespace", s.r.namespace},
			{"query.operation", cs.st.qc.Type.String()},
			{"query.name", cs.st.qc.Name},
			{"query.role", cs.st.role},
		}
		// Add database attribute for multi-database observability
		if s.database != "" {
			attrs = append(attrs, StringAttr{"query.database", s.database})
		}
		span.SetAttributesString(attrs...)
	}

	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return
	}

	s.dhash = sha256.Sum256(s.data)

	s.data, err = encryptValues(s.data,
		s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)

	return
}

func (s *gstate) executeRoleQuery(c context.Context, conn *sql.Conn) (err error) {
	s.role, err = s.gj.executeRoleQuery(c, conn, s.vmap, s.r.requestconfig)
	return
}

func (s *gstate) argList(c context.Context) (args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, s.vmap, s.r.requestconfig, false, s.getTargetPsqlCompiler())
	return
}

func (s *gstate) argListForSub(c context.Context,
	vmap map[string]json.RawMessage,
) (args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, vmap, s.r.requestconfig, true, s.getTargetPsqlCompiler())
	return
}

func (s *gstate) setLocalUserID(c context.Context, conn *sql.Conn) (err error) {
	if v := c.Value(UserIDKey); v == nil {
		return nil
	} else {
		var val string
		switch v1 := v.(type) {
		case string:
			val = v1
		case int:
			val = strconv.Itoa(v1)
		}
		
		q := s.getTargetPsqlCompiler().RenderSetSessionVar("user.id", val)
		if q == "" {
			return nil
		}

		if tx := s.tx(); tx != nil {
			_, err = tx.ExecContext(c, q)
		} else {
			_, err = conn.ExecContext(c, q)
		}
	}
	return
}

var errValidationFailed = errors.New("validation failed")

func (s *gstate) validateAndUpdateVars(c context.Context) (err error) {
	cs := s.cs
	qc := cs.st.qc

	if qc == nil {
		return nil
	}

	if len(qc.Consts) != 0 {
		s.verrs = qc.ProcessConstraints(s.vmap)
		if len(s.verrs) != 0 {
			err = errValidationFailed
			return
		}
	}
	return
}

func (s *gstate) sql() (sql string) {
	if s.cs != nil && s.cs.st.qc != nil {
		sql = s.cs.st.sql
	}
	return
}

func (s *gstate) cacheHeader() (ch string) {
	if s.cs != nil && s.cs.st.qc != nil {
		ch = s.cs.st.qc.Cache.Header
	}
	return
}

func (s *gstate) qcode() (qc *qcode.QCode) {
	if s.cs != nil {
		qc = s.cs.st.qc
	}
	return
}

func (s *gstate) tx() (tx *sql.Tx) {
	if s.r.requestconfig != nil {
		tx = s.r.requestconfig.Tx
	}
	return
}

func (s *gstate) key() (key string) {
	// CRITICAL: Include database in cache key to prevent cross-database cache collisions.
	// Same query name with different databases must have different cache entries.
	if s.multiDB && len(s.dbGroups) > 0 {
		// For multi-DB queries, include sorted list of ALL databases
		dbs := make([]string, 0, len(s.dbGroups))
		for db := range s.dbGroups {
			dbs = append(dbs, db)
		}
		sort.Strings(dbs)
		key = s.r.namespace + s.r.name + s.role + strings.Join(dbs, ",")
	} else {
		key = s.r.namespace + s.r.name + s.role + s.database
	}
	return
}

// tryCacheGet attempts to retrieve the response from cache.
// Returns true if cache hit (s.data is populated), false otherwise.
func (s *gstate) tryCacheGet(c context.Context) bool {
	if s.gj.responseCache == nil || s.gj.cacheKeyBuilder == nil {
		return false
	}

	// Build cache key
	s.cacheKey = s.gj.cacheKeyBuilder.Build(c, s.r.name, s.getAPQKey(), s.r.query, s.r.vars, s.role)

	// Skip if anonymous query (no operation name or APQ key)
	if s.cacheKey == "" || !s.gj.cacheKeyBuilder.ShouldCache(s.r.name, s.getAPQKey()) {
		s.skipCache = true
		return false
	}

	// Try to get from cache
	data, isStale, found := s.gj.responseCache.Get(c, s.cacheKey)
	if !found {
		return false
	}

	// Cache hit - populate response data
	s.data = data
	s.cacheHit = true

	// TODO: Handle SWR (stale-while-revalidate) for isStale == true
	_ = isStale

	return true
}

// tryCacheSet stores the response in cache with row-level indices.
func (s *gstate) tryCacheSet(c context.Context) {
	if s.gj.responseCache == nil || s.cacheKey == "" || len(s.data) == 0 || s.cacheHit {
		return
	}

	cs := s.cs
	if cs == nil || cs.st.qc == nil {
		return
	}

	qc := cs.st.qc

	// Skip caching for offset-based pagination (pages shift on insert/delete)
	if s.hasOffsetPagination(qc) {
		return
	}

	// Skip caching for responses that are too large
	if len(s.data) > maxResponseSize {
		return
	}

	// Process response to extract row refs and clean __gj_id fields
	processor := NewResponseProcessor(qc)
	cleaned, refs, err := processor.ProcessForCache(s.data)
	if err != nil {
		return
	}

	// Store in cache
	_ = s.gj.responseCache.Set(c, s.cacheKey, cleaned, refs, s.queryStarted)
}

// invalidateCache invalidates cache entries for rows affected by a mutation.
func (s *gstate) invalidateCache(c context.Context) {
	if s.gj.responseCache == nil || len(s.data) == 0 {
		return
	}

	cs := s.cs
	if cs == nil || cs.st.qc == nil {
		return
	}

	// Extract affected row IDs from mutation response
	refs := ExtractMutationRefs(cs.st.qc, s.data)
	if len(refs) > 0 {
		_ = s.gj.responseCache.InvalidateRows(c, refs)
	}
}


// getAPQKey returns the APQ key if one was provided in the request.
func (s *gstate) getAPQKey() string {
	if s.r.requestconfig != nil && s.r.requestconfig.APQKey != "" {
		return s.r.requestconfig.APQKey
	}
	return ""
}

// hasOffsetPagination checks if any selection uses offset-based pagination.
// Offset pagination is not cacheable because pages shift on insert/delete.
func (s *gstate) hasOffsetPagination(qc *qcode.QCode) bool {
	for i := range qc.Selects {
		sel := &qc.Selects[i]
		if sel.Paging.Type == qcode.PTOffset && sel.Paging.Offset > 0 {
			return true
		}
	}
	return false
}

// maxResponseSize is the maximum response size to cache (1MB)
const maxResponseSize = 1 << 20
