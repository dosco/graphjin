package main

import "testing"

func TestPluginAliasForcesClaudeClient(t *testing.T) {
	opts, err := resolveInstallOptions(mcpInstallResolveInput{
		Interactive: false,
		ForceClient: "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if opts.Client != "claude" {
		t.Fatalf("expected forced client to be claude, got %q", opts.Client)
	}
	if opts.Scope != "project" {
		t.Fatalf("expected default scope project, got %q", opts.Scope)
	}
	if opts.Server != "http://localhost:8080/api/v1/mcp" {
		t.Fatalf("expected default MCP endpoint, got %q", opts.Server)
	}
	if opts.BaseServer != defaultMCPServerURL {
		t.Fatalf("expected base server %q, got %q", defaultMCPServerURL, opts.BaseServer)
	}
}
