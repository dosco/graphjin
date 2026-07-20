// Main package for the GraphJin service and command line tooling
/*
GraphJin
For documentation, visit https://graphjin.com

Commit SHA-1          : 00c26ba
Commit timestamp      : 2021-10-01 20:30:47 -0700
Go version            : go1.16

Licensed under the Apache Public License 2.0
Copyright 2019 Vikram Rangnekar

Usage:
  graphjin [command]

Available Commands:
  completion  generate the autocompletion script for the specified shell
  serve       Run the GraphJin service and manage server-side app lifecycle
    new       Create a new application
    db        Create or manage databases
    test      Validate server configuration and connectivity
  mcp         Manage MCP client integrations
  cli         Call a running GraphJin server
  config      Inspect and edit configuration
  help        Help about any command
  version     Version information

Flags:
  -h, --help          help for graphjin
      --path string   path to config files (default "./config")

Use "graphjin [command] --help" for more information about a command.
*/

package main

func main() {
	Cmd()
}
