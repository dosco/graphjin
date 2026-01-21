package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	mcpUserID   string
	mcpUserRole string
)

func mcpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP server in stdio mode (for Claude Desktop)",
		Long: `Run the GraphJin MCP server using stdio transport.

Designed for AI assistant integration (Claude Desktop, etc.).
Communicates via stdin/stdout using the MCP protocol.

Authentication:
  --user-id, --user-role flags (highest priority)
  GRAPHJIN_USER_ID, GRAPHJIN_USER_ROLE env vars
  mcp.stdio_user_id, mcp.stdio_user_role config`,
		Run: cmdMCP,
	}

	c.Flags().StringVar(&mcpUserID, "user-id", "", "User ID for MCP session")
	c.Flags().StringVar(&mcpUserRole, "user-role", "", "User role for MCP session")

	return c
}

func cmdMCP(cmd *cobra.Command, args []string) {
	setup(cpath)

	// Override env vars with flags if provided
	if mcpUserID != "" {
		os.Setenv("GRAPHJIN_USER_ID", mcpUserID)
	}
	if mcpUserRole != "" {
		os.Setenv("GRAPHJIN_USER_ROLE", mcpUserRole)
	}

	gj, err := serv.NewGraphJinService(conf)
	if err != nil {
		log.Fatalf("failed to initialize GraphJin: %s", err)
	}

	// Graceful shutdown setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	if err := gj.RunMCPStdio(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("MCP server error: %s", err)
	}
}
