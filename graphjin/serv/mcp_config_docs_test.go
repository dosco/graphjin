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
			if name != "dev" && !strings.Contains(content, "\nsources:\n") {
				t.Fatalf("%s docs template missing sources:\n%s", name, content)
			}
			if strings.Contains(content, "kind: graphjin") || strings.Contains(content, "kind: workflow") {
				t.Fatalf("%s docs template contains a removed internal source kind:\n%s", name, content)
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
	if !strings.Contains(agenticConfigTemplate, "No feature") && !strings.Contains(agenticConfigTemplate, "Add explicit settings only") {
		t.Fatalf("agentic docs template should explain mode-based feature defaults:\n%s", agenticConfigTemplate)
	}
	for _, redundant := range []string{"\nagent:\n", "sampling:", "http_stateful:", "include_tools_with_agent:"} {
		if strings.Contains(agenticConfigTemplate, redundant) {
			t.Fatalf("agentic docs template contains redundant runtime default %q:\n%s", redundant, agenticConfigTemplate)
		}
	}
	if !strings.Contains(agenticConfigTemplate, "agent is enabled automatically") || !strings.Contains(agenticConfigTemplate, "GraphJin-owned model") {
		t.Fatalf("agentic docs template should explain server-owned model credentials:\n%s", agenticConfigTemplate)
	}
}
