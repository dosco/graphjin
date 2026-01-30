package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	mcpUserID       string
	mcpUserRole     string
	mcpDemoPersist  bool
	mcpDemoDBFlags  []string
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

	// Add subcommands
	c.AddCommand(mcpInfoCmd())
	c.AddCommand(mcpDemoCmd())

	return c
}

func cmdMCP(cmd *cobra.Command, args []string) {
	// Redirect CLI logger to stderr before setup to avoid corrupting JSON-RPC stream
	log = newLoggerWithOutput(false, os.Stderr).Sugar()

	setup(cpath)

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

// mcpInfoCmd creates the "mcp info" subcommand to display Claude Desktop config
func mcpInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show Claude Desktop configuration",
		Long: `Display the Claude Desktop MCP configuration for this GraphJin project.

Outputs JSON configuration that can be added to your Claude Desktop config file.`,
		Run: cmdMCPInfo,
	}
}

func cmdMCPInfo(cmd *cobra.Command, args []string) {
	setup(cpath)
	printMCPConfig(conf, false)
}

// mcpDemoCmd creates the "mcp demo" subcommand to run MCP with containers
func mcpDemoCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "demo",
		Short: "Run MCP with temporary database container(s)",
		Long: `Run the MCP server with temporary database container(s) for testing.

Starts database containers, runs migrations and seed scripts, then
starts the MCP server in stdio mode. Perfect for testing with Claude Desktop.

For single-database configs (using 'database:' key):
  graphjin mcp demo                    # Use type from config, default to postgres
  graphjin mcp demo --db mysql         # Override to mysql

For multi-database configs (using 'databases:' map):
  graphjin mcp demo                    # Start containers for ALL configured databases
  graphjin mcp demo --db postgres      # Set the primary/default database type`,
		Run: cmdMCPDemo,
	}

	c.Flags().BoolVar(&mcpDemoPersist, "persist", false, "Persist data using Docker volumes")
	c.Flags().StringArrayVar(&mcpDemoDBFlags, "db", nil, "Database type override(s)")
	c.Flags().StringVar(&mcpUserID, "user-id", "", "User ID for MCP session")
	c.Flags().StringVar(&mcpUserRole, "user-role", "", "User role for MCP session")

	// Add info subcommand under demo
	c.AddCommand(mcpDemoInfoCmd())

	return c
}

// mcpDemoInfoCmd creates the "mcp demo info" subcommand
func mcpDemoInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show Claude Desktop configuration for demo mode",
		Long: `Display the Claude Desktop MCP configuration for demo mode.

Outputs JSON configuration with the "demo" subcommand included in args.`,
		Run: cmdMCPDemoInfo,
	}
}

func cmdMCPDemoInfo(cmd *cobra.Command, args []string) {
	setup(cpath)
	printMCPConfig(conf, true)
}

// cmdMCPDemo runs MCP server with demo containers
func cmdMCPDemo(cmd *cobra.Command, args []string) {
	// Redirect CLI logger to stderr before setup to avoid corrupting JSON-RPC stream
	log = newLoggerWithOutput(false, os.Stderr).Sugar()

	setup(cpath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primaryType, dbOverrides := parseDBFlags(mcpDemoDBFlags)

	var cleanups []func(context.Context) error

	// Check if multi-database mode (conf.Core.Databases is populated)
	if len(conf.Databases) > 0 {
		// Multi-database mode
		var err error
		cleanups, err = startMultiDBDemo(ctx, primaryType, dbOverrides, mcpDemoPersist)
		if err != nil {
			log.Fatalf("Failed to start containers: %s", err)
		}
	} else {
		// Single-database mode
		dbType := conf.DB.Type
		if primaryType != "" {
			dbType = primaryType
		}
		if dbType == "" {
			dbType = "postgres" // Default
		}

		log.Infof("Starting %s container...", dbType)
		cleanup, connInfo, err := startDemoContainer(ctx, dbType, mcpDemoPersist)
		if err != nil {
			log.Fatalf("Failed to start container: %s", err)
		}

		log.Infof("Container started successfully")
		cleanups = append(cleanups, cleanup)

		// Override config with container connection
		applyContainerConfig(connInfo)
	}

	// Initialize database connection
	initDB(true)

	// Run migrations if available
	runDemoMigrations()

	// Run seed script if available
	runDemoSeed()

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

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Info("Shutting down...")
		cancel()

		// Cleanup all containers
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		cleanupAll(shutdownCtx, cleanups)
		log.Info("Container(s) terminated")
	}()

	if err := gj.RunMCPStdio(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("MCP server error: %s", err)
	}
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
	appName := conf.AppName
	if appName == "" {
		appName = "graphjin"
	}

	// Build args
	var cmdArgs []string
	if demoMode {
		cmdArgs = []string{"mcp", "demo", "--path", absConfigPath}
	} else {
		cmdArgs = []string{"mcp", "--path", absConfigPath}
	}

	// Build the config structure
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			appName: map[string]interface{}{
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
