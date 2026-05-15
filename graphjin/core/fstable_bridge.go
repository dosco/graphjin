package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// FilesystemBackendFactory builds a filesystem Backend from one of the
// engine's FilesystemConfig entries. Factories are registered by
// backend name (e.g. "local", "s3", "gcs") via OptionSetFilesystemBackend.
type FilesystemBackendFactory func(conf FilesystemConfig) (fstable.Backend, error)

// fixedFilesystemColumns are the columns every filesystem table exposes,
// regardless of backend. The shape is the smallest portable subset
// that lets the engine emit a uniform GraphQL surface; backend-specific
// fields can be added later via an "extras: JSON" escape hatch without
// breaking existing queries.
//
// Order matters: PrimaryCols is set from the first column with
// PrimaryKey=true, and the SQL pretty-printer iterates in declaration
// order.
func fixedFilesystemColumns(schema, table string) []sdata.DBColumn {
	cols := []sdata.DBColumn{
		{Name: "key", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "size", Type: "bigint"},
		{Name: "content_type", Type: "text"},
		{Name: "etag", Type: "text"},
		{Name: "modified_at", Type: "timestamp without time zone"},
		{Name: "url", Type: "text"},
		{Name: "data", Type: "text"},
	}
	for i := range cols {
		cols[i].Schema = schema
		cols[i].Table = table
		cols[i].ID = int32(i)
	}
	return cols
}

// fixedFilesystemArgs are the GraphQL field-level arguments accepted on
// a filesystem table. They mirror the methods on Backend:
//
//	{ avatars(prefix: "users/", limit: 50) { key url } }       — List
//	{ avatars(key: "users/42.png") { key size url data } }     — Stat / Get
//
// `inline_data` switches Get from "presigned URL" mode to "base64 body
// inline in the response" mode. Off by default because the inline path
// is heavyweight and only useful for small files.
func fixedFilesystemArgs(schema, table string) []sdata.DBColumn {
	args := []sdata.DBColumn{
		{Name: "key", Type: "text"},
		{Name: "prefix", Type: "text"},
		{Name: "limit", Type: "bigint"},
		{Name: "after", Type: "text"},
		{Name: "inline_data", Type: "boolean"},
	}
	for i := range args {
		args[i].Schema = schema
		args[i].Table = table
		args[i].ID = int32(i)
	}
	return args
}

func isFilesystemRemoteTable(t sdata.DBTable) bool {
	if t.Type != "remote" {
		return false
	}
	cols := map[string]struct{}{}
	for _, c := range t.Columns {
		cols[c.Name] = struct{}{}
	}
	args := map[string]struct{}{}
	for _, a := range t.Args {
		args[a.Name] = struct{}{}
	}
	_, hasKey := cols["key"]
	_, hasURL := cols["url"]
	_, hasPrefix := args["prefix"]
	_, hasInlineData := args["inline_data"]
	return hasKey && hasURL && hasPrefix && hasInlineData
}

