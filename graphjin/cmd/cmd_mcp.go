package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	mcpUserID    string
	mcpUserRole  string
	mcpServerURL string
	mcpDemoMode  bool
	mcpPersist   bool
	mcpDBFlags   []string
)

func mcpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP server in stdio mode (for Claude Desktop)",
		Long: `Run the GraphJin MCP server using stdio transport.

Designed for AI assistant integration (Claude Desktop, etc.).
Communicates via stdin/stdout using the MCP protocol.

Demo mode (--demo):
  graphjin mcp --demo                    # Use database type from config, default to postgres
  graphjin mcp --demo --db mysql         # Override database type
  graphjin mcp --demo --persist          # Persist data using Docker volumes

Authentication:
  --user-id, --user-role flags (highest priority)
  GRAPHJIN_USER_ID, GRAPHJIN_USER_ROLE env vars
  mcp.stdio_user_id, mcp.stdio_user_role config`,
		Run: cmdMCP,
	}

	c.Flags().StringVar(&mcpUserID, "user-id", "", "User ID for MCP session")
	c.Flags().StringVar(&mcpUserRole, "user-role", "", "User role for MCP session")
	c.Flags().StringVar(&mcpServerURL, "server", "", "Remote MCP server URL to proxy to (mutually exclusive with --path)")
	c.Flags().BoolVar(&mcpDemoMode, "demo", false, "Run with temporary database container(s)")
	c.Flags().BoolVar(&mcpPersist, "persist", false, "Persist data using Docker volumes (requires --demo)")
	c.Flags().StringArrayVar(&mcpDBFlags, "db", nil, "Database type override(s) (requires --demo)")

	// Add subcommands
	c.AddCommand(mcpInfoCmd())
	c.AddCommand(mcpInstallCmd())
	c.AddCommand(mcpPluginCmd())

	return c
}

func cmdMCP(cmd *cobra.Command, args []string) {
	// Redirect CLI logger to stderr before setup to avoid corrupting JSON-RPC stream
	log = newLoggerWithOutput(false, os.Stderr).Sugar()

	// Check mutual exclusivity of --server and --path
	if mcpServerURL != "" && cmd.Flags().Changed("path") {
		log.Fatal("--server and --path are mutually exclusive")
	}

	// Check that --persist and --db require --demo
	if !mcpDemoMode && (mcpPersist || len(mcpDBFlags) > 0) {
		log.Fatal("--persist and --db flags require --demo")
	}

	// If --server is provided, run in proxy mode
	if mcpServerURL != "" {
		runMCPProxy(cmd, args)
		return
	}

	setup(cpath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cleanups []func(context.Context) error

	// Start demo containers if --demo is set
	if mcpDemoMode {
		var err error
		cleanups, err = StartDemo(ctx, mcpPersist, mcpDBFlags)
		if err != nil {
			log.Fatalf("Failed to start demo: %s", err)
		}
	}

	// Override env vars with flags if provided
	if mcpUserID != "" {
		os.Setenv("GRAPHJIN_USER_ID", mcpUserID) //nolint:errcheck
	}
	if mcpUserRole != "" {
		os.Setenv("GRAPHJIN_USER_ROLE", mcpUserRole) //nolint:errcheck
	}

	// Use stderr for logging in MCP stdio mode to keep stdout clean for JSON-RPC
	gj, err := serv.NewGraphJinService(conf, serv.OptionSetLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("failed to initialize GraphJin: %s", err)
	}

	// Graceful shutdown setup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Info("Shutting down...")
		cancel()

		// Cleanup demo containers if any
		if len(cleanups) > 0 {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			cleanupAll(shutdownCtx, cleanups)
			log.Info("Container(s) terminated")
		}
	}()

	if err := gj.RunMCPStdio(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("MCP server error: %s", err)
	}
}

var mcpInfoDemoMode bool

// mcpInfoCmd creates the "mcp info" subcommand to display Claude Desktop config
func mcpInfoCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "info",
		Short: "Show Claude Desktop configuration",
		Long: `Display the Claude Desktop MCP configuration for this GraphJin project.

Outputs JSON configuration that can be added to your Claude Desktop config file.

Use --demo to include the --demo flag in the generated config.`,
		Run: cmdMCPInfo,
	}

	c.Flags().StringVar(&mcpServerURL, "server", "", "Remote MCP server URL for proxy mode config")
	c.Flags().BoolVar(&mcpInfoDemoMode, "demo", false, "Include --demo flag in generated config")

	return c
}

func cmdMCPInfo(cmd *cobra.Command, args []string) {
	if mcpServerURL != "" {
		printMCPProxyConfig(mcpServerURL)
		return
	}
	setup(cpath)
	printMCPConfig(conf, mcpInfoDemoMode)
}

// printMCPConfig outputs the Claude Desktop configuration JSON
func printMCPConfig(conf *serv.Config, demoMode bool) {
	// Get executable path
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %s", err)
	}

	// Get absolute config path
	absConfigPath, err := filepath.Abs(cpath)
	if err != nil {
		log.Fatalf("Failed to get absolute config path: %s", err)
	}

	// Get app name from config, default to "graphjin"
	// Use slugified version for MCP server name (no spaces/special chars)
	appName := conf.AppName
	if appName == "" {
		appName = "graphjin"
	}

	// Build args
	var cmdArgs []string
	if demoMode {
		cmdArgs = []string{"mcp", "--demo", "--path", absConfigPath}
	} else {
		cmdArgs = []string{"mcp", "--path", absConfigPath}
	}

	// Build the config structure
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"GraphJin": map[string]interface{}{
				"command": execPath,
				"args":    cmdArgs,
			},
		},
	}

	// Output as formatted JSON
	output, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal config: %s", err)
	}

	fmt.Println(string(output))
}

// slugify converts a string to a URL-safe slug
// e.g., "Webshop Development" -> "webshop-development"
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove any character that isn't alphanumeric or hyphen
	reg := regexp.MustCompile(`[^a-z0-9-]+`)
	s = reg.ReplaceAllString(s, "")
	// Remove multiple consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	s = reg.ReplaceAllString(s, "-")
	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")
	if s == "" {
		return "graphjin"
	}
	return s
}
