package serv

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
)

func TestPublicMCPQueryCatalogDoesNotExposeAgentCoverageSearches(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()
	tool, ok := ms.srv.ListTools()["query_catalog"]
	if !ok {
		t.Fatal("public query_catalog tool missing")
	}
	schema, err := json.Marshal(tool.Tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schema), `"searches"`) {
		t.Fatalf("public MCP query_catalog exposed agent-only searches: %s", schema)
	}
}
