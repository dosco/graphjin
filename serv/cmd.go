package serv

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/dosco/super-graph/allow"
	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

//go:generate rice embed-go

const (
	serverName = "Super Graph"
)

var (
	// These variables are set using -ldflags
	version        string
	gitBranch      string
	lastCommitSHA  string
	lastCommitTime string
)

var (
	logger      zerolog.Logger  // logger for everything but errors
	errlog      zerolog.Logger  // logger for errors includes line numbers
	conf        *config         // parsed config
	confPath    string          // path to the config file
	db          *pgxpool.Pool   // database connection pool
	schema      *psql.DBSchema  // database tables, columns and relationships
	allowList   *allow.List     // allow.list is contains queries allowed in production
	qcompile    *qcode.Compiler // qcode compiler
	pcompile    *psql.Compiler  // postgres sql compiler
	secretKey   [32]byte        // encryption key
	internalKey [32]byte        // encryption key used for internal needs
)

func Cmd() {
	initLog()

	rootCmd := &cobra.Command{
		Use:   "super-graph",
		Short: BuildDetails(),
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "serv",
		Short: "Run the super-graph service",
		Run:   cmdServ,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:create",
		Short: "Create database",
		Run:   cmdDBCreate,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:drop",
		Short: "Drop database",
		Run:   cmdDBDrop,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:seed",
		Short: "Run the seed script to seed the database",
		Run:   cmdDBSeed,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:migrate",
		Short: "Migrate the database",
		Long: `Migrate the database to destination migration version.

Destination migration version can be one of the following value types:

Migrate to the most recent migration. 
e.g. db:migrate up

Rollback the most recent migration. 
e.g. db:migrate down

Migrate to a specific migration.
e.g. db:migrate 42

Migrate forward N steps.
e.g. db:migrate +3

Migrate backward N steps.
e.g. db:migrate -2

Redo previous N steps (migrate backward N steps then forward N steps).
e.g. db:migrate -+1
	`,
		Run: cmdDBMigrate,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:status",
		Short: "Print current migration status",
		Run:   cmdDBStatus,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:new NAME",
		Short: "Generate a new migration",
		Long:  "Generate a new migration with the next sequence number and provided name",
		Run:   cmdDBNew,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:setup",
		Short: "Setup database",
		Long:  "This command will create, migrate and seed the database",
		Run:   cmdDBSetup,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:reset",
		Short: "Reset database",
		Long:  "This command will drop, create, migrate and seed the database (won't run in production)",
		Run:   cmdDBReset,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "new APP-NAME",
		Short: "Create a new application",
		Long:  "Generate all the required files to start on a new Super Graph app",
		Run:   cmdNew,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   fmt.Sprintf("conf:dump [%s]", strings.Join(viper.SupportedExts, "|")),
		Short: "Dump config to file",
		Long:  "Dump current config to a file in the selected format",
		Run:   cmdConfDump,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Super Graph binary version information",
		Run:   cmdVersion,
	})

	rootCmd.Flags().StringVar(&confPath,
		"path", "./config", "path to config files")

	if err := rootCmd.Execute(); err != nil {
		errlog.Fatal().Err(err).Send()
	}
}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

func BuildDetails() string {
	return fmt.Sprintf(`
Super Graph %v 
For documentation, visit https://supergraph.dev

Commit SHA-1          : %v
Commit timestamp      : %v
Branch                : %v
Go version            : %v

Licensed under the Apache Public License 2.0
Copyright 2020, Vikram Rangnekar.
`,
		version,
		lastCommitSHA,
		lastCommitTime,
		gitBranch,
		runtime.Version())
}
