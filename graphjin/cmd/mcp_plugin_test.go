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
	if opts.Mode != "stdio" {
		t.Fatalf("expected default mode stdio, got %q", opts.Mode)
	}
}
