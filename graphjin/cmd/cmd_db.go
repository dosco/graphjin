package main

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
)

func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Create, setup, migrate, seed the database",
	}

	c1 := &cobra.Command{
		Use:   "create",
		Short: "Create database",
		Run:   cmdDBCreate,
	}
	c.AddCommand(c1)

	c2 := &cobra.Command{
		Use:   "drop",
		Short: "Drop database",
		Run:   cmdDBDrop,
	}
	c.AddCommand(c2)

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

	c5 := &cobra.Command{
		Use:   "reset",
		Short: "Reset database",
		Long:  "This command will drop, create, migrate and seed the database (won't run in production)",
		Run:   cmdDBReset,
	}
	c.AddCommand(c5)

	return c
}

func cmdDBSetup(cmd *cobra.Command, args []string) {
	setup(cpath)

	if conf.DB.Type == "mysql" {
		log.Fatal("Database setup not support with MySQL")
	}

	cmdDBCreate(cmd, []string{})
	cmdDBMigrate(cmd, []string{"up"})
	cmdDBSeed(cmd, []string{})
}

func cmdDBReset(cmd *cobra.Command, args []string) {
	setup(cpath)

	if conf.Serv.Production {
		log.Fatal("Command db:reset does not work in production")
	}

	cmdDBDrop(cmd, []string{})
	cmdDBSetup(cmd, []string{})
}

func cmdDBCreate(cmd *cobra.Command, args []string) {
	setup(cpath)
	initDB(false)

	if conf.DB.Type == "mysql" {
		log.Fatalf("Database creation not support with MySQL")
	}

	dbName := conf.DB.DBName
	dbExists := false

	err := db.
		QueryRow(`SELECT true as exists FROM pg_database WHERE datname = $1;`, dbName).
		Scan(&dbExists)

	if err != nil && err != sql.ErrNoRows {
		log.Fatalf("Error checking if database exists: %s", err)
	}

	if dbExists {
		log.Infof("Database exists: %s", dbName)
		return
	}

	if _, err = db.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
		log.Fatalf("Failed to create database: %s", err)
	}

	log.Infof("Created database: %s", dbName)
}

func cmdDBDrop(cmd *cobra.Command, args []string) {
	setup(cpath)
	initDB(false)

	sql := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, conf.DB.DBName)

	if _, err := db.Exec(sql); err != nil {
		log.Fatalf("Failed to drop database: %s", err)
	}

	log.Infof("Database dropped: %s", conf.DB.DBName)
}
