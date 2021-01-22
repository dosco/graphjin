package serv

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
			servConf.log.Fatalf("ERR database setup not support with MySQL")
		}

		cmdDBCreate(servConf)(cmd, []string{})
		cmdDBMigrate(servConf)(cmd, []string{"up"})

		sfile := path.Join(servConf.conf.cpath, servConf.conf.SeedFile)
		_, err := os.Stat(sfile)
		if err == nil {
			cmdDBSeed(servConf)(cmd, []string{})
			return
		}

		if !os.IsNotExist(err) {
			servConf.log.Fatalf("ERR unable to check if '%s' exists: %s", sfile, err.Error())
		}

		servConf.log.Printf("WRN failed to read seed file '%s'", sfile)
	}
}

func cmdDBReset(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		if servConf.conf.Production {
			servConf.log.Fatalln("ERR db:reset does not work in production")
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
			servConf.log.Fatalf("ERR database creation not support with MySQL")
		}

		if db, err = initDB(servConf, false, false); err != nil {
			servConf.log.Fatalf("ERR failed to connect to database: %s", err)
		}
		defer db.Close()

		dbName := servConf.conf.DB.DBName
		dbExists := false

		err = db.
			QueryRow(`SELECT true as exists FROM pg_database WHERE datname = $1;`, dbName).
			Scan(&dbExists)

		if err != nil && err != sql.ErrNoRows {
			servConf.log.Fatalf("ERR failed checking if database exists: %s", err)
		}

		if dbExists {
			servConf.log.Printf("INF database exists: %s", dbName)
			return
		}

		if _, err = db.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
			servConf.log.Fatalf("ERR failed to create database: %s", err)
		}
		servConf.log.Printf("INF created database: %s", dbName)
	}
}

func cmdDBDrop(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		db, err := initDB(servConf, false, false)
		if err != nil {
			servConf.log.Fatalf("ERR failed to connect to database: %s", err)
		}
		defer db.Close()

		sql := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, servConf.conf.DB.DBName)

		_, err = db.Exec(sql)
		if err != nil {
			servConf.log.Fatalf("ERR failed to drop database: %s", err)
		}

		servConf.log.Printf("INF database dropped: %s", servConf.conf.DB.DBName)
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
			servConf.log.Fatalf("ERR error loading migrations: %s", err)
		}

		mname := fmt.Sprintf("%d_%s.sql", len(m), name)

		// Write new migration
		mpath := filepath.Join(migrationsPath, mname)
		mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			servConf.log.Fatalf("ERR %s", err)
		}
		defer mfile.Close()

		_, err = mfile.WriteString(newMigrationText)
		if err != nil {
			servConf.log.Fatalf("ERR %s", err)
		}

		servConf.log.Printf("INR created migration '%s'", mpath)
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
			servConf.log.Fatalf("ERR migration not support with MySQL")
		}

		conn, err := initDB(servConf, true, false)
		if err != nil {
			servConf.log.Fatalf("ERR failed to connect to database: %s", err)
		}
		defer conn.Close()

		m, err := migrate.NewMigrator(conn, "schema_version")
		if err != nil {
			servConf.log.Fatalf("ERR failed to initializing migrator: %s", err)
		}

		m.Data = getMigrationVars(servConf)

		err = m.LoadMigrations(servConf.conf.relPath(servConf.conf.MigrationsPath))
		if err != nil {
			servConf.log.Fatalf("ERR failed to load migrations: %s", err)
		}

		if len(m.Migrations) == 0 {
			servConf.log.Fatalf("ERR no migrations found")
		}

		m.OnStart = func(sequence int32, name, direction, sql string) {
			servConf.log.Printf("INF %s executing %s %s\n%s\n\n",
				time.Now().Format("2006-01-02 15:04:05"), name, direction, sql)
		}

		currentVersion, err := m.GetCurrentVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get current version:\n  %v\n", err)
			os.Exit(1)
		}

		mustParseDestination := func(d string) int32 {
			var n int64
			n, err = strconv.ParseInt(d, 10, 32)
			if err != nil {
				servConf.log.Fatalf("ERR invalid destination: %s", err)
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
			servConf.log.Fatalf("ERR %s", err)

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

		servConf.log.Println("INF migration done")
	}
}

func cmdDBStatus(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfOnce(servConf)

		db, err := initDB(servConf, true, false)
		if err != nil {
			servConf.log.Fatalf("ERR failed to connect to database: %s", err)
		}
		defer db.Close()

		m, err := migrate.NewMigrator(db, "schema_version")
		if err != nil {
			servConf.log.Fatalf("ERR failed to initialize migrator: %s", err)
		}

		m.Data = getMigrationVars(servConf)

		err = m.LoadMigrations(servConf.conf.relPath(servConf.conf.MigrationsPath))
		if err != nil {
			servConf.log.Fatalf("ERR failed to load migrations: %s", err)
		}

		if len(m.Migrations) == 0 {
			servConf.log.Fatalf("ERR no migrations found")
		}

		mver, err := m.GetCurrentVersion()
		if err != nil {
			servConf.log.Fatalf("ERR failed to retrieve migration: %s", err)
		}

		var status string
		behindCount := len(m.Migrations) - int(mver)
		if behindCount == 0 {
			status = "up to date"
		} else {
			status = "migration(s) pending"
		}

		servConf.log.Printf("INF status: %s, version: %d of %d, host: %s, database: %s",
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
		servConf.log.Fatalf("ERR failed to read config: %s", err)
	}
}
