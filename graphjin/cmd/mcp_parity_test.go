package main

import (
	"sort"
	"testing"

	"github.com/dosco/graphjin/serv/v3"
)

// TestMCPCLIParity enforces the hard rule: every MCP tool the server may
// expose must have a corresponding CLI subcommand with the exact same
// name. The CLI is the only path users have without an MCP-aware client,
// so drift here silently strands a capability for those users.
//
// If this test fails after adding/removing/renaming an MCP tool, the
// fix is in cmd/mcp_tools.go::mcpParityToolCmds (which builds from
// serv.MCPAllToolNames) — not by editing the test.
func TestMCPCLIParity(t *testing.T) {
	serverTools := append([]string(nil), serv.MCPAllToolNames()...)
	cliTools := append([]string(nil), mcpExactToolNames()...)
	sort.Strings(serverTools)
	sort.Strings(cliTools)

	serverSet := make(map[string]bool, len(serverTools))
	for _, n := range serverTools {
		serverSet[n] = true
	}
	cliSet := make(map[string]bool, len(cliTools))
	for _, n := range cliTools {
		cliSet[n] = true
	}

	var missingFromCLI []string
	for _, n := range serverTools {
		if !cliSet[n] {
			missingFromCLI = append(missingFromCLI, n)
		}
	}
	var extraInCLI []string
	for _, n := range cliTools {
		if !serverSet[n] {
			extraInCLI = append(extraInCLI, n)
		}
	}

	if len(missingFromCLI) > 0 {
		t.Errorf("MCP tools missing CLI parity command: %v", missingFromCLI)
	}
	if len(extraInCLI) > 0 {
		t.Errorf("CLI commands without a matching MCP tool: %v", extraInCLI)
	}

	if t.Failed() {
		t.Logf("server (%d): %v", len(serverTools), serverTools)
		t.Logf("cli    (%d): %v", len(cliTools), cliTools)
	}
}

// TestMCPCLIResourceParity enforces the same rule for static MCP
// resources (query_syntax, mutation_syntax, workflow_guide, etc.).
// Surfaced via mcpParityResourceCmds in cmd_cli.go.
func TestMCPCLIResourceParity(t *testing.T) {
	serverResources := serv.MCPCLIResources()
	cliResourceCmds := mcpParityResourceCmds()

	if len(cliResourceCmds) != len(serverResources) {
		t.Fatalf("resource parity drift: %d server-side, %d cli-side", len(serverResources), len(cliResourceCmds))
	}

	cliNames := make(map[string]bool, len(cliResourceCmds))
	for _, c := range cliResourceCmds {
		cliNames[c.Use] = true
	}
	for _, r := range serverResources {
		if !cliNames[r.Command] {
			t.Errorf("MCP resource %q (%s) has no CLI command", r.Command, r.URI)
		}
	}
}