// loadFilesystemIntegration mirrors loadOpenAPIIntegration: it builds
// every configured backend, validates names against existing tables,
// pre-registers the synthetic DBTable entries, and appends matching
// ResolverConfig entries so the existing resolver pipeline picks them up.
func (gj *graphjinEngine) loadFilesystemIntegration() error {
	if len(gj.conf.Filesystems) == 0 {
		return nil
	}

	pdb := gj.primaryDB()
	if pdb == nil || pdb.dbinfo == nil {
		return errors.New("filesystems: primary database not initialised")
	}
	schema := pdb.dbinfo.Schema

	if gj.fsBackends == nil {
		gj.fsBackends = make(map[string]fstable.Backend)
	}

	seen := make(map[string]struct{}, len(gj.conf.Filesystems))
	for i := range gj.conf.Filesystems {
		fc := gj.conf.Filesystems[i]
		if fc.Name == "" {
			return fmt.Errorf("filesystems[%d]: name is required", i)
		}
		if _, dup := seen[fc.Name]; dup {
			return fmt.Errorf("filesystems: duplicate name %q", fc.Name)
		}
		seen[fc.Name] = struct{}{}

		if fc.Backend == "" {
			return fmt.Errorf("filesystems[%q]: backend is required", fc.Name)
		}
		factory, ok := gj.fsFactories[fc.Backend]
		if !ok {
			return fmt.Errorf("filesystems[%q]: unknown backend %q (register one with core.OptionSetFilesystemBackend)", fc.Name, fc.Backend)
		}
		if _, terr := pdb.dbinfo.GetTable(schema, fc.Name); terr == nil {
			return fmt.Errorf("filesystems[%q]: name collides with an existing table in schema %q", fc.Name, schema)
		}

		backend, err := factory(fc)
		if err != nil {
			return fmt.Errorf("filesystems[%q]: backend init: %w", fc.Name, err)
		}
		if fc.ReadOnly {
			backend = readOnlyFilesystemBackend{
				name:    fc.Name,
				backend: backend,
			}
		}
		if gj.responseCache != nil {
			backend = cacheInvalidatingFilesystemBackend{
				name:    fc.Name,
				backend: backend,
				cache:   gj.responseCache,
			}
		}
		gj.fsBackends[fc.Name] = backend

		t := sdata.NewDBTable(schema, fc.Name, "remote", fixedFilesystemColumns(schema, fc.Name))
		t.Args = fixedFilesystemArgs(schema, fc.Name)
		pdb.dbinfo.AddTable(t)

		gj.conf.Resolvers = append(gj.conf.Resolvers, ResolverConfig{
			Name:      fc.Name,
			Type:      "filesystem",
			Schema:    schema,
			StripPath: "items",
			Props: ResolverProps{
				"fs_name":        fc.Name,
				"fs_fingerprint": filesystemConfigFingerprint(fc),
			},
		})
	}
	return nil
}

