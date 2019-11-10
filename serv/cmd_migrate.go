package serv

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/super-graph/migrate"
	"github.com/spf13/cobra"
)

var sampleMigration = `-- This is a sample migration.

create table users(
  id serial primary key,
  fullname varchar not null,
  email varchar not null
);

---- create above / drop below ----

drop table users;
`

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

func cmdDBSetup(cmd *cobra.Command, args []string) {
	cmdDBCreate(cmd, []string{})
	cmdDBMigrate(cmd, []string{"up"})

	sfile := path.Join(confPath, conf.SeedFile)
	_, err := os.Stat(sfile)

	if err == nil {
		cmdDBSeed(cmd, []string{})
		return
	}

	if os.IsNotExist(err) == false {
		logger.Fatal().Err(err).Msgf("unable to check if '%s' exists", sfile)
	}

	logger.Warn().Msgf("failed to read seed file '%s'", sfile)
}

func cmdDBCreate(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	ctx := context.Background()

	conn, err := initDB(conf, false)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer conn.Close(ctx)

	sql := fmt.Sprintf("CREATE DATABASE %s", conf.DB.DBName)

	_, err = conn.Exec(ctx, sql)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create database")
	}

	logger.Info().Msgf("created database '%s'", conf.DB.DBName)
}

func cmdDBDrop(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	ctx := context.Background()

	conn, err := initDB(conf, false)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer conn.Close(ctx)

	sql := fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, conf.DB.DBName)

	_, err = conn.Exec(ctx, sql)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create database")
	}

	logger.Info().Msgf("dropped database '%s'", conf.DB.DBName)
}

func cmdDBNew(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help()
		os.Exit(1)
	}

	var err error

	if conf, err = initConf(); err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	name := args[0]

	m, err := migrate.FindMigrations(conf.MigrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	mname := fmt.Sprintf("%03d_%s.sql", (len(m) + 1), name)

	// Write new migration
	mpath := filepath.Join(conf.MigrationsPath, mname)
	mfile, err := os.OpenFile(mpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer mfile.Close()

	_, err = mfile.WriteString(newMigrationText)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger.Info().Msgf("created migration '%s'", mpath)
}

func cmdDBMigrate(cmd *cobra.Command, args []string) {
	var err error

	if len(args) == 0 {
		cmd.Help()
		os.Exit(1)
	}

	dest := args[0]

	if conf, err = initConf(); err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	conn, err := initDB(conf, true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer conn.Close(context.Background())

	m, err := migrate.NewMigrator(conn, "schema_version")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initializing migrator")
	}

	m.Data = getMigrationVars()

	err = m.LoadMigrations(conf.MigrationsPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load migrations")
	}

	if len(m.Migrations) == 0 {
		logger.Fatal().Msg("No migrations found")
	}

	m.OnStart = func(sequence int32, name, direction, sql string) {
		logger.Info().Msgf("%s executing %s %s\n%s\n\n",
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
			logger.Fatal().Err(err).Msg("invalid destination")
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
		cmd.Help()
		os.Exit(1)
	}

	if err != nil {
		logger.Info().Err(err).Send()

		// logger.Info().Err(err).Send()

		// if err, ok := err.(m.MigrationPgError); ok {
		// 	if err.Detail != "" {
		// 		logger.Info().Err(err).Msg(err.Detail)
		// 	}

		// 	if err.Position != 0 {
		// 		ele, err := ExtractErrorLine(err.Sql, int(err.Position))
		// 		if err != nil {
		// 			logger.Fatal().Err(err).Send()
		// 		}

		// 		prefix := fmt.Sprintf()
		// 		logger.Info().Msgf("line %d, %s%s", ele.LineNum, prefix, ele.Text)
		// 	}
		// }
		// os.Exit(1)
	}

	logger.Info().Msg("migration done")

}

func cmdDBStatus(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	conn, err := initDB(conf, true)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer conn.Close(context.Background())

	m, err := migrate.NewMigrator(conn, "schema_version")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize migrator")
	}

	m.Data = getMigrationVars()

	err = m.LoadMigrations(conf.MigrationsPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load migrations")
	}

	if len(m.Migrations) == 0 {
		logger.Fatal().Msg("no migrations found")
	}

	mver, err := m.GetCurrentVersion()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to retrieve migration")
	}

	var status string
	behindCount := len(m.Migrations) - int(mver)
	if behindCount == 0 {
		status = "up to date"
	} else {
		status = "migration(s) pending"
	}

	fmt.Println("status:  ", status)
	fmt.Printf("version:  %d of %d\n", mver, len(m.Migrations))
	fmt.Println("host:    ", conf.DB.Host)
	fmt.Println("database:", conf.DB.DBName)
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
