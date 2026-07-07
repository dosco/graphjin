package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

const catalogModelRevision = "catalog-v3"

func SourceRevisions(snapshot *MetadataSnapshot, conf any, opts BuildOptions) map[string]string {
	if snapshot == nil {
		snapshot = &MetadataSnapshot{}
	}
	opts = normalizeBuildOptions(opts)
	revisions := map[string]string{
		"catalog":       hashJSON(catalogModelRevision),
		"schema":        hashJSON(snapshot),
		"config":        hashJSON(ConfigFingerprint(conf)),
		"sources":       hashJSON(opts.Sources),
		"language":      hashJSON(languageFeatures),
		"tools":         hashJSON(map[string]any{"known": opts.EnabledToolsKnown, "enabled": opts.EnabledTools}),
		"workflows":     hashJSON(opts.Workflows),
		"fragments":     hashJSON(opts.Fragments),
		"saved_queries": hashJSON(opts.SavedQueries),
	}
	for _, source := range opts.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			continue
		}
		revisions["source:"+name] = hashJSON(source)
	}
	for _, name := range metadataSourceNames(snapshot) {
		revisions["schema:"+name] = hashJSON(metadataForSources(snapshot, map[string]struct{}{name: struct{}{}}))
	}
	return revisions
}

func RevisionFromSourceRevisions(source map[string]string) string {
	return hashJSON(source)
}

func hashJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("{}")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func metadataSourceNames(snapshot *MetadataSnapshot) []string {
	if snapshot == nil {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	for _, db := range snapshot.Databases {
		add(db.Name)
	}
	for _, table := range snapshot.Tables {
		add(table.DatabaseName)
	}
	for _, column := range snapshot.Columns {
		add(column.DatabaseName)
	}
	for _, rel := range snapshot.Relationships {
		add(rel.FromDatabaseName)
		add(rel.ToDatabaseName)
	}
	for _, fn := range snapshot.Functions {
		add(fn.DatabaseName)
	}
	for _, idx := range snapshot.Indexes {
		add(idx.DatabaseName)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func metadataForSources(snapshot *MetadataSnapshot, sources map[string]struct{}) *MetadataSnapshot {
	if snapshot == nil {
		return &MetadataSnapshot{}
	}
	owns := func(name string) bool {
		_, ok := sources[strings.TrimSpace(name)]
		return ok
	}
	out := &MetadataSnapshot{}
	for _, db := range snapshot.Databases {
		if owns(db.Name) {
			out.Databases = append(out.Databases, db)
		}
	}
	for _, table := range snapshot.Tables {
		if owns(table.DatabaseName) {
			out.Tables = append(out.Tables, table)
		}
	}
	for _, column := range snapshot.Columns {
		if owns(column.DatabaseName) {
			out.Columns = append(out.Columns, column)
		}
	}
	for _, rel := range snapshot.Relationships {
		if owns(rel.FromDatabaseName) || owns(rel.ToDatabaseName) {
			out.Relationships = append(out.Relationships, rel)
		}
	}
	for _, fn := range snapshot.Functions {
		if owns(fn.DatabaseName) {
			out.Functions = append(out.Functions, fn)
		}
	}
	for _, idx := range snapshot.Indexes {
		if owns(idx.DatabaseName) {
			out.Indexes = append(out.Indexes, idx)
		}
	}
	return out
}
