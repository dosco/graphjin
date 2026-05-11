//go:build cgo

package codesql

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func commonLanguages() []languageSpec {
	return []languageSpec{
		newLanguageSpec("go", "tree-sitter-go", []string{".go"}, ts.NewLanguage(tree_sitter_go.Language()), goQueries()),
		newLanguageSpec("python", "tree-sitter-python", []string{".py", ".pyw"}, ts.NewLanguage(tree_sitter_python.Language()), pythonQueries()),
		newLanguageSpec("javascript", "tree-sitter-javascript", []string{".js", ".jsx", ".mjs", ".cjs"}, ts.NewLanguage(tree_sitter_javascript.Language()), javascriptQueries()),
		newLanguageSpec("typescript", "tree-sitter-typescript", []string{".ts", ".mts", ".cts"}, ts.NewLanguage(tree_sitter_typescript.LanguageTypescript()), typescriptQueries()),
		newLanguageSpec("tsx", "tree-sitter-tsx", []string{".tsx"}, ts.NewLanguage(tree_sitter_typescript.LanguageTSX()), typescriptQueries()),
		newLanguageSpec("rust", "tree-sitter-rust", []string{".rs"}, ts.NewLanguage(tree_sitter_rust.Language()), rustQueries()),
		newLanguageSpec("java", "tree-sitter-java", []string{".java"}, ts.NewLanguage(tree_sitter_java.Language()), javaQueries()),
	}
}
