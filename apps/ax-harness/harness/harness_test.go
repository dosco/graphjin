package harness

import (
	"slices"
	"strings"
	"testing"
)

func TestScopeRequiresUnambiguousHostIdentity(t *testing.T) {
	if err := validateScope("org-42", "thread-17"); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][2]string{
		{"", "thread-17"}, {"org-42", ""},
		{" org-42", "thread-17"}, {"org-42", "thread-17 "},
		{"org-42\nthread:other", "thread-17"}, {"org-42", "thread-17\rorg:other"},
		{strings.Repeat("o", 129), "thread-17"},
	} {
		if err := validateScope(ids[0], ids[1]); err == nil {
			t.Fatalf("admitted invalid identity %q", ids)
		}
	}
}

func TestBridgeCommandGetsOnlyBridgeEnvironment(t *testing.T) {
	cmd := bridgeCommand("/app/mcp-bridge.ts", "neko_memory,neko_library", []string{
		"PATH=/usr/bin", "OPENNEKO_MCP_ORG_ID=org", "OPENNEKO_BROKER_URL=http://broker", "OPENNEKO_BROKER_TOKEN=secret",
		"HTTPS_PROXY=http://proxy", "MODEL_API_KEY=placeholder", "HARNESS_MODEL_ROUTES={}", "HOME=/root",
	})
	if strings.Join(cmd.Args, " ") != "node --import tsx /app/mcp-bridge.ts neko_memory,neko_library" || cmd.Dir != "/app" {
		t.Fatalf("args=%v dir=%s", cmd.Args, cmd.Dir)
	}
	want := []string{"PATH=/usr/bin", "OPENNEKO_MCP_ORG_ID=org", "OPENNEKO_BROKER_URL=http://broker", "OPENNEKO_BROKER_TOKEN=secret", "HTTPS_PROXY=http://proxy"}
	if !slices.Equal(cmd.Env, want) {
		t.Fatalf("env=%v", cmd.Env)
	}
	if js := bridgeCommand("/app/mcp-bridge.js", "", nil); strings.Join(js.Args, " ") != "node /app/mcp-bridge.js" {
		t.Fatalf("args=%v", js.Args)
	}
}