func filesystemConfigFingerprint(fc FilesystemConfig) string {
	b, err := json.Marshal(fc)
	if err != nil {
		b = []byte(fmt.Sprintf("%s|%s|%s|%s|%s", fc.Name, fc.Backend, fc.Root, fc.Bucket, fc.Prefix))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newFilesystemResolverFn returns the factory registered under
// "filesystem" in newRTMap. It closes over the engine so each invocation
// can look up its backend by name without round-tripping through Props.
func (gj *graphjinEngine) newFilesystemResolverFn() ResolverFn {
	return func(props ResolverProps) (Resolver, error) {
		name, _ := props["fs_name"].(string)
		if name == "" {
			return nil, errors.New("filesystem: 'fs_name' missing from resolver props")
		}
		backend, ok := gj.fsBackends[name]
		if !ok {
			return nil, fmt.Errorf("filesystem: backend for %q not initialised", name)
		}
		var conf FilesystemConfig
		for i := range gj.conf.Filesystems {
			if gj.conf.Filesystems[i].Name == name {
				conf = gj.conf.Filesystems[i]
				break
			}
		}
		return &filesystemBridge{name: name, backend: backend, conf: conf}, nil
	}
}

// filesystemBridge implements core.Resolver. The same instance handles
// both LIST and SINGLE queries. Legacy filesystem-only args live in
// Sel.ExtraArgs, while common GraphJin args like where/order_by/limit
// arrive through the normal compiled selector fields.
type filesystemBridge struct {
	name    string
	backend fstable.Backend
	conf    FilesystemConfig
}

func (b *filesystemBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
	if req.Sel == nil {
		return nil, errors.New("filesystem: resolve called without a select")
	}
	plan, err := b.planQuery(req)
	if err != nil {
		return nil, err
	}

	// Single-key fetch when `key` is supplied.
	if plan.key != "" {
		entry, err := b.backend.Stat(ctx, plan.key)
		if err != nil {
			if errors.Is(err, fstable.ErrNotFound) {
				if req.Sel.Singular {
					return wrapFilesystemItem(nil)
				}
				return wrapItems(nil)
			}
			return nil, fmt.Errorf("filesystem(%s): stat: %w", b.name, err)
		}
		ok, err := filesystemEntryMatches(entry, req.Sel.Where.Exp, req.Vars, plan.cursor)
		if err != nil {
			return nil, err
		}
		if !ok || plan.offset > 0 {
			if req.Sel.Singular {
				return wrapFilesystemItem(nil)
			}
			return wrapItems(nil)
		}
		row, err := b.entryToRow(ctx, entry, plan.inlineData)
		if err != nil {
			return nil, err
		}
		if req.Sel.Singular {
			return wrapFilesystemItem(row)
		}
		return wrapItems([]map[string]any{row})
	}

	// List path.
	entries, err := b.listEntries(ctx, req, plan)
	if err != nil {
		return nil, err
	}
	if len(req.Sel.OrderBy) != 0 {
		if err := sortFilesystemEntries(entries, req.Sel.OrderBy); err != nil {
			return nil, err
		}
	}
	entries = pageFilesystemEntries(entries, plan.offset, plan.limit)
	cursor, err := filesystemCursor(req.Sel, entries)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row, err := b.entryToRow(ctx, e, plan.inlineData)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return wrapFilesystemItems(rows, cursor)
}

type filesystemQueryPlan struct {
	key          string
	prefix       string
	after        string
	limit        int
	offset       int
	inlineData   bool
	globalSort   bool
	resultTarget int
	cursor       filesystemCursorState
}

type filesystemCursorState struct {
	Values map[string]string
}

func (b *filesystemBridge) planQuery(req ResolverReq) (filesystemQueryPlan, error) {
	args := req.Sel.ExtraArgs
	plan := filesystemQueryPlan{
		key:        args["key"],
		prefix:     args["prefix"],
		after:      args["after"],
		inlineData: args["inline_data"] == "true" || filesystemSelectsData(req.Sel),
		globalSort: filesystemNeedsGlobalSort(req.Sel.OrderBy),
	}

	if plan.key == "" {
		key, ok, err := filesystemWhereKeyEquals(req.Sel.Where.Exp, req.Vars)
		if err != nil {
			return plan, err
		}
		if ok {
			plan.key = key
		}
	}
	if plan.prefix == "" {
		prefix, ok, err := filesystemWhereKeyPrefix(req.Sel.Where.Exp, req.Vars)
		if err != nil {
			return plan, err
		}
		if ok {
			plan.prefix = prefix
		}
	}

	var err error
	plan.cursor, err = filesystemCursorFromSelect(req.Sel, req.Vars)
	if err != nil {
		return plan, err
	}
	if plan.after == "" && filesystemCanUseAfterToken(req.Sel.OrderBy) {
		plan.after = plan.cursor.Values["key"]
	}
	plan.limit, err = filesystemLimit(req.Sel, args, req.Vars)
	if err != nil {
		return plan, err
	}
	plan.offset, err = filesystemOffset(req.Sel, req.Vars)
	if err != nil {
		return plan, err
	}
	if max := b.conf.MaxListPageSize; max > 0 && (plan.limit == 0 || plan.limit > max) {
		plan.limit = max
	}
	if plan.limit > 0 {
		plan.resultTarget = plan.offset + plan.limit
	}
	return plan, nil
}

func (b *filesystemBridge) listEntries(ctx context.Context, req ResolverReq, plan filesystemQueryPlan) ([]fstable.Entry, error) {
	pageSize := b.pageSize(plan)
	after := plan.after
	capHint := pageSize
	if plan.resultTarget > capHint {
		capHint = plan.resultTarget
	}
	out := make([]fstable.Entry, 0, capHint)

	for {
		entries, next, err := b.backend.List(ctx, fstable.ListOpts{
			Prefix: plan.prefix,
			Limit:  pageSize,
			After:  after,
		})
		if err != nil {
			return nil, fmt.Errorf("filesystem(%s): list: %w", b.name, err)
		}
		for _, e := range entries {
			ok, err := filesystemEntryMatches(e, req.Sel.Where.Exp, req.Vars, plan.cursor)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, e)
			}
		}
		if next == "" {
			break
		}
		after = next
		if !plan.globalSort && plan.resultTarget > 0 && len(out) >= plan.resultTarget {
			break
		}
	}
	return out, nil
}

func (b *filesystemBridge) pageSize(plan filesystemQueryPlan) int {
	if plan.globalSort || plan.resultTarget == 0 {
		if b.conf.MaxListPageSize > 0 {
			return b.conf.MaxListPageSize
		}
		return 1000
	}
	if b.conf.MaxListPageSize > 0 && plan.resultTarget > b.conf.MaxListPageSize {
		return b.conf.MaxListPageSize
	}
	return plan.resultTarget
}

