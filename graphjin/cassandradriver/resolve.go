package cassandradriver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// Statement is one CQL execution unit handed to an Executor.
type Statement struct {
	CQL        string
	Args       []any
	PageSize   int
	PageState  []byte
	Idempotent bool
}

// ResultSet is the rows (column name -> Go value) and next page-state an Executor
// returns.
type ResultSet struct {
	Rows      []map[string]any
	PageState []byte
}

// Executor is the seam between the pure planner/assembler and the real database.
// M1 tests supply a fake; M2 implements it over *gocql.Session.
type Executor interface {
	Query(ctx context.Context, stmt Statement) (ResultSet, error)
	Exec(ctx context.Context, stmt Statement) (ResultSet, error)
}

const (
	defaultChunkSize   = 100
	defaultConcurrency = 8
	defaultMaxDepth    = 20
)

// Resolver runs a parsed DSL against an Executor, assembling nested JSON via
// bounded, concurrency-capped N+1 by partition key.
type Resolver struct {
	Exec            Executor
	ChunkSize       int // max IN-list size per child query
	Concurrency     int // worker-pool cap for child chunk fetches
	MaxDepth        int
	DefaultKeyspace string // qualifies tables whose DSL node omits a keyspace
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

// deriveCardinality decides a relationship's cardinality from the child's key
// roles, and rejects join columns that aren't the child's full partition key (an
// N+1 IN fetch on such a column would be a cross-partition scan).
func deriveCardinality(childCol string, partitionKeys, clusteringKeys []string) (string, error) {
	if len(partitionKeys) == 1 && partitionKeys[0] == childCol {
		if len(clusteringKeys) == 0 {
			return RelOneToOne, nil
		}
		return RelOneToMany, nil
	}
	return "", fmt.Errorf("cassandradriver: relationship join column %q is not the child's full partition key %v — "+
		"an N+1 IN fetch would be a cross-partition scan", childCol, partitionKeys)
}

// ResolveQuery is the driver entrypoint: it executes the DSL and returns the JSON
// result GraphJin expects (one root object keyed by the field name).
func (r *Resolver) ResolveQuery(ctx context.Context, dsl *QueryDSL) ([]byte, error) {
	switch dsl.Operation {
	case OpQuery:
		return r.resolveRead(ctx, dsl.Root)
	case OpInsert, OpUpsert, OpUpdate, OpDelete:
		return r.resolveWrite(ctx, dsl.Mutation)
	default:
		return nil, fmt.Errorf("cassandradriver: resolve does not handle operation %q", dsl.Operation)
	}
}

func (r *Resolver) resolveRead(ctx context.Context, node *Node) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("cassandradriver: query has no root node")
	}
	rows, err := r.fetchNode(ctx, node, nil, node.resolvedCursor)
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
		} else {
			data = nil
		}
	} else {
		data = rows
	}
	return json.Marshal(map[string]any{node.FieldName: data})
}

// fetchNode runs one node's SELECT (with optional extra predicates for a child
// IN-fetch), decodes column values, and tags __typename.
func (r *Resolver) fetchNode(ctx context.Context, node *Node, extra []Filter, cursor string) ([]map[string]any, error) {
	fnode := node
	if len(extra) > 0 || (node.Keyspace == "" && r.DefaultKeyspace != "") {
		clone := *node
		if len(extra) > 0 {
			clone.Filters = append(append([]Filter{}, node.Filters...), extra...)
		}
		if clone.Keyspace == "" {
			clone.Keyspace = r.DefaultKeyspace
		}
		fnode = &clone
	}

	plan, err := PlanRead(fnode)
	if err != nil {
		return nil, err
	}
	cql, args, err := BuildSelect(plan)
	if err != nil {
		return nil, err
	}

	pageState, err := DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}

	rs, err := r.Exec.Query(ctx, Statement{
		CQL:        cql,
		Args:       args,
		PageSize:   node.PageSize,
		PageState:  pageState,
		Idempotent: true,
	})
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, len(rs.Rows))
	for i, raw := range rs.Rows {
		out[i] = decodeRow(node, raw)
	}
	return out, nil
}

