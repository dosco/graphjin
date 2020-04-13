package serv

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/super-graph/cmd/internal/serv/internal/migrate"
	"github.com/spf13/cobra"
)

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

func cmdDBSetup(cmd *cobra.Command, args []string) {
	initConfOnce()
	cmdDBCreate(cmd, []string{})
	cmdDBMigrate(cmd, []string{"up"})

	sfile := path.Join(conf.cpath, conf.SeedFile)
	_, err := os.Stat(sfile)

	if err == nil {
		cmdDBSeed(cmd, []string{})
		return
	}

	if !os.IsNotExist(err) {
		log.Fatalf("ERR unable to check if '%s' exists: %s", sfile, err)
	}

	log.Printf("WRN failed to read seed file '%s'", sfile)
}

func cmdDBReset(cmd *cobra.Command, args []string) {
	initConfOnce()

	if conf.Production {
		log.Fatalln("ERR db:reset does not work in production")
	}

	cmdDBDrop(cmd, []string{})
	cmdDBSetup(cmd, []string{})
}

func cmdDBCreate(cmd *cobra.Command, args []string) {
	initConfOnce()

	db, err := initDB(conf, false)
	if err != nil {
		log.Fatalf("ERR failed to connect to database: %s", err)
	}
	defer db.Close()

	sql := fmt.Sprintf(`CREATE DATABASE "%s"`, conf.DB.DBName)

	_, err = db.Exec(sql)
	if err != nil {
		log.Fatalf("ERR failed to create database: %s", err)
	}

	log.Printf("INF created database '%s'", conf.DB.DBName)
}

func cmdDBDrop(cmd *cobra.Command, args []string) {
	initConfOnce()

	db, err := initDB(conf, false)
	if err != nil {
		log.Fatalf("ERR failed to connect to database: %s", err)
	}
	defer db.Close()

	sql := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, conf.DB.DBName)

	_, err = db.Exec(sql)
	if err != nil {
		log.Fatalf("ERR failed to drop database: %s", err)
	}

	log.Printf("INF dropped database '%s'", conf.DB.DBName)
}

func cmdDBNew(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help() //nolint: errcheck
		os.Exit(1)
	}

	initConfOnce()
	name := args[0]

	m, err := migrate.FindMigrations(conf.MigrationsPath)
	if err != nil {
		log.Fatalf("ERR error loading migrations: %s", err)
	}

	mname := fmt.Sprintf("%d_%s.sql", len(m), name)

	// Write new migration
	mpath := filepath.Join(conf.MigrationsPath, mname)
	mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}
	defer mfile.Close()

	_, err = mfile.WriteString(newMigrationText)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}

	log.Printf("INR created migration '%s'", mpath)
}

func cmdDBMigrate(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		cmd.Help() //nolint: errcheck
		os.Exit(1)
	}

	initConfOnce()
	dest := args[0]

	conn, err := initDB(conf, true)
	if err != nil {
		log.Fatalf("ERR failed to connect to database: %s", err)
	}
	defer conn.Close()

	m, err := migrate.NewMigrator(conn, "schema_version")
	if err != nil {
		log.Fatalf("ERR failed to initializing migrator: %s", err)
	}

	m.Data = getMigrationVars()

	err = m.LoadMigrations(path.Join(conf.cpath, conf.MigrationsPath))
	if err != nil {
		log.Fatalf("ERR failed to load migrations: %s", err)
	}

	if len(m.Migrations) == 0 {
		log.Fatalf("ERR no migrations found")
	}

	m.OnStart = func(sequence int32, name, direction, sql string) {
		log.Printf("INF %s executing %s %s\n%s\n\n",
			time.Now().Format("2006-01-02 15:04:05"), name, direction, sql)
	}

	var currentVersion int32
	currentVersion, err = m.GetCurrentVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to get current version:\n  %v\n", err)
		os.Exit(1)
	}

	mustParseDestination := func(d string) int32 {
		var n int64
		n, err = strconv.ParseInt(d, 10, 32)
		if err != nil {
			log.Fatalf("ERR invalid destination: %s", err)
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
		log.Fatalf("ERR %s", err)

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

	log.Println("INF migration done")
}

func cmdDBStatus(cmd *cobra.Command, args []string) {
	initConfOnce()

	db, err := initDB(conf, true)
	if err != nil {
		log.Fatalf("ERR failed to connect to database: %s", err)
	}
	defer db.Close()

	m, err := migrate.NewMigrator(db, "schema_version")
	if err != nil {
		log.Fatalf("ERR failed to initialize migrator: %s", err)
	}

	m.Data = getMigrationVars()

	err = m.LoadMigrations(conf.MigrationsPath)
	if err != nil {
		log.Fatalf("ERR failed to load migrations: %s", err)
	}

	if len(m.Migrations) == 0 {
		log.Fatalf("ERR no migrations found")
	}

	mver, err := m.GetCurrentVersion()
	if err != nil {
		log.Fatalf("ERR failed to retrieve migration: %s", err)
	}

	var status string
	behindCount := len(m.Migrations) - int(mver)
	if behindCount == 0 {
		status = "up to date"
	} else {
		status = "migration(s) pending"
	}

	log.Printf("INF status: %s, version: %d of %d, host: %s, database: %s",
		status, mver, len(m.Migrations), conf.DB.Host, conf.DB.DBName)
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

func getMigrationVars() map[string]interface{} {
	return map[string]interface{}{
		"app_name":      strings.Title(conf.AppName),
		"app_name_slug": strings.ToLower(strings.Replace(conf.AppName, " ", "_", -1)),
		"env":           strings.ToLower(os.Getenv("GO_ENV")),
	}
}

func initConfOnce() {
	var err error

	if conf != nil {
		return
	}

	conf, err = initConf()
	if err != nil {
		log.Fatalf("ERR failed to read config: %s", err)
	}
}