func filesystemSelectsData(sel *qcode.Select) bool {
	if sel == nil {
		return false
	}
	for _, f := range sel.Fields {
		if f.Col.Name == "data" || f.FieldName == "data" {
			return true
		}
	}
	return false
}

func filesystemCanUseAfterToken(orderBy []qcode.OrderBy) bool {
	return len(orderBy) == 0 ||
		(len(orderBy) == 1 && orderBy[0].Col.Name == "key" && !filesystemOrderDesc(orderBy[0].Order))
}

func filesystemCursorFromSelect(sel *qcode.Select, vars map[string]json.RawMessage) (filesystemCursorState, error) {
	st := filesystemCursorState{}
	if sel == nil || sel.Paging.CursorVar == "" {
		return st, nil
	}
	raw, ok, err := filesystemOptionalStringVar(sel.Paging.CursorVar, vars)
	if err != nil || !ok || raw == "" {
		return st, err
	}

	values := filesystemCursorValues(raw)
	if len(values) == 0 {
		return st, nil
	}
	st.Values = make(map[string]string, len(values))
	for i, ob := range sel.OrderBy {
		if i >= len(values) {
			break
		}
		name := ob.Col.Name
		if name != "" {
			st.Values[name] = values[i]
		}
	}
	if len(st.Values) == 0 && len(values) == 1 {
		st.Values["key"] = values[0]
	}
	return st, nil
}

func filesystemOptionalStringVar(name string, vars map[string]json.RawMessage) (string, bool, error) {
	raw, ok := vars[name]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return "", false, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("filesystem cursor variable %q must be a string", name)
	}
	return v, true, nil
}

func filesystemCursorValues(raw string) []string {
	if raw == "" {
		return nil
	}
	if idx := strings.Index(raw, ":"); idx != -1 && strings.HasPrefix(raw, "gj-") {
		raw = raw[idx+1:]
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 1 {
		if _, err := strconv.ParseInt(parts[0], 10, 32); err == nil {
			parts = parts[1:]
		}
	}
	return parts
}

func filesystemLimit(sel *qcode.Select, args map[string]string, vars map[string]json.RawMessage) (int, error) {
	if sel != nil {
		if sel.Paging.LimitVar != "" {
			return filesystemIntVar("limit", sel.Paging.LimitVar, vars)
		}
		if sel.Paging.Limit > 0 {
			return int(sel.Paging.Limit), nil
		}
		if sel.Paging.NoLimit {
			return 0, nil
		}
	}
	if v := args["limit"]; v != "" {
		return parsePositiveInt(v)
	}
	return 0, nil
}

func filesystemOffset(sel *qcode.Select, vars map[string]json.RawMessage) (int, error) {
	if sel == nil {
		return 0, nil
	}
	if sel.Paging.OffsetVar != "" {
		return filesystemIntVar("offset", sel.Paging.OffsetVar, vars)
	}
	if sel.Paging.Offset > 0 {
		return int(sel.Paging.Offset), nil
	}
	return 0, nil
}

func filesystemIntVar(arg, name string, vars map[string]json.RawMessage) (int, error) {
	v, err := filesystemVar(name, vars)
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case float64:
		if n <= 0 || n != float64(int(n)) {
			return 0, fmt.Errorf("filesystem %s variable %q must be a positive integer", arg, name)
		}
		return int(n), nil
	case int:
		if n <= 0 {
			return 0, fmt.Errorf("filesystem %s variable %q must be positive", arg, name)
		}
		return n, nil
	case string:
		return parsePositiveInt(n)
	default:
		return 0, fmt.Errorf("filesystem %s variable %q must be numeric", arg, name)
	}
}

func filesystemNeedsGlobalSort(orderBy []qcode.OrderBy) bool {
	if len(orderBy) == 0 {
		return false
	}
	return !(len(orderBy) == 1 && orderBy[0].Col.Name == "key" && !filesystemOrderDesc(orderBy[0].Order))
}

func pageFilesystemEntries(entries []fstable.Entry, offset, limit int) []fstable.Entry {
	if offset > 0 {
		if offset >= len(entries) {
			return nil
		}
		entries = entries[offset:]
	}
	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}
	return entries
}

