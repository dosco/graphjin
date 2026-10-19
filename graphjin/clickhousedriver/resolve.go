package clickhousedriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
)

var debugSQL = os.Getenv("CH_DEBUG_SQL") != ""

// Statement is one SQL execution unit handed to an Executor.
type Statement struct {
	SQL  string
	Args []any
}

// ResultSet is the scanned rows an Executor returns.
type ResultSet struct {
	Rows []map[string]any
}

// Executor is the seam between the resolver and the database: production runs SQL
// on the wrapped clickhouse-go *sql.DB, tests supply a fake.
type Executor interface {
	Query(ctx context.Context, stmt Statement) (ResultSet, error)
	Exec(ctx context.Context, stmt Statement) error
}

type sqlExecutor struct{ db *sql.DB }

func (e *sqlExecutor) Query(ctx context.Context, stmt Statement) (ResultSet, error) {
	if debugSQL {
		fmt.Fprintf(os.Stderr, "CHSQL: %s | args=%v\n", stmt.SQL, stmt.Args)
	}
	rows, err := e.db.QueryContext(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return ResultSet{}, err
	}
	defer rows.Close()
	out, err := scanRows(rows)
	if err != nil {
		return ResultSet{}, err
	}
	return ResultSet{Rows: out}, nil
}

func (e *sqlExecutor) Exec(ctx context.Context, stmt Statement) error {
	_, err := e.db.ExecContext(ctx, stmt.SQL, stmt.Args...)
	return err
}

const (
	defaultChunkSize   = 100
	defaultConcurrency = 8
	defaultMaxDepth    = 20
)

// Resolver runs a parsed DSL against an Executor, assembling nested JSON via
// bounded, concurrency-capped N+1 (real SQL leaf reads, Go-side assembly).
type Resolver struct {
	Exec            Executor
	ChunkSize       int // max IN-list size per child query
	Concurrency     int // worker-pool cap for child chunk fetches
	MaxDepth        int
	DefaultDatabase string // qualifies nodes whose DSL omits a database
}

func (r *Resolver) chunkSize() int {
	if r.ChunkSize > 0 {
		return r.ChunkSize
	}
	return defaultChunkSize
}

func (r *Resolver) concurrency() int {
	if r.Concurrency > 0 {
		return r.Concurrency
	}
	return defaultConcurrency
}

func (r *Resolver) maxDepth() int {
	if r.MaxDepth > 0 {
		return r.MaxDepth
	}
	return defaultMaxDepth
}

// ResolveQuery executes the DSL and returns the JSON result GraphJin expects (one
// root object keyed by the field name).
func (r *Resolver) ResolveQuery(ctx context.Context, dsl *QueryDSL) ([]byte, error) {
	switch dsl.Operation {
	case OpQuery:
		return r.resolveRead(ctx, dsl.Root)
	case OpInsert, OpUpdate, OpDelete:
		return r.resolveWrite(ctx, dsl.Mutation)
	default:
		return nil, fmt.Errorf("clickhousedriver: resolve does not handle operation %q", dsl.Operation)
	}
}

func (r *Resolver) resolveRead(ctx context.Context, node *Node) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("clickhousedriver: query has no root node")
	}
	rows, err := r.fetchNode(ctx, node, nil)
	if err != nil {
		return nil, err
	}
	if err := r.attachChildren(ctx, node, rows, 1); err != nil {
		return nil, err
	}
	return marshalRoot(node, rows)
}

func marshalRoot(node *Node, rows []map[string]any) ([]byte, error) {
	var data any
	if node.Singular {
		if len(rows) > 0 {
			data = rows[0]
		}
	} else {
		data = rows
	}
	out := map[string]any{node.FieldName: data}
	if node.Keyset != nil && len(rows) > 0 {
		out[node.FieldName+"_cursor"] = encodeCursor(node.Keyset, rows[len(rows)-1])
	}
	return json.Marshal(out)
}

// fetchNode runs one node's SELECT (with optional extra predicates for a child
// IN-fetch) and tags __typename.
func (r *Resolver) fetchNode(ctx context.Context, node *Node, extra []Filter) ([]map[string]any, error) {
	db := node.Database
	if db == "" {
		db = r.DefaultDatabase
	}
	var sqlStr string
	var args []any
	var err error
	// A limited one-to-many child needs a per-parent limit (window), not a plain
	// LIMIT that would cap the whole IN-chunk.
	if node.Rel != nil && node.Rel.Kind == RelOneToMany && node.Limit > 0 && len(extra) > 0 && len(node.Aggregates) == 0 {
		sqlStr, args, err = BuildWindowedChildSelect(node, db, extra)
	} else {
		sqlStr, args, err = BuildSelect(node, db, extra)
	}
	if err != nil {
		return nil, err
	}
	rs, err := r.Exec.Query(ctx, Statement{SQL: sqlStr, Args: args})
	if err != nil {
		return nil, err
	}
	if node.Typename != "" {
		for _, row := range rs.Rows {
			row["__typename"] = node.Typename
		}
	}
	return rs.Rows, nil
}

