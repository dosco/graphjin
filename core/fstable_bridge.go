package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
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
				"fs_name": fc.Name,
			},
		})
	}
	return nil
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
// both LIST (no `key` arg) and SINGLE (`key` arg present) queries —
// the engine treats this as a top-level remote table, so all GraphQL
// field args arrive on Sel.ExtraArgs.
type filesystemBridge struct {
	name    string
	backend fstable.Backend
	conf    FilesystemConfig
}

func (b *filesystemBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
	if req.Sel == nil {
		return nil, errors.New("filesystem: resolve called without a select")
	}
	args := req.Sel.ExtraArgs

	inlineData := args["inline_data"] == "true"

	// Single-key fetch when `key` is supplied.
	if key, ok := args["key"]; ok && key != "" {
		entry, err := b.backend.Stat(ctx, key)
		if err != nil {
			if errors.Is(err, fstable.ErrNotFound) {
				return wrapItems(nil)
			}
			return nil, fmt.Errorf("filesystem(%s): stat: %w", b.name, err)
		}
		row, err := b.entryToRow(ctx, entry, inlineData)
		if err != nil {
			return nil, err
		}
		return wrapItems([]map[string]any{row})
	}

	// List path.
	limit := 0
	if v := args["limit"]; v != "" {
		if n, perr := parsePositiveInt(v); perr == nil {
			limit = n
		}
	}
	if max := b.conf.MaxListPageSize; max > 0 && (limit == 0 || limit > max) {
		limit = max
	}
	opts := fstable.ListOpts{
		Prefix: args["prefix"],
		Limit:  limit,
		After:  args["after"],
	}
	entries, _, err := b.backend.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("filesystem(%s): list: %w", b.name, err)
	}
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row, err := b.entryToRow(ctx, e, inlineData)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return wrapItems(rows)
}

// entryToRow converts a backend Entry to the JSON row shape the engine
// will pluck columns out of. URL is filled by Presign (or the public
// base URL); data is base64-encoded only when inline_data=true.
func (b *filesystemBridge) entryToRow(ctx context.Context, e fstable.Entry, inlineData bool) (map[string]any, error) {
	ttl := b.conf.PresignTTL
	if ttl == 0 {
		ttl = 15 * time.Minute
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
	if rows == nil {
		rows = []map[string]any{}
	}
	return json.Marshal(map[string]any{"items": rows})
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
