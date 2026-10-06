package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/openapi"
)

const inlineConfigSpec = `
openapi: 3.0.0
info: { title: Forum, version: '1.0' }
paths:
  /latest.json:
    get:
      operationId: listLatestTopics
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: { type: object }
`

func TestNormalizeSourcesAcceptsInlineSpecBesideSpecsDir(t *testing.T) {
	conf := &Config{Sources: []SourceConfig{
		{Name: "crm_api", Kind: "api", SpecsDir: "./specs", Specs: map[string]openapi.SpecConfig{"crm": {}}},
		{Name: "forum_api", Kind: "api", Specs: map[string]openapi.SpecConfig{"forum": {Document: inlineConfigSpec}}},
	}}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if conf.OpenAPISpecsDir != "./specs" {
		t.Fatalf("specs dir = %q, want ./specs", conf.OpenAPISpecsDir)
	}
	forum := conf.OpenAPI["forum"]
	if forum.SourceName != "forum_api" || forum.Document != inlineConfigSpec {
		t.Fatalf("forum spec = source %q, document kept %v", forum.SourceName, forum.Document == inlineConfigSpec)
	}
}

func TestNormalizeSourcesRejectsInlineSpecThatDoesNotParse(t *testing.T) {
	conf := &Config{Sources: []SourceConfig{
		{Name: "forum_api", Kind: "api", Specs: map[string]openapi.SpecConfig{"forum": {Document: "openapi: [unclosed"}}},
	}}
	err := conf.NormalizeSources()
	if err == nil || !strings.Contains(err.Error(), `sources["forum_api"].specs["forum"].document`) {
		t.Fatalf("err = %v, want a document error naming the source and spec", err)
	}
}
