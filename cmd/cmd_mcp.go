package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	mcpUserID    string
	mcpUserRole  string
	mcpServerURL string // populated from client.json when proxy mode is auto-detected
	mcpDemoMode  bool
	mcpDBFlags   []string
)

func mcpCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP server in stdio mode (for Claude Desktop / Codex)",
		Long: `Run the GraphJin MCP server using stdio transport.

Designed for AI assistant integration (Claude Desktop, Codex, etc.) which
spawn this binary and talk to it over stdin/stdout using the MCP protocol.

Two modes, auto-selected:

  Proxy mode  — when ~/.config/graphjin/client.json exists, this command
                forwards stdio MCP traffic to the remote GraphJin server
                saved there. Sign in with: graphjin mcp setup <server-url>

  Local mode  — when no client.json is present, runs an embedded MCP server
                using --path config + the local database.

Demo mode (--demo, local mode only):
  graphjin mcp --demo                            # built-in demo (SQLite, no containers)
  graphjin mcp --demo --path examples/webshop
  graphjin mcp --demo --path examples/coffee-roastery

Without --path the built-in clinic-scheduler demo is extracted to
./graphjin-demo and served from there. Demo state is stored under
<path>/demo. Delete that folder to reset.

Authentication for local mode:
  --user-id, --user-role flags (highest priority)
  GRAPHJIN_USER_ID, GRAPHJIN_USER_ROLE env vars
  mcp.stdio_user_id, mcp.stdio_user_role config`,
		Run: cmdMCP,
	}

	c.Flags().StringVar(&mcpUserID, "user-id", "", "User ID for MCP session (local mode)")
	c.Flags().StringVar(&mcpUserRole, "user-role", "", "User role for MCP session (local mode)")
	c.Flags().BoolVar(&mcpDemoMode, "demo", false, "Run a curated local demo (built-in example when --path is unset) with state under <path>/demo (local mode)")
	c.Flags().StringArrayVar(&mcpDBFlags, "db", nil, "Database type override(s) (requires --demo)")

	// Subcommands
	c.AddCommand(setupCmd())
	c.AddCommand(mcpInfoCmd())
	c.AddCommand(mcpAddCmd())
	c.AddCommand(mcpInstallCmd())
	c.AddCommand(mcpPluginCmd())

	return c
}

func cmdMCP(cmd *cobra.Command, args []string) {
	// Redirect CLI logger to stderr before setup to avoid corrupting JSON-RPC stream
	log = newLoggerWithOutput(false, os.Stderr).Sugar()

	// Auto-select proxy vs local mode based on whether the user has run
	// `graphjin mcp setup`.
	if cc, _ := LoadClientConfig(); cc != nil && cc.Server != "" {
		mcpServerURL = cc.Server
		runMCPProxy(cmd, args)
		return
	}

	// Local mode from here on.
	if !mcpDemoMode && len(mcpDBFlags) > 0 {
		log.Fatal("--db flags require --demo")
	}

	if mcpDemoMode {
		pathSet := cmd.Flags().Changed("path") || cmd.Flags().Changed("config")
		// Status goes to stderr: stdout carries the JSON-RPC stream.
		dpath, err := resolveDemoPath(pathSet, os.Stderr)
		if err != nil {
			log.Fatalf("Failed to prepare demo: %s", err)
		}
		cpath = dpath
	}

	setup(cpath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var demo *DemoRuntime

	// Start demo containers if --demo is set
	if mcpDemoMode {
		var err error
		demo, err = StartDemo(ctx, mcpDBFlags, os.Stderr)
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
	opts := []serv.Option{serv.OptionSetLogOutput(os.Stderr)}
	if demo != nil && len(demo.Databases) != 0 {
		opts = append(opts,
			serv.OptionSetDatabases(demo.Databases),
			serv.OptionSetRuntimeSchemaDDLDir(demoRuntimeSchemaDDLDir()),
		)
	}
	gj, err := serv.NewGraphJinService(conf, opts...)
	if err != nil {
		log.Fatalf("failed to initialize GraphJin: %s", err)
	}
	if demo != nil {
		demo.Status.Emit("GraphJin", "ready", "service initialized")
	}

	// RunMCPStdio installs its own SIGINT/SIGTERM handling and returns when a
	// signal arrives or stdin is closed by the client. Run it, then always
	// tear down the demo containers on the way out — covering both the signal
	// and the EOF paths so the process never exits leaving containers running.
	runErr := gj.RunMCPStdio(ctx)

	if demo != nil && len(demo.Cleanups) > 0 {
		log.Info("Shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupAll(shutdownCtx, demo.Cleanups)
		shutdownCancel()
		log.Info("Container(s) terminated")
	}

	// A canceled context is the clean signal-triggered shutdown path, not an
	// error worth a non-zero exit.
	if runErr != nil && !errors.Is(runErr, context.Canceled) && ctx.Err() == nil {
		log.Fatalf("MCP server error: %s", runErr)
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

	// --server is inherited from the parent `mcp` command's persistent flag.
	c.Flags().BoolVar(&mcpInfoDemoMode, "demo", false, "Include --demo flag in generated config")

	return c
}

func cmdMCPInfo(cmd *cobra.Command, args []string) {
	// Proxy-mode info: derived from client.json. The generated MCP-client
	// config no longer embeds --server; `graphjin mcp` reads client.json on
	// its own.
	if cc, _ := LoadClientConfig(); cc != nil && cc.Server != "" {
		printMCPProxyConfig(cc.Server)
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