func filesystemCursor(sel *qcode.Select, entries []fstable.Entry) (string, error) {
	if sel == nil || !sel.Paging.Cursor || len(entries) == 0 || len(sel.OrderBy) == 0 {
		return "", nil
	}
	last := entries[len(entries)-1]
	var b strings.Builder
	b.WriteString(strconv.Itoa(int(sel.ID)))
	for _, ob := range sel.OrderBy {
		name := ob.Col.Name
		if name == "" {
			return "", fmt.Errorf("filesystem cursor order column is missing")
		}
		v, null, err := filesystemEntryColumnValue(last, name)
		if err != nil {
			return "", err
		}
		b.WriteByte(',')
		if !null {
			b.WriteString(filesystemCursorString(v))
		}
	}
	return b.String(), nil
}

func filesystemCursorString(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprint(v)
}

func sortFilesystemEntries(entries []fstable.Entry, orderBy []qcode.OrderBy) error {
	if len(orderBy) == 0 || len(entries) < 2 {
		return nil
	}
	for _, ob := range orderBy {
		if _, _, err := filesystemEntryColumnValue(entries[0], ob.Col.Name); err != nil {
			return err
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		for _, ob := range orderBy {
			cmp := compareFilesystemEntryColumn(entries[i], entries[j], ob.Col.Name)
			if cmp == 0 {
				continue
			}
			if filesystemOrderDesc(ob.Order) {
				return cmp > 0
			}
			return cmp < 0
		}
		return entries[i].Key < entries[j].Key
	})
	return nil
}

func compareFilesystemEntryColumn(left, right fstable.Entry, name string) int {
	lv, _, _ := filesystemEntryColumnValue(left, name)
	rv, _, _ := filesystemEntryColumnValue(right, name)
	switch l := lv.(type) {
	case int64:
		r := rv.(int64)
		switch {
		case l < r:
			return -1
		case l > r:
			return 1
		default:
			return 0
		}
	case time.Time:
		r := rv.(time.Time)
		switch {
		case l.Before(r):
			return -1
		case l.After(r):
			return 1
		default:
			return 0
		}
	default:
		return strings.Compare(fmt.Sprint(lv), fmt.Sprint(rv))
	}
}

func filesystemOrderDesc(order qcode.Order) bool {
	return order == qcode.OrderDesc || order == qcode.OrderDescNullsFirst || order == qcode.OrderDescNullsLast
}

func filesystemWhereKeyEquals(ex *qcode.Exp, vars map[string]json.RawMessage) (string, bool, error) {
	if ex == nil {
		return "", false, nil
	}
	switch ex.Op {
	case qcode.OpAnd:
		for _, child := range ex.Children {
			key, ok, err := filesystemWhereKeyEquals(child, vars)
			if err != nil || ok {
				return key, ok, err
			}
		}
	case qcode.OpEquals, qcode.OpNotDistinct:
		if filesystemExpColumnName(ex) != "key" {
			return "", false, nil
		}
		v, err := filesystemExpScalar(ex, vars, filesystemCursorState{})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprint(v), true, nil
	}
	return "", false, nil
}

func filesystemWhereKeyPrefix(ex *qcode.Exp, vars map[string]json.RawMessage) (string, bool, error) {
	if ex == nil {
		return "", false, nil
	}
	switch ex.Op {
	case qcode.OpAnd:
		for _, child := range ex.Children {
			prefix, ok, err := filesystemWhereKeyPrefix(child, vars)
			if err != nil || ok {
				return prefix, ok, err
			}
		}
	case qcode.OpLike:
		if filesystemExpColumnName(ex) != "key" {
			return "", false, nil
		}
		v, err := filesystemExpScalar(ex, vars, filesystemCursorState{})
		if err != nil {
			return "", false, err
		}
		if prefix, ok := filesystemLikePrefix(fmt.Sprint(v)); ok {
			return prefix, true, nil
		}
	}
	return "", false, nil
}

func filesystemLikePrefix(pattern string) (string, bool) {
	if !strings.HasSuffix(pattern, "%") {
		return "", false
	}
	prefix := strings.TrimSuffix(pattern, "%")
	if strings.ContainsAny(prefix, "%_") {
		return "", false
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		return "", false
	}
	return prefix, true
}

