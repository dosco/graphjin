package main

import (
	"github.com/spf13/cobra"
)

// dbCmd creates the db command
func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Create, setup, migrate, seed the database",
	}

	c.AddCommand(migrateCmd())

	c3 := &cobra.Command{
		Use:   "seed",
		Short: "Run the seed script to seed the database",
		Run:   cmdDBSeed,
	}
	c.AddCommand(c3)

	c4 := &cobra.Command{
		Use:   "setup",
		Short: "Setup database",
		Long:  "This command will create, migrate and seed the database",
		Run:   cmdDBSetup,
	}
	c.AddCommand(c4)

	return c
}

// cmdDBSeed seeds the database
func cmdDBSetup(cmd *cobra.Command, args []string) {
	setup(cpath)

	if conf.DB.Type == "mysql" {
		log.Fatal("Database setup not support with MySQL")
	}

	cmdDBMigrate(cmd, []string{"up"})
	cmdDBSeed(cmd, []string{})
}
