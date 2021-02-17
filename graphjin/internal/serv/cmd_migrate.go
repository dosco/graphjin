package serv

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/internal/serv/internal/migrate"
	"github.com/spf13/cobra"
)

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

func cmdDBSetup(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		if servConf.conf.DB.Type == "mysql" {
			servConf.log.Fatal("Database setup not support with MySQL")
		}

		cmdDBCreate(servConf)(cmd, []string{})
		cmdDBMigrate(servConf)(cmd, []string{"up"})

		sfile := path.Join(servConf.conf.cpath, servConf.conf.SeedFile)
		_, err := os.Stat(sfile)

		if err == nil {
			cmdDBSeed(servConf)(cmd, []string{})
		} else {
			servConf.log.Warn("Unable to read seed file: %s", sfile)
		}
	}
}

func cmdDBReset(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		if servConf.conf.Production {
			servConf.log.Fatal("Command db:reset does not work in production")
		}

		cmdDBDrop(servConf)(cmd, []string{})
		cmdDBSetup(servConf)(cmd, []string{})
	}
}

func cmdDBCreate(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		var db *sql.DB
		var err error

		initConfOnce(servConf)

		if servConf.conf.DB.Type == "mysql" {
			servConf.log.Fatalf("Database creation not support with MySQL")
		}

		if db, err = initDB(servConf, false, false); err != nil {
			servConf.log.Fatalf("Failed to connect to database: %s", err)
		}
		defer db.Close()

		dbName := servConf.conf.DB.DBName
		dbExists := false

		err = db.
			QueryRow(`SELECT true as exists FROM pg_database WHERE datname = $1;`, dbName).
			Scan(&dbExists)

		if err != nil && err != sql.ErrNoRows {
			servConf.log.Fatalf("Error checking if database exists: %s", err)
		}

		if dbExists {
			servConf.log.Infof("Database exists: %s", dbName)
			return
		}

		if _, err = db.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
			servConf.log.Fatalf("Failed to create database: %s", err)
		}

		servConf.log.Infof("Created database: %s", dbName)
	}
}

func cmdDBDrop(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		db, err := initDB(servConf, false, false)
		if err != nil {
			servConf.log.Fatalf("Failed to connect to database: %s", err)
		}
		defer db.Close()

		sql := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, servConf.conf.DB.DBName)

		_, err = db.Exec(sql)
		if err != nil {
			servConf.log.Fatalf("Failed to drop database: %s", err)
		}

		servConf.log.Infof("Database dropped: %s", servConf.conf.DB.DBName)
	}
}

func cmdDBNew(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			cmd.Help() //nolint: errcheck
			os.Exit(1)
		}

		initConfOnce(servConf)
		name := args[0]
		migrationsPath := servConf.conf.relPath(servConf.conf.MigrationsPath)

		m, err := migrate.FindMigrations(migrationsPath)
		if err != nil {
			servConf.log.Fatalf("Error loading migrations: %s", err)
		}

		mname := fmt.Sprintf("%d_%s.sql", len(m), name)

		// Write new migration
		mpath := filepath.Join(migrationsPath, mname)
		mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			servConf.log.Fatalf("Error creating migration file: %s", err)
		}
		defer mfile.Close()

		_, err = mfile.WriteString(newMigrationText)
		if err != nil {
			servConf.log.Fatalf("Error creating migration file: %s", err)
		}

		servConf.log.Infof("Migration file created: %s", mpath)
	}
}

func cmdDBMigrate(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help() //nolint: errcheck
			os.Exit(1)
		}

		initConfOnce(servConf)
		dest := args[0]

		if servConf.conf.DB.Type == "mysql" {
			servConf.log.Fatal("Migrations not support with MySQL")
		}

		conn, err := initDB(servConf, true, false)
		if err != nil {
			servConf.log.Fatalf("Failed to connect to database: %s", err)
		}
		defer conn.Close()

		m, err := migrate.NewMigrator(conn, "schema_version")
		if err != nil {
			servConf.log.Fatalf("Error initializing migrations: %s", err)
		}

		m.Data = getMigrationVars(servConf)

		err = m.LoadMigrations(servConf.conf.relPath(servConf.conf.MigrationsPath))
		if err != nil {
			servConf.log.Fatalf("Failed to load migrations: %s", err)
		}

		if len(m.Migrations) == 0 {
			servConf.log.Fatalf("No migrations found")
		}

		m.OnStart = func(sequence int32, name, direction, sql string) {
			servConf.log.Infof("Executing migration: %s, %s\n%s", name, direction, sql)
		}

		currentVersion, err := m.GetCurrentVersion()
		if err != nil {
			servConf.log.Fatalf("Failed fetching current migrations version: %s", err)
			servConf.log.Fatalf("Unable to get current migration version:\n  %v\n", err)
		}

		mustParseDestination := func(d string) int32 {
			var n int64
			n, err = strconv.ParseInt(d, 10, 32)
			if err != nil {
				servConf.log.Fatalf("Invalid migration version: %s", err)
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
			servConf.log.Fatalf("Error with migrations: %s", err)

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

		servConf.log.Info("Migrations completed")
	}
}

func cmdDBStatus(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		db, err := initDB(servConf, true, false)
		if err != nil {
			servConf.log.Fatalf("Failed to connect to database: %s", err)
		}
		defer db.Close()

		m, err := migrate.NewMigrator(db, "schema_version")
		if err != nil {
			servConf.log.Fatalf("Error initializing migrations: %s", err)
		}

		m.Data = getMigrationVars(servConf)

		err = m.LoadMigrations(servConf.conf.relPath(servConf.conf.MigrationsPath))
		if err != nil {
			servConf.log.Fatalf("Failed to load migrations: %s", err)
		}

		if len(m.Migrations) == 0 {
			servConf.log.Fatal("No migrations found")
		}

		mver, err := m.GetCurrentVersion()
		if err != nil {
			servConf.log.Fatalf("Failed to retrieve current migration version: %s", err)
		}

		var status string
		behindCount := len(m.Migrations) - int(mver)
		if behindCount == 0 {
			status = "up to date"
		} else {
			status = "migration(s) pending"
		}

		servConf.log.Infof("Status: %s, version: %d of %d, host: %s, database: %s",
			status, mver, len(m.Migrations), servConf.conf.DB.Host, servConf.conf.DB.DBName)
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

func getMigrationVars(servConf *ServConfig) map[string]interface{} {
	return map[string]interface{}{
		"AppName":     strings.Title(servConf.conf.AppName),
		"AppNameSlug": strings.ToLower(strings.ReplaceAll(servConf.conf.AppName, " ", "_")),
		"Env":         strings.ToLower(os.Getenv("GO_ENV")),
	}
}

func initConfOnce(servConf *ServConfig) {
	var err error

	if servConf.conf != nil {
		return
	}

	servConf.conf, err = initConf(servConf)
	if err != nil {
		servConf.log.Fatalf("Failed to read config: %s", err)
	}
}