func filesystemEntryMatches(e fstable.Entry, ex *qcode.Exp, vars map[string]json.RawMessage, cursor filesystemCursorState) (bool, error) {
	if ex == nil {
		return true, nil
	}
	switch ex.Op {
	case qcode.OpNop:
		return true, nil
	case qcode.OpAnd:
		for _, child := range ex.Children {
			ok, err := filesystemEntryMatches(e, child, vars, cursor)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case qcode.OpOr:
		for _, child := range ex.Children {
			ok, err := filesystemEntryMatches(e, child, vars, cursor)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case qcode.OpNot:
		if len(ex.Children) == 0 {
			return false, nil
		}
		ok, err := filesystemEntryMatches(e, ex.Children[0], vars, cursor)
		return !ok, err
	case qcode.OpFalse:
		return false, nil
	}
	return filesystemCompareEntry(e, ex, vars, cursor)
}

func filesystemCompareEntry(e fstable.Entry, ex *qcode.Exp, vars map[string]json.RawMessage, cursor filesystemCursorState) (bool, error) {
	left, null, err := filesystemLeftValue(e, ex, cursor)
	if err != nil {
		return false, err
	}
	switch ex.Op {
	case qcode.OpIsNull:
		want := true
		if ex.Right.Val != "" {
			var err error
			want, err = filesystemExpBool(ex, vars, cursor)
			if err != nil {
				return false, err
			}
		}
		return null == want, nil
	case qcode.OpIsNotNull:
		return !null, nil
	}
	if null {
		return false, nil
	}

	switch ex.Op {
	case qcode.OpIn, qcode.OpNotIn:
		list, err := filesystemExpList(ex, vars, cursor)
		if err != nil {
			return false, err
		}
		ok := false
		for _, right := range list {
			eq, err := filesystemEqual(left, right)
			if err != nil {
				return false, err
			}
			if eq {
				ok = true
				break
			}
		}
		if ex.Op == qcode.OpNotIn {
			ok = !ok
		}
		return ok, nil
	}

	right, err := filesystemExpScalar(ex, vars, cursor)
	if err != nil {
		return false, err
	}
	switch ex.Op {
	case qcode.OpEquals, qcode.OpNotDistinct:
		return filesystemEqual(left, right)
	case qcode.OpNotEquals, qcode.OpDistinct:
		ok, err := filesystemEqual(left, right)
		return !ok, err
	case qcode.OpGreaterThan, qcode.OpGreaterOrEquals, qcode.OpLesserThan, qcode.OpLesserOrEquals:
		cmp, err := filesystemCompare(left, right)
		if err != nil {
			return false, err
		}
		switch ex.Op {
		case qcode.OpGreaterThan:
			return cmp > 0, nil
		case qcode.OpGreaterOrEquals:
			return cmp >= 0, nil
		case qcode.OpLesserThan:
			return cmp < 0, nil
		default:
			return cmp <= 0, nil
		}
	case qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike:
		ok, err := filesystemLike(fmt.Sprint(left), fmt.Sprint(right), ex.Op == qcode.OpILike || ex.Op == qcode.OpNotILike)
		if ex.Op == qcode.OpNotLike || ex.Op == qcode.OpNotILike {
			ok = !ok
		}
		return ok, err
	case qcode.OpRegex, qcode.OpNotRegex, qcode.OpIRegex, qcode.OpNotIRegex:
		pattern := fmt.Sprint(right)
		if ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotIRegex {
			pattern = "(?i)" + pattern
		}
		ok, err := regexp.MatchString(pattern, fmt.Sprint(left))
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ok = !ok
		}
		return ok, err
	case qcode.OpContains:
		return strings.Contains(fmt.Sprint(left), fmt.Sprint(right)), nil
	default:
		return false, fmt.Errorf("filesystem where does not support operator %s", ex.Op)
	}
}

func filesystemLeftValue(e fstable.Entry, ex *qcode.Exp, cursor filesystemCursorState) (any, bool, error) {
	if ex != nil && ex.Left.Table == "__cur" {
		name := ex.Left.Col.Name
		if ex.Left.ColName != "" {
			name = ex.Left.ColName
		}
		v, ok := cursor.Values[name]
		return v, !ok || v == "", nil
	}
	return filesystemEntryColumnValue(e, filesystemExpColumnName(ex))
}

func filesystemExpColumnName(ex *qcode.Exp) string {
	if ex == nil {
		return ""
	}
	if ex.Left.Col.Name != "" {
		return ex.Left.Col.Name
	}
	return ex.Left.ColName
}

func filesystemEntryColumnValue(e fstable.Entry, name string) (any, bool, error) {
	switch name {
	case "key":
		return e.Key, false, nil
	case "size":
		return e.Size, false, nil
	case "content_type":
		return e.ContentType, e.ContentType == "", nil
	case "etag":
		return e.ETag, e.ETag == "", nil
	case "modified_at":
		return e.ModifiedAt, e.ModifiedAt.IsZero(), nil
	case "url", "data":
		return nil, true, fmt.Errorf("filesystem where/order_by on %q is not supported", name)
	default:
		return nil, true, fmt.Errorf("filesystem where/order_by column %q is not supported", name)
	}
}

func filesystemExpScalar(ex *qcode.Exp, vars map[string]json.RawMessage, cursor filesystemCursorState) (any, error) {
	if ex.Right.Table == "__cur" {
		name := ex.Right.Col.Name
		if ex.Right.ColName != "" {
			name = ex.Right.ColName
		}
		v, ok := cursor.Values[name]
		if !ok {
			return nil, fmt.Errorf("filesystem cursor is missing value for %q", name)
		}
		return v, nil
	}
	switch ex.Right.ValType {
	case qcode.ValVar:
		return filesystemVar(ex.Right.Val, vars)
	case qcode.ValNum:
		if strings.ContainsAny(ex.Right.Val, ".eE") {
			return strconv.ParseFloat(ex.Right.Val, 64)
		}
		return strconv.ParseInt(ex.Right.Val, 10, 64)
	case qcode.ValBool:
		return strconv.ParseBool(ex.Right.Val)
	default:
		return ex.Right.Val, nil
	}
}

func filesystemExpBool(ex *qcode.Exp, vars map[string]json.RawMessage, cursor filesystemCursorState) (bool, error) {
	v, err := filesystemExpScalar(ex, vars, cursor)
	if err != nil {
		return false, err
	}
	switch b := v.(type) {
	case bool:
		return b, nil
	case string:
		return strconv.ParseBool(b)
	default:
		return false, fmt.Errorf("filesystem boolean expression expects a boolean, got %T", v)
	}
}

func filesystemExpList(ex *qcode.Exp, vars map[string]json.RawMessage, cursor filesystemCursorState) ([]any, error) {
	if ex.Right.ValType == qcode.ValVar {
		v, err := filesystemVar(ex.Right.Val, vars)
		if err != nil {
			return nil, err
		}
		switch list := v.(type) {
		case []any:
			return list, nil
		case []string:
			out := make([]any, len(list))
			for i, v := range list {
				out[i] = v
			}
			return out, nil
		default:
			return nil, fmt.Errorf("filesystem variable %q must be a list", ex.Right.Val)
		}
	}
	if ex.Right.Table == "__cur" {
		v, err := filesystemExpScalar(ex, vars, cursor)
		if err != nil {
			return nil, err
		}
		return []any{v}, nil
	}
	out := make([]any, 0, len(ex.Right.ListVal))
	for _, v := range ex.Right.ListVal {
		switch ex.Right.ListType {
		case qcode.ValNum:
			if strings.ContainsAny(v, ".eE") {
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return nil, err
				}
				out = append(out, n)
			} else {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, err
				}
				out = append(out, n)
			}
		case qcode.ValBool:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		default:
			out = append(out, v)
		}
	}
	return out, nil
}