func decodeRow(node *Node, raw map[string]any) map[string]any {
	row := make(map[string]any, len(raw)+1)
	for col, val := range raw {
		t := ""
		if node.ColumnTypes != nil {
			t = node.ColumnTypes[col]
		}
		if dv, err := DecodeValue(t, val); err == nil {
			row[col] = dv
		} else {
			row[col] = val
		}
	}
	if node.Typename != "" {
		row["__typename"] = node.Typename
	}
	return row
}

// attachChildren fetches and attaches every child relationship onto rows.
func (r *Resolver) attachChildren(ctx context.Context, node *Node, rows []map[string]any, depth int) error {
	if len(rows) == 0 || len(node.Children) == 0 {
		return nil
	}
	if depth > r.maxDepth() {
		return fmt.Errorf("cassandradriver: nested query exceeds max depth %d", r.maxDepth())
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
		return fmt.Errorf("cassandradriver: child %q is missing relationship metadata", child.FieldName)
	}

	kind := child.Rel.Kind
	if kind == "" {
		k, err := deriveCardinality(child.Rel.ChildCol, child.PartitionKeys, child.ClusteringKeys)
		if err != nil {
			return err
		}
		kind = k
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
		attachEmpty(child, parentRows, kind)
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

func attachEmpty(child *Node, rows []map[string]any, kind string) {
	for _, pr := range rows {
		pr[child.FieldName] = emptyValue(kind)
	}
}

func emptyValue(kind string) any {
	if kind == RelOneToOne {
		return nil
	}
	return []map[string]any{}
}

// fetchChildChunks fetches child rows for all parent join values via chunked IN
// queries on the child's partition key, capped by a worker pool. Results are
// returned in chunk order so assembly is deterministic.
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
			rows, err := r.fetchNode(ctx, child, []Filter{{Col: child.Rel.ChildCol, Op: OpIn, Value: ch}}, "")
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

func (r *Resolver) resolveWrite(ctx context.Context, m *Mutation) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("cassandradriver: mutation has no body")
	}
	if m.Keyspace == "" {
		m.Keyspace = r.DefaultKeyspace
	}
	if err := m.applyRawDoc(); err != nil {
		return nil, err
	}
	if err := PlanMutation(m); err != nil {
		return nil, err
	}

	// DELETE returns the pre-image, so read before the write.
	if m.Type == OpDelete {
		var data []byte
		var err error
		if m.Returning != nil {
			data, err = r.resolveRead(ctx, m.Returning)
			if err != nil {
				return nil, err
			}
		}
		cql, args, err := BuildDelete(m)
		if err != nil {
			return nil, err
		}
		if _, err := r.Exec.Exec(ctx, Statement{CQL: cql, Args: args}); err != nil {
			return nil, err
		}
		if data == nil {
			return []byte("null"), nil
		}
		return data, nil
	}

	var (
		cql  string
		args []any
		err  error
	)
	switch m.Type {
	case OpInsert, OpUpsert:
		cql, args, err = BuildInsert(m)
	case OpUpdate:
		cql, args, err = BuildUpdate(m)
	default:
		return nil, fmt.Errorf("cassandradriver: unsupported mutation type %q", m.Type)
	}
	if err != nil {
		return nil, err
	}

	// LWT and counter writes are non-idempotent.
	idempotent := !m.IfExists && !m.IfNotExists && len(m.CounterCols) == 0
	if _, err := r.Exec.Exec(ctx, Statement{CQL: cql, Args: args, Idempotent: idempotent}); err != nil {
		return nil, err
	}

	if m.Returning == nil {
		return []byte("null"), nil
	}
	return r.resolveRead(ctx, m.Returning)
}
