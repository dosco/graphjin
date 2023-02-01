package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "version",
		Short: "Version information",
		Run:   cmdVersion,
	}
	return c
}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

func BuildDetails() string {
	if version == "" {
		return `
GraphJin (unknown version)
For documentation, visit https://graphjin.com

To build with version information please use the Makefile
> git clone https://github.com/dosco/graphjin
> cd graphjin && make install

Licensed under the Apache Public License 2.0
Copyright 2019 Vikram Rangnekar
`
	}

	return fmt.Sprintf(`
GraphJin %v 
For documentation, visit https://graphjin.com

Commit SHA-1          : %v
Commit timestamp      : %v
Go version            : %v

Licensed under the Apache Public License 2.0
Copyright 2019 Vikram Rangnekar
`,
		version,
		commit,
		date,
		runtime.Version())
}
