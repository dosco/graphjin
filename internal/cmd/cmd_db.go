package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/dosco/graphjin/internal/cmd/internal/migrate"
	"github.com/spf13/cobra"
)

func cmdDBSetup() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)

		if conf.DB.Type == "mysql" {
			log.Fatal("Database setup not support with MySQL")
		}

		cmdDBCreate()(cmd, []string{})
		cmdDBMigrate()(cmd, []string{"up"})

		sfile := path.Join(cpath, conf.SeedFile)
		_, err := os.Stat(sfile)

		if err == nil {
			cmdDBSeed()(cmd, []string{})
		} else {
			log.Warn("Unable to read seed file: %s", sfile)
		}
	}
}

func cmdDBReset() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)

		if conf.Serv.Production {
			log.Fatal("Command db:reset does not work in production")
		}

		cmdDBDrop()(cmd, []string{})
		cmdDBSetup()(cmd, []string{})
	}
}

func cmdDBCreate() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)
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
}

func cmdDBDrop() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)
		initDB(false)

		sql := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, conf.DB.DBName)

		if _, err := db.Exec(sql); err != nil {
			log.Fatalf("Failed to drop database: %s", err)
		}

		log.Infof("Database dropped: %s", conf.DB.DBName)
	}
}

func cmdDBNew() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			cmd.Help() //nolint: errcheck
			os.Exit(1)
		}

		initCmd(cpath)
		initDB(false)

		name := args[0]
		migrationsPath := conf.RelPath(conf.MigrationsPath)

		m, err := migrate.FindMigrations(migrationsPath)
		if err != nil {
			log.Fatalf("Error loading migrations: %s", err)
		}

		mname := fmt.Sprintf("%d_%s.sql", len(m), name)

		// Write new migration
		mpath := filepath.Join(migrationsPath, mname)
		mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("Error creating migration file: %s", err)
		}
		defer mfile.Close()

		_, err = mfile.WriteString(newMigrationText)
		if err != nil {
			log.Fatalf("Error creating migration file: %s", err)
		}

		log.Infof("Migration file created: %s", mpath)
	}
}
