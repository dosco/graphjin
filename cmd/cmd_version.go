package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// This is cobra CLI command for the version subcommand
func versionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "version",
		Short: "Version information",
		Run:   cmdVersion,
	}
	return c
}

// cmdVersion is the handler for the version subcommand
func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

// BuildDetails returns the version information
func BuildDetails() string {
	if version == "" {
		return `
DeepORG (unknown version)
For documentation, visit https://deeporg.io

To build with version information please use the Makefile
> git clone https://github.com/dosco/deeporg
> cd deeporg && make install

Licensed under the Apache Public License 2.0
Copyright 2019-2025 Vikram Rangnekar
`
	}

	return fmt.Sprintf(`
DeepORG %v 
For documentation, visit https://deeporg.io

Commit SHA-1          : %v
Commit timestamp      : %v
Go version            : %v

Licensed under the Apache Public License 2.0
Copyright 2019-2025 Vikram Rangnekar
`,
		version,
		commit,
		date,
		runtime.Version())
}
