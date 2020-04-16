package serv

import (
	"database/sql"
	"fmt"
	_log "log"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
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
	log       *_log.Logger // logger
	zlog      *zap.Logger  // fast logger
	logLevel  int          // log level
	conf      *Config      // parsed config
	confPath  string       // path to the config file
	db        *sql.DB      // database connection pool
	secretKey [32]byte     // encryption key
)

func Cmd() {
	log = _log.New(os.Stdout, "", 0)
	zlog = zap.NewExample()

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

	// rootCmd.AddCommand(&cobra.Command{
	// 	Use:   fmt.Sprintf("conf:dump [%s]", strings.Join(viper.SupportedExts, "|")),
	// 	Short: "Dump config to file",
	// 	Long:  "Dump current config to a file in the selected format",
	// 	Run:   cmdConfDump,
	// })

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Super Graph binary version information",
		Run:   cmdVersion,
	})

	rootCmd.PersistentFlags().StringVar(&confPath,
		"path", "./config", "path to config files")

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("ERR %s", err)
	}
}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

func BuildDetails() string {
	if len(version) == 0 {
		return fmt.Sprintf(`
Super Graph (unknown version)
For documentation, visit https://supergraph.dev

To build with version information please use the Makefile
> git clone https://github.com/dosco/super-graph
> cd super-graph && make install

Licensed under the Apache Public License 2.0
Copyright 2020, Vikram Rangnekar
`)
	}

	return fmt.Sprintf(`
Super Graph %v 
For documentation, visit https://supergraph.dev

Commit SHA-1          : %v
Commit timestamp      : %v
Branch                : %v
Go version            : %v

Licensed under the Apache Public License 2.0
Copyright 2020, Vikram Rangnekar
`,
		version,
		lastCommitSHA,
		lastCommitTime,
		gitBranch,
		runtime.Version())
}
