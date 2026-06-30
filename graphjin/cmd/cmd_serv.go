package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	// deployActive  bool
	servDemoMode bool
	servDBFlags  []string
)

// ANSI color codes
const (
	colorCyan    = "\033[36m"
	colorMagenta = "\033[35m"
	colorYellow  = "\033[33m"
	colorGray    = "\033[90m"
	colorReset   = "\033[0m"
)

// printBanner prints the sci-fi ASCII art banner on startup
func printBanner() {
	// Respect NO_COLOR environment variable for CI environments
	noColor := os.Getenv("NO_COLOR") != ""

	cyan := colorCyan
	magenta := colorMagenta
	reset := colorReset

	if noColor {
		cyan = ""
		magenta = ""
		reset = ""
	}

	// ASCII art with GRAPH in cyan and JIN in magenta
	banner := fmt.Sprintf(`
%s  ██████╗ ██████╗  █████╗ ██████╗ ██╗  ██╗%s     %s██╗██╗███╗   ██╗%s
%s ██╔════╝ ██╔══██╗██╔══██╗██╔══██╗██║  ██║%s     %s██║██║████╗  ██║%s
%s ██║  ███╗██████╔╝███████║██████╔╝███████║%s     %s██║██║██╔██╗ ██║%s
%s ██║   ██║██╔══██╗██╔══██║██╔═══╝ ██╔══██║%s%s██   ██║%s%s██║██║╚██╗██║%s
%s ╚██████╔╝██║  ██║██║  ██║██║     ██║  ██║%s%s╚█████╔╝%s%s██║██║ ╚████║%s
%s  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝%s %s╚════╝ %s%s╚═╝╚═╝  ╚═══╝%s


`,
		cyan, reset, magenta, reset,
		cyan, reset, magenta, reset,
		cyan, reset, magenta, reset,
		cyan, reset, magenta, reset, magenta, reset,
		cyan, reset, magenta, reset, magenta, reset,
		cyan, reset, magenta, reset, magenta, reset,
	)

	fmt.Print(banner)
}

// servCmd is the cobra CLI command for the serve subcommand
func servCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"serv"},
		Short:   "Run the GraphJin service",
		Long: `Run the GraphJin HTTP service.

Demo mode (--demo):
  graphjin serve --demo --path examples/webshop
  graphjin serve --demo --path examples/coffee-roastery

Demo state is stored under <path>/demo. Delete that folder to reset.`,
		Run: cmdServ,
	}
	// c.Flags().BoolVar(&deployActive, "deploy-active", false, "Deploy active config")
	c.Flags().BoolVar(&servDemoMode, "demo", false, "Run a curated local demo with state under <path>/demo")
	c.Flags().StringArrayVar(&servDBFlags, "db", nil, "Database type override(s) (requires --demo)")

	// Server-side lifecycle subcommands. These used to live at the top level
	// (`graphjin db`, `graphjin new`, `graphjin test`) but were moved under
	// `serve` since they all operate on the same server-side config and DB.
	c.AddCommand(newCmd())
	c.AddCommand(dbCmd())
	c.AddCommand(testCmd())
	return c
}

// cmdServ is the handler for the serve subcommand
func cmdServ(cmd *cobra.Command, args []string) {
	printBanner()

	// Check that --db requires --demo
	if !servDemoMode && len(servDBFlags) > 0 {
		log.Fatal("--db flags require --demo")
	}

	if servDemoMode {
		if err := loadDemoEnv(cpath, os.Stdout); err != nil {
			log.Fatalf("Failed to load demo .env: %s", err)
		}
	}

	setup(cpath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var demo *DemoRuntime

	// Start demo containers if --demo is set
	if servDemoMode {
		var err error
		demo, err = StartDemo(ctx, servDBFlags, os.Stdout)
		if err != nil {
			log.Fatalf("Failed to start demo: %s", err)
		}
	}

	var opt []serv.Option
	if demo != nil && len(demo.Databases) != 0 {
		opt = append(opt,
			serv.OptionSetDatabases(demo.Databases),
			serv.OptionSetRuntimeSchemaDDLDir(demoRuntimeSchemaDDLDir()),
		)
	}
	// if deployActive {
	// 	opt = append(opt, serv.OptionDeployActive())
	// }

	gj, err := serv.NewGraphJinService(conf, opt...)
	if err != nil {
		log.Fatalf("%s", err)
	}
	if demo != nil {
		demo.Status.Emit("GraphJin", "ready", "service initialized")
	}

	// Setup graceful shutdown for demo mode
	if demo != nil && len(demo.Cleanups) > 0 {
		done := make(chan struct{})
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			<-sigCh

			log.Info("Shutting down...")
			cancel()

			// Cleanup all containers
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			cleanupAll(shutdownCtx, demo.Cleanups)
			log.Info("Container(s) terminated")

			close(done)
		}()

		if err := gj.Start(); err != nil {
			log.Fatalf("%s", err)
		}

		<-done
	} else {
		if err := gj.Start(); err != nil {
			log.Fatalf("%s", err)
		}
	}
}
