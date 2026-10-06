package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inlineWidgetsSpec = `
openapi: 3.0.0
info: { title: Widgets, version: '1.0' }
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: { type: array, items: { type: object } }
`

func TestLoadInlineDocumentWithoutSpecsDir(t *testing.T) {
	res, err := Load(LoaderOptions{SpecsDir: filepath.Join(t.TempDir(), "missing"), RequireSource: true}, map[string]SpecConfig{
		"widgets": {SourceName: "widgets_api", Document: inlineWidgetsSpec},
	}, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(res.Registry.Specs) != 1 {
		t.Fatalf("specs = %d, want 1 (warnings %v)", len(res.Registry.Specs), res.Warnings)
	}
	spec := res.Registry.Specs[0]
	if spec.Key != "widgets" || spec.SourceName != "widgets_api" || spec.SourcePath != "inline:widgets" {
		t.Fatalf("spec = key %q source %q path %q", spec.Key, spec.SourceName, spec.SourcePath)
	}
	if string(spec.SourceDocument) != inlineWidgetsSpec {
		t.Fatal("inline document bytes were not kept on the spec")
	}
	if len(spec.Operations) != 1 || spec.Operations[0].OperationID != "listWidgets" {
		t.Fatalf("operations = %+v", spec.Operations)
	}
}

func TestLoadInlineDocumentReplacesFileWithSameKey(t *testing.T) {
	dir := t.TempDir()
	fileSpec := strings.Replace(inlineWidgetsSpec, "listWidgets", "listFileWidgets", 1)
	if err := os.WriteFile(filepath.Join(dir, "widgets.yaml"), []byte(fileSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Load(LoaderOptions{SpecsDir: dir, RequireSource: true}, map[string]SpecConfig{
		"widgets": {SourceName: "widgets_api", Document: inlineWidgetsSpec},
	}, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(res.Registry.Specs) != 1 || res.Registry.Specs[0].Operations[0].OperationID != "listWidgets" {
		t.Fatalf("inline document should replace the file: %+v", res.Registry.Specs)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "inline document replaces widgets.yaml") {
		t.Fatalf("warnings = %v, want a replacement warning", res.Warnings)
	}
}

func TestLoadKeepsFileSpecsBesideInlineDocuments(t *testing.T) {
	dir := t.TempDir()
	fileSpec := strings.Replace(inlineWidgetsSpec, "listWidgets", "listGadgets", 1)
	if err := os.WriteFile(filepath.Join(dir, "gadgets.yaml"), []byte(fileSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Load(LoaderOptions{SpecsDir: dir, RequireSource: true}, map[string]SpecConfig{
		"gadgets": {SourceName: "gadgets_api"},
		"widgets": {SourceName: "widgets_api", Document: inlineWidgetsSpec},
	}, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(res.Registry.Specs) != 2 || res.Registry.Specs[0].Key != "gadgets" || res.Registry.Specs[1].Key != "widgets" {
		t.Fatalf("specs = %+v, want gadgets from the file and widgets inline", res.Registry.Specs)
	}
}

func TestValidateDocument(t *testing.T) {
	if err := ValidateDocument(inlineWidgetsSpec); err != nil {
		t.Fatalf("valid document: %v", err)
	}
	if err := ValidateDocument("openapi: [unclosed"); err == nil {
		t.Fatal("a document that does not parse should fail")
	}
}
