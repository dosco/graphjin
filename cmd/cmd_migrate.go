package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/cmd/v3/internal/migrate"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func migrateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate the database",
		Long: `Migrate the database to destination migration version.

		Destination migration version can be one of the following value types:

		Migrate to the most recent migration.
		e.g. db migrate up

		Rollback the most recent migration.
		e.g. db migrate down

		Migrate to a specific migration.
		e.g. db migrate 42

		Migrate forward N steps.
		e.g. db migrate +3

		Migrate backward N steps.
		e.g. db migrate -2

		Redo previous N steps (migrate backward N steps then forward N steps).
		e.g. db migrate -+1
			`,
		Run: cmdDBMigrate,
	}

	c1 := &cobra.Command{
		Use:   "new NAME",
		Short: "Generate a new migration",
		Long:  "Generate a new migration with the next sequence number and provided name",
		Run:   cmdMigrateNew,
	}
	c.AddCommand(c1)

	c2 := &cobra.Command{
		Use:   "status",
		Short: "Print current migration status",
		Run:   cmdMigrateStatus,
	}
	c.AddCommand(c2)

	return c
}

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

func cmdDBMigrate(cmd *cobra.Command, args []string) {
	doneSomething := false

	if len(args) == 0 {
		cmd.Help() //nolint:errcheck
		os.Exit(1)
	}

	dest := args[0]

	setup(cpath)
	initDB(true)

	if conf.DB.Type == "mysql" {
		log.Fatal("Migrations not support with MySQL")
	}

	m, err := migrate.NewMigrator(db)
	if err != nil {
		log.Fatalf("Error initializing migrations: %s", err)
	}

	m.Data = getMigrationVars(conf)

	err = m.LoadMigrations(conf.RelPath(conf.MigrationsPath))
	if err != nil {
		log.Fatalf("Failed to load migrations: %s", err)
	}

	if len(m.Migrations) == 0 {
		log.Fatalf("No migrations found")
	}

	m.OnStart = func(name, direction, sql string) {
		var action string
		if direction == "up" {
			action = "Migrating: "
		} else if direction == "down" {
			action = "Rolling back: "
		} else {
			log.Fatalf("Migration direction %s not supported", direction)
		}
		log.Infof("%s %s", action, name)

		if conf.Debug {
			log.Infof("SQL:\n%s", sql)
		}

		doneSomething = true
	}

	m.OnFinish = func(name, direction string, durationMs int64) {
		var action string
		if direction == "up" {
			action = "Migrated:  "
		} else if direction == "down" {
			action = "Rolled back:  "
		} else {
			log.Fatalf("Migration direction %s not supported", direction)
		}
		log.Infof("%s %s (%d ms)", action, name, durationMs)
	}

	m.OnError = func(name string, err error, sql string) {
		sql = strings.TrimSpace(sql)
		sql = "> " + strings.ReplaceAll(sql, "\n", "\n> ")
		log.Infof("Error in %s\n%s\n%s", name, sql, err)
	}

	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		log.Fatalf("Failed fetching current migrations version: %s", err)
		log.Fatalf("Unable to get current migration version:\n  %v\n", err)
	}

	mustParseDestination := func(d string) int32 {
		var n int64
		n, err = strconv.ParseInt(d, 10, 32)
		if err != nil {
			log.Fatalf("Invalid migration version: %s", err)
		}
		return int32(n)
	}

	if dest == "up" {
		err = m.Migrate()
	} else if dest == "down" {
		err = m.MigrateTo(currentVersion - 1)
	} else if len(dest) >= 3 && dest[0:2] == "-+" {
		err = m.MigrateTo(currentVersion - mustParseDestination(dest[2:]))
		if err == nil {
			err = m.MigrateTo(currentVersion)
		}

	} else if len(dest) >= 2 && dest[0] == '-' {
		err = m.MigrateTo(currentVersion - mustParseDestination(dest[1:]))
	} else if len(dest) >= 2 && dest[0] == '+' {
		err = m.MigrateTo(currentVersion + mustParseDestination(dest[1:]))
	} else {
		cmd.Help() //nolint:errcheck
		os.Exit(1)
	}

	if err != nil {
		log.Fatalf("Error with migrations: %s", err)

		// if err, ok := err.(m.MigrationPgError); ok {
		// 	if err.Detail != "" {
		// 		log.Fatalf("ERR %s", err.Detail)
		// 	}

		// 	if err.Position != 0 {
		// 		ele, err := ExtractErrorLine(err.Sql, int(err.Position))
		// 		if err != nil {
		// 			log.Fatalf("ERR %s", err)
		// 		}

		// 		log.Fatalf("INF line %d, %s%s", ele.LineNum, ele.Text)
		// 	}

		// }
	}

	if !doneSomething {
		log.Infof("Nothing to do")
	}
}

func cmdMigrateStatus(cmd *cobra.Command, args []string) {
	setup(cpath)
	initDB(true)

	if conf.DB.Type == "mysql" {
		log.Fatal("Migrations not support with MySQL")
	}

	m, err := migrate.NewMigrator(db)
	if err != nil {
		log.Fatalf("Error initializing migrations: %s", err)
	}

	m.Data = getMigrationVars(conf)

	err = m.LoadMigrations(conf.RelPath(conf.MigrationsPath))
	if err != nil {
		log.Fatalf("Failed to load migrations: %s", err)
	}

	if len(m.Migrations) == 0 {
		log.Fatal("No migrations found")
	}

	mver, err := m.GetCurrentVersion()
	if err != nil {
		log.Fatalf("Failed to retrieve current migration version: %s", err)
	}

	var status string
	behindCount := len(m.Migrations) - int(mver)
	if behindCount == 0 {
		status = "up to date"
	} else {
		status = "migration(s) pending"
	}

	log.Infof("Status: %s, version: %d of %d, host: %s, database: %s",
		status, mver, len(m.Migrations), conf.DB.Host, conf.DB.DBName)
}

func cmdMigrateNew(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help() //nolint:errcheck
		os.Exit(1)
	}

	setup(cpath)
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
	mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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

type ErrorLineExtract struct {
	LineNum   int    // Line number starting with 1
	ColumnNum int    // Column number starting with 1
	Text      string // Text of the line without a new line character.
}

// ExtractErrorLine takes source and character position extracts the line
// number, column number, and the line of text.
//
// The first character is position 1.
func ExtractErrorLine(source string, position int) (ErrorLineExtract, error) {
	ele := ErrorLineExtract{LineNum: 1}

	if position > len(source) {
		return ele, fmt.Errorf("position (%d) is greater than source length (%d)", position, len(source))
	}

	lines := strings.SplitAfter(source, "\n")
	for _, ele.Text = range lines {
		if position-len(ele.Text) < 1 {
			ele.ColumnNum = position
			break
		}

		ele.LineNum += 1
		position -= len(ele.Text)
	}

	ele.Text = strings.TrimSuffix(ele.Text, "\n")

	return ele, nil
}

func getMigrationVars(c *serv.Config) map[string]interface{} {
	en := cases.Title(language.English)

	return map[string]interface{}{
		"AppName":     en.String(c.AppName),
		"AppNameSlug": strings.ToLower(strings.ReplaceAll(c.AppName, " ", "_")),
		"Env":         strings.ToLower(os.Getenv("GO_ENV")),
	}
}