func filesystemVar(name string, vars map[string]json.RawMessage) (any, error) {
	raw, ok := vars[name]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("filesystem variable %q not found", name)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("filesystem variable %q: %w", name, err)
	}
	return v, nil
}

func filesystemEqual(left, right any) (bool, error) {
	cmp, err := filesystemCompare(left, right)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

func filesystemCompare(left, right any) (int, error) {
	switch l := left.(type) {
	case int64:
		r, err := filesystemFloat(right)
		if err != nil {
			return 0, err
		}
		lf := float64(l)
		switch {
		case lf < r:
			return -1, nil
		case lf > r:
			return 1, nil
		default:
			return 0, nil
		}
	case time.Time:
		r, err := filesystemTime(right)
		if err != nil {
			return 0, err
		}
		switch {
		case l.Before(r):
			return -1, nil
		case l.After(r):
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return strings.Compare(fmt.Sprint(left), fmt.Sprint(right)), nil
	}
}

func filesystemFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case int:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("filesystem numeric comparison got %T", v)
	}
}

func filesystemTime(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	s := fmt.Sprint(v)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func filesystemLike(value, pattern string, insensitive bool) (bool, error) {
	var b strings.Builder
	b.WriteByte('^')
	for _, r := range pattern {
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteByte('$')
	re := b.String()
	if insensitive {
		re = "(?i)" + re
	}
	return regexp.MatchString(re, value)
}

// entryToRow converts a backend Entry to the JSON row shape the engine
// will pluck columns out of. URL is filled by Presign (or the public
// base URL); data is base64-encoded only when inline_data=true.
func (b *filesystemBridge) entryToRow(ctx context.Context, e fstable.Entry, inlineData bool) (map[string]any, error) {
	ttl := b.conf.PresignTTL
	if ttl == 0 {
		ttl = defaultFilesystemPresignTTL
	}

	url, err := b.backend.Presign(ctx, e.Key, fstable.PresignGet, ttl)
	if err != nil && !errors.Is(err, fstable.ErrUnsupported) {
		return nil, fmt.Errorf("filesystem(%s): presign: %w", b.name, err)
	}
	if b.conf.PublicBaseURL != "" {
		// Replace the presigned URL with the configured CDN base. The
		// trailing-slash handling is deliberate: callers are expected
		// to set PublicBaseURL with or without trailing "/" and the
		// key is appended verbatim.
		url = trimTrailingSlash(b.conf.PublicBaseURL) + "/" + e.Key
	}

	row := map[string]any{
		"key":          e.Key,
		"size":         e.Size,
		"content_type": e.ContentType,
		"etag":         e.ETag,
		"modified_at":  e.ModifiedAt.UTC().Format(time.RFC3339Nano),
		"url":          url,
		"data":         nil,
	}

	if inlineData {
		body, _, gerr := b.backend.Get(ctx, e.Key)
		if gerr != nil {
			return nil, fmt.Errorf("filesystem(%s): get: %w", b.name, gerr)
		}
		buf, rerr := io.ReadAll(body)
		_ = body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("filesystem(%s): read: %w", b.name, rerr)
		}
		row["data"] = base64.StdEncoding.EncodeToString(buf)
	}

	return row, nil
}

// wrapItems serialises a list of rows under {"items": [...]}; the
// resolver registration sets StripPath="items" so the engine sees the
// inner array.
func wrapItems(rows []map[string]any) ([]byte, error) {
	return wrapFilesystemItems(rows, "")
}

func wrapFilesystemItem(row map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{"items": row})
}

func wrapFilesystemItems(rows []map[string]any, cursor string) ([]byte, error) {
	if rows == nil {
		rows = []map[string]any{}
	}
	out := map[string]any{"items": rows}
	if cursor != "" {
		out["cursor"] = cursor
	}
	return json.Marshal(out)
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not an int")
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return n, nil
}

// registerBuiltinFilesystemFactories installs the built-in "local"
// factory if no caller already registered one via
// OptionSetFilesystemBackend. Called during engine setup, which is
// single-threaded per engine — no locking needed.
func (gj *graphjinEngine) registerBuiltinFilesystemFactories() {
	if gj.fsFactories == nil {
		gj.fsFactories = make(map[string]FilesystemBackendFactory)
	}
	if _, ok := gj.fsFactories["local"]; ok {
		return
	}
	gj.fsFactories["local"] = func(conf FilesystemConfig) (fstable.Backend, error) {
		return fstable.NewLocal(fstable.LocalConfig{Root: conf.Root})
	}
}
