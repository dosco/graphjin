package main

import (
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

func TestNormalizeInstallMode(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "stdio", want: "stdio"},
		{in: "http", want: "http"},
		{in: "tcp", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeInstallMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeInstallMode(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeInstallMode(%q): unexpected error: %s", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeInstallMode(%q) = %q, want %q", tt.in, got, tt.want)
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
	if opts.Mode != "stdio" {
		t.Fatalf("mode = %q, want stdio", opts.Mode)
	}
}

func TestResolveInstallOptions_ExplicitFlagsOverridePrompts(t *testing.T) {
	calls := 0
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Client:      "both",
		ClientSet:   true,
		Scope:       "global",
		ScopeSet:    true,
		Mode:        "http",
		ModeSet:     true,
		Server:      "http://localhost:9090/api/v1/mcp",
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
	if opts.Client != "both" || opts.Scope != "global" || opts.Mode != "http" {
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
				return "both", nil
			case "scope":
				return "local", nil
			case "mode":
				return "http", nil
			default:
				return defaultValue, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if opts.Client != "both" || opts.Scope != "local" || opts.Mode != "http" {
		t.Fatalf("unexpected resolved values: %+v", opts)
	}
	if opts.Server != defaultMCPServerURL {
		t.Fatalf("server = %q, want default %q", opts.Server, defaultMCPServerURL)
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
		Scope:      "global",
		Mode:       "stdio",
		ConfigPath: "/tmp/config",
	}

	args := buildCodexAddArgs(opts, true)
	want := []string{"mcp", "add", "graphjin", "--scope", "user", "--", "graphjin", "mcp", "--path", "/tmp/config"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestBuildCodexAddArgsHTTP(t *testing.T) {
	opts := mcpInstallOptions{
		Scope:  "project",
		Mode:   "http",
		Server: "http://localhost:8080/api/v1/mcp",
	}

	args := buildCodexAddArgs(opts, false)
	want := []string{"mcp", "add", "graphjin", "--url", "http://localhost:8080/api/v1/mcp"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestUpsertCodexConfig_PreservesUnrelated(t *testing.T) {
	input := []byte(`model = "o3"

[profiles.default]
approval_policy = "on-request"
`)

	out, err := upsertCodexConfig(input, "graphjin", codexServerConfig{
		Command: "graphjin",
		Args:    []string{"mcp", "--path", "/tmp/config"},
	})
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
	if !strings.Contains(s, "command =") || !strings.Contains(s, "graphjin") {
		t.Fatalf("expected graphjin command:\n%s", s)
	}
}
