//go:build cgo

package codesql

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

type queryPack struct {
	Kind   string
	Source string
	Hash   string
}

type languageSpec struct {
	Name       string
	ParserName string
	Extensions []string
	Language   *ts.Language
	QueryPacks []queryPack
}

func newLanguageSpec(name, parser string, exts []string, lang *ts.Language, packs []queryPack) languageSpec {
	for i := range packs {
		sum := sha256.Sum256([]byte(packs[i].Source))
		packs[i].Hash = hex.EncodeToString(sum[:])
	}
	return languageSpec{Name: name, ParserName: parser, Extensions: exts, Language: lang, QueryPacks: packs}
}

func languageByExt(specs []languageSpec, path string) (languageSpec, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, spec := range specs {
		for _, e := range spec.Extensions {
			if ext == e {
				return spec, true
			}
		}
	}
	return languageSpec{}, false
}

func languageByName(specs []languageSpec, name string) (languageSpec, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "golang":
		name = "go"
	case "py":
		name = "python"
	case "js", "jsx", "mjs", "cjs":
		name = "javascript"
	case "ts", "mts", "cts":
		name = "typescript"
	case "rs":
		name = "rust"
	}
	for _, spec := range specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return languageSpec{}, false
}

func combinedQueryHash(spec languageSpec) string {
	h := sha256.New()
	for _, qp := range spec.QueryPacks {
		h.Write([]byte(qp.Kind))
		h.Write([]byte{0})
		h.Write([]byte(qp.Hash))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func grammarHash(spec languageSpec) string {
	h := sha256.Sum256([]byte(spec.ParserName + ":" + spec.Name))
	return hex.EncodeToString(h[:])
}
