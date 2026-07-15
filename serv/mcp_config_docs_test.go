package serv

import (
	"strings"
	"testing"
)

func TestConfigDocsTemplatesUseSources(t *testing.T) {
	cases := map[string]string{
		"dev":     devConfigTemplate,
		"prod":    prodConfigTemplate,
		"agentic": agenticConfigTemplate,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(content, "\nsources:\n") {
				t.Fatalf("%s docs template missing sources:\n%s", name, content)
			}
			if strings.Contains(content, "\ndatabases:\n") || strings.Contains(content, "\ndatabase:\n") {
				t.Fatalf("%s docs template contains active legacy database config:\n%s", name, content)
			}
			for _, required := range []string{
				"\ndiscovery_cache:\n",
				"  refresh_interval: 5m",
				"  startup_wait: 2m",
				"  retain_generations: 2",
				"\ncatalog_search:\n",
				"    embedding_model: text-embedding-3-small",
				"    dimensions: tiny",
			} {
				if !strings.Contains(content, required) {
					t.Fatalf("%s docs template missing discovery/semantic setting %q:\n%s", name, required, content)
				}
			}
		})
	}
}

func TestAgenticConfigDocsTemplate(t *testing.T) {
	if !strings.Contains(agenticConfigTemplate, "mode: agentic") {
		t.Fatalf("agentic docs template missing mode: agentic:\n%s", agenticConfigTemplate)
	}
	if !strings.Contains(agenticConfigTemplate, "workflow.execute: true") {
		t.Fatalf("agentic docs template should allow approved workflow execution:\n%s", agenticConfigTemplate)
	}
	if !strings.Contains(agenticConfigTemplate, "runtime.read: true") {
		t.Fatalf("agentic docs template should allow runtime observability:\n%s", agenticConfigTemplate)
	}
	if !strings.Contains(agenticConfigTemplate, "\nagent:\n") || !strings.Contains(agenticConfigTemplate, "ask_graphjin_agent") {
		t.Fatalf("agentic docs template should document the server-side agent:\n%s", agenticConfigTemplate)
	}
}
