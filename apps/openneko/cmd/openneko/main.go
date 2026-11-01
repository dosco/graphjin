package main

import (
	"fmt"
	"os"

	"github.com/open-neko/neko/apps/openneko/internal/cli"
	"github.com/open-neko/neko/apps/openneko/internal/roles"
)

func main() {
	if roles.Run(os.Args) {
		return
	}
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "error:", msg)
		}
		code := cli.ExitCodeFor(err)
		if code == 0 {
			code = 1
		}
		os.Exit(code)
	}
}
