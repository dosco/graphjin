package main

import (
	"fmt"
	"os"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var deployActive bool

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
		Run:     cmdServ,
	}
	c.Flags().BoolVar(&deployActive, "deploy-active", false, "Deploy active config")
	return c
}

// cmdServ is the handler for the serve subcommand
func cmdServ(*cobra.Command, []string) {
	printBanner()
	setup(cpath)

	var opt []serv.Option
	if deployActive {
		opt = append(opt, serv.OptionDeployActive())
	}

	gj, err := serv.NewGraphJinService(conf, opt...)
	if err != nil {
		log.Fatalf("%s", err)
	}

	if err := gj.Start(); err != nil {
		log.Fatalf("%s", err)
	}
}
