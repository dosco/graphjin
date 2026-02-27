package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeInstallClient(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "claude", want: "claude"},
		{in: "codex", want: "codex"},
		{in: "both", want: "both"},
		{in: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeInstallClient(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeInstallClient(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeInstallClient(%q): unexpected error: %s", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeInstallClient(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeInstallScope(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "project", want: "project"},
		{in: "global", want: "global"},
		{in: "local", want: "local"},
		{in: "user", want: "global"},
		{in: "workspace", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeInstallScope(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeInstallScope(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeInstallScope(%q): unexpected error: %s", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeInstallScope(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveInstallOptions_DefaultsNonInteractive(t *testing.T) {
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if opts.Client != "codex" {
		t.Fatalf("client = %q, want codex", opts.Client)
	}
	if opts.Scope != "project" {
		t.Fatalf("scope = %q, want project", opts.Scope)
	}
	if opts.Server != defaultMCPServerURL {
		t.Fatalf("server = %q, want default %q", opts.Server, defaultMCPServerURL)
	}
}

func TestResolveInstallOptions_ExplicitFlagsOverridePrompts(t *testing.T) {
	calls := 0
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Client:      "both",
		ClientSet:   true,
		Scope:       "global",
		ScopeSet:    true,
		Server:      "http://localhost:9090/",
		ServerSet:   true,
		Interactive: true,
		PromptFn: func(kind, prompt string, options []string, defaultValue string) (string, error) {
			calls++
			return defaultValue, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if calls != 0 {
		t.Fatalf("expected no prompt calls when explicit flags are set, got %d", calls)
	}
	if opts.Client != "both" || opts.Scope != "global" {
		t.Fatalf("unexpected resolved values: %+v", opts)
	}
	if opts.Server != "http://localhost:9090/" {
		t.Fatalf("server = %q, want explicit URL", opts.Server)
	}
}

func TestResolveInstallOptions_InteractivePrompts(t *testing.T) {
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Interactive: true,
		PromptFn: func(kind, prompt string, options []string, defaultValue string) (string, error) {
			switch kind {
			case "client":
				return "both", nil
			case "scope":
				return "local", nil
			default:
				return defaultValue, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if opts.Client != "both" || opts.Scope != "local" {
		t.Fatalf("unexpected resolved values: %+v", opts)
	}
	if opts.Server != defaultMCPServerURL {
		t.Fatalf("server = %q, want default %q", opts.Server, defaultMCPServerURL)
	}
}

func TestResolveInstallOptions_InvalidServer(t *testing.T) {
	_, err := resolveInstallOptions(mcpInstallResolveInput{
		Server:    "not-a-url",
		ServerSet: true,
	})
	if err == nil {
		t.Fatal("expected invalid --server error")
	}
	if !strings.Contains(err.Error(), "invalid --server") {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestCodexHelpHasScope(t *testing.T) {
	if !codexHelpHasScope("Usage...\n  --scope <SCOPE>\n") {
		t.Fatalf("expected scope support to be detected")
	}
	if codexHelpHasScope("Usage...\n  --url <URL>\n") {
		t.Fatalf("did not expect scope support")
	}
}

func TestBuildCodexAddArgs(t *testing.T) {
	opts := mcpInstallOptions{
		Scope:  "global",
		Server: "http://localhost:8080/",
	}

	args := buildCodexAddArgs(opts, true)
	want := []string{"mcp", "add", "graphjin", "--scope", "user", "--url", "http://localhost:8080/"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestBuildCodexAddArgs_WithoutScope(t *testing.T) {
	opts := mcpInstallOptions{
		Scope:  "project",
		Server: "http://localhost:9090/",
	}

	args := buildCodexAddArgs(opts, false)
	want := []string{"mcp", "add", "graphjin", "--url", "http://localhost:9090/"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestBuildCodexRemoveArgs(t *testing.T) {
	opts := mcpInstallOptions{
		Scope: "global",
	}

	args := buildCodexRemoveArgs(opts, true)
	want := []string{"mcp", "remove", "graphjin", "--scope", "user"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestBuildCodexRemoveArgs_WithoutScope(t *testing.T) {
	opts := mcpInstallOptions{
		Scope: "project",
	}

	args := buildCodexRemoveArgs(opts, false)
	want := []string{"mcp", "remove", "graphjin"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestGraphjinCommandForMCP(t *testing.T) {
	orig := resolveGraphJinPathForMCP
	defer func() { resolveGraphJinPathForMCP = orig }()

	resolveGraphJinPathForMCP = func() (string, error) {
		return "/opt/bin/graphjin", nil
	}
	if got := graphjinCommandForMCP(); got != "/opt/bin/graphjin" {
		t.Fatalf("graphjinCommandForMCP() = %q, want /opt/bin/graphjin", got)
	}

	resolveGraphJinPathForMCP = func() (string, error) {
		return "", errors.New("missing")
	}
	if got := graphjinCommandForMCP(); got != "graphjin" {
		t.Fatalf("graphjinCommandForMCP() fallback = %q, want graphjin", got)
	}
}

func TestCodexConfigTargetPath(t *testing.T) {
	wd := "/tmp/work"

	got, err := codexConfigTargetPath("project", wd)
	if err != nil {
		t.Fatalf("unexpected error for project scope: %s", err)
	}
	if got != "/tmp/work/.codex/config.toml" {
		t.Fatalf("project target path = %q, want %q", got, "/tmp/work/.codex/config.toml")
	}

	got, err = codexConfigTargetPath("local", wd)
	if err != nil {
		t.Fatalf("unexpected error for local scope: %s", err)
	}
	if got != "/tmp/work/.codex/config.toml" {
		t.Fatalf("local target path = %q, want %q", got, "/tmp/work/.codex/config.toml")
	}
}

func TestUpsertCodexConfig_PreservesUnrelated(t *testing.T) {
	input := []byte(`model = "o3"

[profiles.default]
approval_policy = "on-request"
`)

	out, err := upsertCodexConfig(input, "graphjin", codexServerConfig{URL: "http://localhost:8080/"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	s := string(out)
	if !strings.Contains(s, "model =") || !strings.Contains(s, "o3") {
		t.Fatalf("expected unrelated root key to be preserved:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.graphjin]") {
		t.Fatalf("expected mcp server section:\n%s", s)
	}
	if !strings.Contains(s, "url =") || !strings.Contains(s, "http://localhost:8080/") {
		t.Fatalf("expected graphjin URL:\n%s", s)
	}
}

func TestBuildClaudeMCPServerArgs(t *testing.T) {
	got := buildClaudeMCPServerArgs(mcpInstallOptions{Server: "http://localhost:8080/"})
	want := []string{"mcp", "--server", "http://localhost:8080/"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("buildClaudeMCPServerArgs() = %v, want %v", got, want)
	}
}

func TestPrintInstallPreview(t *testing.T) {
	var b bytes.Buffer
	printInstallPreview(&b, mcpInstallOptions{
		Client: "both",
		Scope:  "global",
		Server: "http://localhost:8080/",
	})

	out := b.String()
	if !strings.Contains(out, "Install target: both") {
		t.Fatalf("expected install target in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Scope: global") {
		t.Fatalf("expected scope in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Server: http://localhost:8080/") {
		t.Fatalf("expected server in output, got:\n%s", out)
	}
}

func TestPrintPostInstallGuide(t *testing.T) {
	var b bytes.Buffer
	printPostInstallGuide(&b, mcpInstallOptions{
		Client: "both",
		Server: "http://localhost:8080/",
	}, codexInstallPlan{
		UseCLI:     false,
		ConfigPath: "/tmp/.codex/config.toml",
	})

	out := b.String()
	if !strings.Contains(out, "GraphJin MCP setup complete.") {
		t.Fatalf("expected completion message, got:\n%s", out)
	}
	if !strings.Contains(out, "Claude Desktop / Claude Code") {
		t.Fatalf("expected claude quick guide, got:\n%s", out)
	}
	if !strings.Contains(out, "Customizer -> Plugins -> search \"GraphJin\" -> Install.") {
		t.Fatalf("expected claude chat note, got:\n%s", out)
	}
	if !strings.Contains(out, "OpenAI Codex") {
		t.Fatalf("expected codex quick guide, got:\n%s", out)
	}
	if !strings.Contains(out, "Config written to: /tmp/.codex/config.toml") {
		t.Fatalf("expected codex config path note, got:\n%s", out)
	}
}
