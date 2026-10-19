package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

const (
	fragmentKindDBRoot = "db-root"
	fragmentKindDBJoin = "db-join"
	fragmentKindRemote = "remote"

	defaultFilesystemPresignTTL  = 15 * time.Minute
	filesystemPresignSafetySlack = 30 * time.Second
)

func (s *gstate) fragmentCacheEnabled(qc *qcode.QCode) bool {
	if s.gj == nil || s.gj.responseCache == nil || s.gj.cacheKeyBuilder == nil {
		return false
	}
	if s.r.operation != qcode.QTQuery || s.tx() != nil || s.skipCache {
		return false
	}
	if !s.gj.cacheKeyBuilder.ShouldCache(s.r.name, s.getAPQKey()) {
		return false
	}
	return qc == nil || !s.hasOffsetPagination(qc)
}

func (s *gstate) buildFragmentCacheKey(ctx context.Context, kind string, parts map[string]interface{}) string {
	if !s.fragmentCacheEnabled(nil) {
		return ""
	}
	if parts == nil {
		parts = make(map[string]interface{})
	}
	parts["namespace"] = s.r.namespace
	parts["query_name"] = s.r.name
	parts["apq"] = s.getAPQKey()
	return s.gj.cacheKeyBuilder.BuildFragment(ctx, kind, s.role, parts)
}

func (s *gstate) fragmentCacheGet(
	ctx context.Context,
	key string,
	refresh RefreshFnWithOptions,
) ([]byte, bool) {
	if key == "" || s.gj == nil || s.gj.responseCache == nil {
		return nil, false
	}
	data, isStale, found := s.gj.responseCache.Get(ctx, key)
	if !found {
		s.fragmentMisses.Add(1)
		return nil, false
	}
	s.fragmentHits.Add(1)
	if isStale && refresh != nil {
		if refresher, ok := s.gj.responseCache.(SWRRefresherWithOptions); ok {
			refresher.SubmitRefreshWithOptions(key, refresh)
		} else if refresher, ok := s.gj.responseCache.(SWRRefresher); ok {
			refresher.SubmitRefresh(key, func() ([]byte, []RowRef, error) {
				data, refs, opts, err := refresh()
				if opts.NoStore || opts.HardTTL > 0 || opts.FreshTTL > 0 {
					return nil, nil, err
				}
				return data, refs, err
			})
		}
	}
	return data, true
}

func (s *gstate) fragmentCacheSet(
	ctx context.Context,
	key string,
	data []byte,
	refs []RowRef,
	start time.Time,
	opts CacheEntryOptions,
) {
	if key == "" || len(data) == 0 || s.gj == nil || s.gj.responseCache == nil {
		return
	}
	if len(data) > maxResponseSize {
		return
	}
	if opts.NoStore {
		return
	}
	if setter, ok := s.gj.responseCache.(ResponseCacheProviderWithOptions); ok {
		_ = setter.SetWithOptions(ctx, key, data, refs, start, opts)
		return
	}
	if opts.HardTTL > 0 || opts.FreshTTL > 0 {
		return
	}
	_ = s.gj.responseCache.Set(ctx, key, data, refs, start)
}

func (s *gstate) processDBFragmentForCache(dbName string, qc *qcode.QCode, data []byte) ([]byte, []RowRef, error) {
	if len(data) == 0 || qc == nil {
		return data, nil, nil
	}
	cleaned, refs, err := NewResponseProcessor(qc).ProcessForCache(data)
	if err != nil {
		return data, nil, err
	}
	scoped := s.scopeDBRefs(dbName, refs)
	if s.isCodeSQLDatabase(dbName) {
		scoped = appendUniqueCacheRefs(scoped, codeSQLSelectedTableRefs(dbName, qc)...)
	}
	return cleaned, scoped, nil
}

func (s *gstate) scopeDBRefs(dbName string, refs []RowRef) []RowRef {
	if len(refs) == 0 {
		return refs
	}
	source := CacheSourceDB
	if s.isCodeSQLDatabase(dbName) {
		source = CacheSourceCodeSQL
	}
	out := make([]RowRef, 0, len(refs))
	for _, ref := range refs {
		ref = ref.Normalize()
		ref.Source = source
		ref.Scope = dbName
		out = append(out, ref)
	}
	return out
}

func (s *gstate) isCodeSQLDatabase(dbName string) bool {
	if s.gj == nil || s.gj.conf == nil {
		return false
	}
	dbConf, ok := s.gj.conf.Databases[dbName]
	return ok && dbConf.ManagedType == "codesql"
}

func codeSQLSelectedTableRefs(dbName string, qc *qcode.QCode) []RowRef {
	if qc == nil {
		return nil
	}
	refs := make([]RowRef, 0, len(qc.Selects))
	for i := range qc.Selects {
		sel := &qc.Selects[i]
		if sel.Table == "" || sel.SkipRender == qcode.SkipTypeRemote {
			continue
		}
		refs = append(refs, RowRef{
			Source: CacheSourceCodeSQL,
			Scope:  dbName,
			Kind:   CacheKindTable,
			Table:  sel.Table,
		})
	}
	return refs
}