// attachChildren fetches and attaches every child relationship onto rows.
func (r *Resolver) attachChildren(ctx context.Context, node *Node, rows []map[string]any, depth int) error {
	if len(rows) == 0 || len(node.Children) == 0 {
		return nil
	}
	if depth > r.maxDepth() {
		return fmt.Errorf("clickhousedriver: nested query exceeds max depth %d", r.maxDepth())
	}
	for _, child := range node.Children {
		if err := r.attachChild(ctx, child, rows, depth); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) attachChild(ctx context.Context, child *Node, parentRows []map[string]any, depth int) error {
	if child.Rel == nil || child.Rel.ParentCol == "" || child.Rel.ChildCol == "" {
		return fmt.Errorf("clickhousedriver: child %q is missing relationship metadata", child.FieldName)
	}
	kind := child.Rel.Kind
	if kind == "" {
		kind = RelOneToMany
	}

	// Collect distinct parent join values.
	seen := make(map[string]bool)
	var values []any
	for _, pr := range parentRows {
		v, ok := pr[child.Rel.ParentCol]
		if !ok || v == nil {
			continue
		}
		k := fmt.Sprint(v)
		if !seen[k] {
			seen[k] = true
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		for _, pr := range parentRows {
			pr[child.FieldName] = emptyValue(kind)
		}
		return nil
	}

	childRows, err := r.fetchChildChunks(ctx, child, values)
	if err != nil {
		return err
	}

	// Recurse before grouping so grandchildren are assembled too.
	if err := r.attachChildren(ctx, child, childRows, depth+1); err != nil {
		return err
	}

	groups := make(map[string][]map[string]any)
	for _, cr := range childRows {
		k := fmt.Sprint(cr[child.Rel.ChildCol])
		groups[k] = append(groups[k], cr)
	}

	for _, pr := range parentRows {
		v, ok := pr[child.Rel.ParentCol]
		if !ok || v == nil {
			pr[child.FieldName] = emptyValue(kind)
			continue
		}
		matched := groups[fmt.Sprint(v)]
		if kind == RelOneToOne {
			if len(matched) > 0 {
				pr[child.FieldName] = matched[0]
			} else {
				pr[child.FieldName] = nil
			}
		} else {
			if matched == nil {
				matched = []map[string]any{}
			}
			pr[child.FieldName] = matched
		}
	}
	return nil
}

func emptyValue(kind string) any {
	if kind == RelOneToOne {
		return nil
	}
	return []map[string]any{}
}

// fetchChildChunks fetches child rows for all parent join values via chunked IN
// queries, capped by a worker pool. Results are returned in chunk order so
// assembly is deterministic.
func (r *Resolver) fetchChildChunks(ctx context.Context, child *Node, values []any) ([]map[string]any, error) {
	child = ensureJoinColumn(child)
	chunks := chunk(values, r.chunkSize())

	results := make([][]map[string]any, len(chunks))
	errs := make([]error, len(chunks))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, r.concurrency())
	var wg sync.WaitGroup
	for i, ch := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ch []any) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			rows, err := r.fetchNode(ctx, child, []Filter{{Col: child.Rel.ChildCol, Op: OpIn, Value: ch}})
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			results[i] = rows
		}(i, ch)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	var out []map[string]any
	for _, rs := range results {
		out = append(out, rs...)
	}
	return out, nil
}

// ensureJoinColumn guarantees the child's join column is selected so grouping works.
func ensureJoinColumn(child *Node) *Node {
	if slices.Contains(child.Columns, child.Rel.ChildCol) {
		return child
	}
	clone := *child
	clone.Columns = append(append([]string{}, child.Columns...), child.Rel.ChildCol)
	return &clone
}

func chunk(vals []any, size int) [][]any {
	if size <= 0 {
		size = defaultChunkSize
	}
	var out [][]any
	for i := 0; i < len(vals); i += size {
		end := min(i+size, len(vals))
		out = append(out, vals[i:end])
	}
	return out
}

// resolveWrite executes a single-table mutation. INSERT is read-after-write via
// the returning select; UPDATE is a synchronous ALTER mutation; DELETE is
// lightweight and returns the pre-image.
func (r *Resolver) resolveWrite(ctx context.Context, m *Mutation) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("clickhousedriver: mutation has no body")
	}
	db := m.Database
	if db == "" {
		db = r.DefaultDatabase
	}
	m.applyRawDoc()

	// DELETE returns the pre-image, so read before the write.
	if m.Type == OpDelete {
		var pre []byte
		if m.Returning != nil {
			d, err := r.resolveRead(ctx, m.Returning)
			if err != nil {
				return nil, err
			}
			pre = d
		}
		sqlStr, args, err := BuildDelete(m, db)
		if err != nil {
			return nil, err
		}
		if err := r.Exec.Exec(ctx, Statement{SQL: sqlStr, Args: args}); err != nil {
			return nil, err
		}
		if pre == nil {
			return []byte("null"), nil
		}
		return pre, nil
	}

	var (
		sqlStr string
		args   []any
		err    error
	)
	switch m.Type {
	case OpInsert:
		sqlStr, args, err = BuildInsert(m, db)
	case OpUpdate:
		sqlStr, args, err = BuildUpdate(m, db)
	default:
		return nil, fmt.Errorf("clickhousedriver: unsupported mutation type %q", m.Type)
	}
	if err != nil {
		return nil, err
	}
	if err := r.Exec.Exec(ctx, Statement{SQL: sqlStr, Args: args}); err != nil {
		return nil, err
	}

	if m.Returning == nil {
		return []byte("null"), nil
	}
	return r.resolveRead(ctx, m.Returning)
}
