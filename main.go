// Main package for the GraphJin service and command line tooling
/*
GraphJin
For documentation, visit https://graphjin.com

Commit SHA-1          : 75ff551
Commit timestamp      : 2020-04-13 00:43:18 -0400
Branch                : master
Go version            : go1.14

Licensed under the Apache Public License 2.0
Copyright 2020, Vikram Rangnekar

Usage:
  graphjin [command]

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
  serv        Run the graphjin service
  version     GraphJin binary version information

Flags:
  -h, --help          help for graphjin
      --path string   path to config files (default "./config")

Use "graphjin [command] --help" for more information about a command.
*/

package main

import "github.com/dosco/graphjin/internal/serv"

func main() {
	serv.Cmd()
}