func appendUniqueCacheRefs(refs []RowRef, more ...RowRef) []RowRef {
	if len(more) == 0 {
		return refs
	}
	seen := make(map[string]struct{}, len(refs)+len(more))
	out := make([]RowRef, 0, len(refs)+len(more))
	for _, ref := range refs {
		ref = ref.Normalize()
		key := ref.DependencyKey()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range more {
		ref = ref.Normalize()
		key := ref.DependencyKey()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (s *gstate) dbFragmentKey(
	ctx context.Context,
	kind string,
	dbName string,
	querySQL string,
	args []interface{},
	qc *qcode.QCode,
) string {
	if !s.fragmentCacheEnabled(qc) {
		return ""
	}
	var schemaHash string
	if s.gj != nil {
		if dbCtx, ok := s.gj.GetDatabase(dbName); ok && dbCtx.dbinfo != nil {
			schemaHash = fmt.Sprintf("%x", dbCtx.dbinfo.Hash())
		}
	}
	return s.buildFragmentCacheKey(ctx, kind, map[string]interface{}{
		"database":    dbName,
		"schema_hash": schemaHash,
		"sql":         querySQL,
		"args":        cacheableArgs(args),
	})
}

func cacheableArgs(args []interface{}) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	for i, arg := range args {
		b, err := json.Marshal(arg)
		if err != nil {
			out[i] = fmt.Sprintf("%v", arg)
			continue
		}
		out[i] = string(b)
	}
	return out
}

func (s *gstate) remoteFragmentKey(
	ctx context.Context,
	source string,
	scope string,
	fingerprint string,
	id []byte,
	sel *qcode.Select,
) string {
	return s.buildFragmentCacheKey(ctx, fragmentKindRemote, map[string]interface{}{
		"source":      source,
		"scope":       scope,
		"fingerprint": fingerprint,
		"id":          string(id),
		"select":      selectSignature(sel),
	})
}

func (s *gstate) remoteFragmentCacheOptions(source, scope string) CacheEntryOptions {
	if source != "filesystem" || s.gj == nil || s.gj.conf == nil {
		return CacheEntryOptions{}
	}
	for i := range s.gj.conf.Filesystems {
		fc := s.gj.conf.Filesystems[i]
		if fc.Name != scope {
			continue
		}
		if fc.PublicBaseURL != "" || (fc.Backend != "s3" && fc.Backend != "gcs") {
			return CacheEntryOptions{}
		}
		ttl := fc.PresignTTL
		if ttl == 0 {
			ttl = defaultFilesystemPresignTTL
		}
		hardTTL := ttl - filesystemPresignSafetySlack
		if hardTTL <= 0 {
			return CacheEntryOptions{NoStore: true}
		}
		return CacheEntryOptions{HardTTL: hardTTL}
	}
	return CacheEntryOptions{}
}

func remoteFragmentRefs(source, scope string, id []byte, sel *qcode.Select) []RowRef {
	switch source {
	case "filesystem":
		if sel == nil {
			return nil
		}
		if key := sel.ExtraArgs["key"]; key != "" {
			return []RowRef{filesystemKeyRef(scope, key), filesystemPrefixRef(scope, "")}
		}
		return FilesystemPrefixRefs(scope, sel.ExtraArgs["prefix"])
	case "openapi", "remote_api":
		return []RowRef{RemoteResolverRef(scope, string(id))}
	default:
		return []RowRef{RemoteResolverRef(scope, string(id))}
	}
}

func selectSignature(sel *qcode.Select) string {
	if sel == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(sel.Table)
	b.WriteByte('|')
	b.WriteString(sel.FieldName)
	b.WriteByte('|')
	if len(sel.ExtraArgs) != 0 {
		keys := make([]string, 0, len(sel.ExtraArgs))
		for k := range sel.ExtraArgs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(sel.ExtraArgs[k])
			b.WriteByte(';')
		}
	}
	b.WriteByte('|')
	for _, f := range sel.Fields {
		// Aliased fields ship the source column under the output name, so
		// the signature needs both: `name: key` and `name: url` produce
		// different fragments. Unaliased fields keep the bare-name form so
		// existing cache entries stay addressable.
		if f.Col.Name != "" && f.Col.Name != f.FieldName {
			b.WriteString(f.Col.Name)
			b.WriteByte(':')
		}
		b.WriteString(f.FieldName)
		b.WriteByte(',')
	}
	return b.String()
}

func scanJSONRow(ctx context.Context, dbType string, conn *sql.Conn, tx *sql.Tx, query string, args []interface{}) ([]byte, error) {
	var data []byte
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, query, args...)
		return data, row.Scan(&data)
	}
	err := retryOperationForDB(ctx, dbType, func() error {
		row = conn.QueryRowContext(ctx, query, args...)
		return row.Scan(&data)
	})
	return data, err
}

func encryptResultFragment(data []byte, printFormat []byte, key [32]byte) ([]byte, [sha256.Size]byte, error) {
	dhash := sha256.Sum256(data)
	encrypted, err := encryptValues(data, printFormat, decPrefix, dhash[:], key)
	return encrypted, dhash, err
}
