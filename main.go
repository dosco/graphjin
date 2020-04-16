// Main package for the Super Graph service and command line tooling
/*
Super Graph
For documentation, visit https://supergraph.dev

Commit SHA-1          : 75ff551
Commit timestamp      : 2020-04-13 00:43:18 -0400
Branch                : master
Go version            : go1.14

Licensed under the Apache Public License 2.0
Copyright 2020, Vikram Rangnekar

Usage:
  super-graph [command]

Available Commands:
  conf:dump   Dump config to file
  db:create   Create database
  db:drop     Drop database
  db:migrate  Migrate the database
  db:new      Generate a new migration
  db:reset    Reset database
  db:seed     Run the seed script to seed the database
  db:setup    Setup database
  db:status   Print current migration status
  help        Help about any command
  new         Create a new application
  serv        Run the super-graph service
  version     Super Graph binary version information

Flags:
  -h, --help          help for super-graph
      --path string   path to config files (default "./config")

Use "super-graph [command] --help" for more information about a command.
*/

package main

import "github.com/dosco/super-graph/internal/serv"

func main() {
	serv.Cmd()
}
