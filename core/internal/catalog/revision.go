package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const catalogModelRevision = "catalog-v1"

func SourceRevisions(snapshot *MetadataSnapshot, conf any, opts BuildOptions) map[string]string {
	if snapshot == nil {
		snapshot = &MetadataSnapshot{}
	}
	opts = normalizeBuildOptions(opts)
	return map[string]string{
		"catalog":   hashJSON(catalogModelRevision),
		"schema":    hashJSON(snapshot),
		"config":    hashJSON(ConfigFingerprint(conf)),
		"language":  hashJSON(languageFeatures),
		"tools":     hashJSON(map[string]any{"known": opts.EnabledToolsKnown, "enabled": opts.EnabledTools}),
		"workflows": hashJSON(opts.Workflows),
	}
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
