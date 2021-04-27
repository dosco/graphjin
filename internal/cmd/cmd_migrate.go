package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/internal/cmd/internal/migrate"
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/cobra"
)

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

func cmdDBMigrate() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help() //nolint: errcheck
			os.Exit(1)
		}

		dest := args[0]

		initCmd(cpath)
		initDB(true)

		if conf.DB.Type == "mysql" {
			log.Fatal("Migrations not support with MySQL")
		}

		m, err := migrate.NewMigrator(db, "schema_version")
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

		if conf.Debug {
			m.OnStart = func(sequence int32, name, direction, sql string) {
				log.Infof("Executing migration: %s, %s\n%s", name, direction, sql)
			}
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
			cmd.Help() //nolint: errcheck
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

		log.Info("Migrations completed")
	}

}

func cmdDBStatus() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)
		initDB(true)

		if conf.DB.Type == "mysql" {
			log.Fatal("Migrations not support with MySQL")
		}

		m, err := migrate.NewMigrator(db, "schema_version")
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
	return map[string]interface{}{
		"AppName":     strings.Title(c.AppName),
		"AppNameSlug": strings.ToLower(strings.ReplaceAll(c.AppName, " ", "_")),
		"Env":         strings.ToLower(os.Getenv("GO_ENV")),
	}
}
