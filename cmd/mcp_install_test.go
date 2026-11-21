package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		{in: "all", want: "all"},
		{in: "both", want: "all"},
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
	if opts.Server != "http://localhost:8080/api/v1/mcp" {
		t.Fatalf("server = %q, want normalized local MCP endpoint", opts.Server)
	}
	if opts.BaseServer != defaultMCPServerURL {
		t.Fatalf("base server = %q, want %q", opts.BaseServer, defaultMCPServerURL)
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
	if opts.Client != "all" || opts.Scope != "global" {
		t.Fatalf("unexpected resolved values: %+v", opts)
	}
	if opts.Server != "http://localhost:9090/api/v1/mcp" {
		t.Fatalf("server = %q, want explicit URL", opts.Server)
	}
}

func TestResolveInstallOptions_InteractivePrompts(t *testing.T) {
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Interactive: true,
		PromptFn: func(kind, prompt string, options []string, defaultValue string) (string, error) {
			switch kind {
			case "client":
				return "all", nil
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

	if opts.Client != "all" || opts.Scope != "local" {
		t.Fatalf("unexpected resolved values: %+v", opts)
	}
	if opts.Server != "http://localhost:8080/api/v1/mcp" {
		t.Fatalf("server = %q, want normalized local MCP endpoint", opts.Server)
	}
}

func TestResolveInstallOptions_InvalidServer(t *testing.T) {
	_, err := resolveInstallOptions(mcpInstallResolveInput{
		Server:    "ftp://example.com",
		ServerSet: true,
	})
	if err == nil {
		t.Fatal("expected invalid --server error")
	}
	if !strings.Contains(err.Error(), "invalid server URL") {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestNormalizeMCPAddURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "localhost:8080", want: "http://localhost:8080/api/v1/mcp"},
		{in: "http://localhost:8080", want: "http://localhost:8080/api/v1/mcp"},
		{in: "http://localhost:8080/api/v1", want: "http://localhost:8080/api/v1/mcp"},
		{in: "http://localhost:8080/api/v1/mcp/message", want: "http://localhost:8080/api/v1/mcp"},
	}
	for _, tt := range tests {
		got, err := normalizeMCPAddURL(tt.in)
		if err != nil {
			t.Fatalf("normalizeMCPAddURL(%q): %s", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeMCPAddURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProbeMCPServerNoAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer ts.Close()

	origClient := mcpProbeHTTPClient
	mcpProbeHTTPClient = ts.Client()
	defer func() { mcpProbeHTTPClient = origClient }()

	probe, err := probeMCPServer(context.Background(), ts.URL, ts.URL)
	if err != nil {
		t.Fatalf("probeMCPServer: %s", err)
	}
	if probe.Kind != mcpProbeNoAuth {
		t.Fatalf("probe kind = %s, want %s", probe.Kind, mcpProbeNoAuth)
	}
}

func TestProbeMCPServerOAuthChallenge(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource/api/v1/mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	origClient := mcpProbeHTTPClient
	mcpProbeHTTPClient = ts.Client()
	defer func() { mcpProbeHTTPClient = origClient }()

	probe, err := probeMCPServer(context.Background(), ts.URL, ts.URL)
	if err != nil {
		t.Fatalf("probeMCPServer: %s", err)
	}
	if probe.Kind != mcpProbeOAuth {
		t.Fatalf("probe kind = %s, want %s", probe.Kind, mcpProbeOAuth)
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
		return "/opt/bin/deeporg", nil
	}
	if got := graphjinCommandForMCP(); got != "/opt/bin/deeporg" {
		t.Fatalf("graphjinCommandForMCP() = %q, want /opt/bin/deeporg", got)
	}

	resolveGraphJinPathForMCP = func() (string, error) {
		return "", errors.New("missing")
	}
	if got := graphjinCommandForMCP(); got != "deeporg" {
		t.Fatalf("graphjinCommandForMCP() fallback = %q, want deeporg", got)
	}
}

// The command written into an MCP client config is the new name when it is
// installed, and the old name only on a machine that has nothing else: an
// install from before the rename must still produce a config that starts.
func TestResolveBinaryPathPrefersDeeporgThenGraphjin(t *testing.T) {
	orig := lookPathFn
	defer func() { lookPathFn = orig }()

	onPath := func(found map[string]string) func(string) (string, error) {
		return func(name string) (string, error) {
			if p, ok := found[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		}
	}

	lookPathFn = onPath(map[string]string{"deeporg": "/opt/bin/deeporg", "graphjin": "/opt/bin/graphjin"})
	if got, err := resolveGraphJinBinaryPath(); err != nil || got != "/opt/bin/deeporg" {
		t.Fatalf("both installed: got %q, %v; want /opt/bin/deeporg", got, err)
	}

	lookPathFn = onPath(map[string]string{"graphjin": "/opt/bin/graphjin"})
	if got, err := resolveGraphJinBinaryPath(); err != nil || got != "/opt/bin/graphjin" {
		t.Fatalf("old install only: got %q, %v; want /opt/bin/graphjin", got, err)
	}

	lookPathFn = onPath(nil)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveGraphJinBinaryPath(); err != nil || got != self {
		t.Fatalf("neither on PATH: got %q, %v; want the running binary %q", got, err, self)
	}
}

// The root command is `deeporg`. `graphjin` survives only as a second name on
// the PATH for the same file, so the name the binary was started under must
// not change what it parses or prints.
func TestRootCommandIsDeeporgUnderEitherName(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	var help [2]string
	for i, argv0 := range []string{"/usr/local/bin/deeporg", "/usr/local/bin/graphjin"} {
		os.Args = []string{argv0, "--help"}
		root := newRootCmd()
		if root.Name() != "deeporg" {
			t.Fatalf("started as %s: root command is %q, want deeporg", argv0, root.Name())
		}
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"--help"})
		if err := root.Execute(); err != nil {
			t.Fatalf("started as %s: %v", argv0, err)
		}
		help[i] = out.String()
	}
	if help[0] != help[1] {
		t.Fatalf("help differs by the name the binary was started under:\n--- deeporg\n%s\n--- graphjin\n%s", help[0], help[1])
	}
	if !strings.Contains(help[0], "deeporg [command]") {
		t.Fatalf("help does not name the deeporg command:\n%s", help[0])
	}
	if strings.Contains(help[0], "graphjin [command]") {
		t.Fatalf("help still names the graphjin command:\n%s", help[0])
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
	// The generated MCP-client config no longer embeds --server: `deeporg
	// mcp` reads the server URL from ~/.config/graphjin/client.json, so the
	// args list is simply ["mcp"]. This keeps the MCP client config
	// credential-free and lets `deeporg mcp setup` rotate tokens without
	// any MCP-client edits.
	got := buildClaudeMCPServerArgs(mcpInstallOptions{Server: "http://localhost:8080/"})
	want := []string{"mcp"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("buildClaudeMCPServerArgs() = %v, want %v", got, want)
	}
}

func TestPrintInstallPreview(t *testing.T) {
	var b bytes.Buffer
	printInstallPreview(&b, mcpInstallOptions{
		Client: "all",
		Scope:  "global",
		Server: "http://localhost:8080/",
	})

	out := b.String()
	if !strings.Contains(out, "Install target: all") {
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
		Client: "all",
		Server: "http://localhost:8080/",
	}, codexInstallPlan{
		UseCLI:     false,
		ConfigPath: "/tmp/.codex/config.toml",
	})

	out := b.String()
	if !strings.Contains(out, "GraphJin MCP connection added.") {
		t.Fatalf("expected completion message, got:\n%s", out)
	}
	if !strings.Contains(out, "Claude Desktop / Claude Code") {
		t.Fatalf("expected claude quick guide, got:\n%s", out)
	}
	if !strings.Contains(out, "OpenAI Codex") {
		t.Fatalf("expected codex quick guide, got:\n%s", out)
	}
	if !strings.Contains(out, "Config written to: /tmp/.codex/config.toml") {
		t.Fatalf("expected codex config path note, got:\n%s", out)
	}
}
