package serv

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//go:generate rice embed-go

const (
	serverName = "GraphJin"
)

var buildInfo BuildInfo

type ServConfig struct {
	log      *zap.SugaredLogger // logger
	zlog     *zap.Logger        // faster logger
	logLevel int                // log level
	conf     *Config            // parsed config
	confPath string             // path to config
	db       *sql.DB            // database connection pool
}

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func Cmd(bi BuildInfo) {
	buildInfo = bi
	servConf := new(ServConfig)
	servConf.log = newLogger(nil).Sugar()

	rootCmd := &cobra.Command{
		Use:   "graphjin",
		Short: BuildDetails(),
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "serv",
		Short: "Run the graphjin service",
		Run:   cmdServ(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:create",
		Short: "Create database",
		Run:   cmdDBCreate(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:drop",
		Short: "Drop database",
		Run:   cmdDBDrop(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:seed",
		Short: "Run the seed script to seed the database",
		Run:   cmdDBSeed(servConf),
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
		Run: cmdDBMigrate(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:status",
		Short: "Print current migration status",
		Run:   cmdDBStatus(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:new NAME",
		Short: "Generate a new migration",
		Long:  "Generate a new migration with the next sequence number and provided name",
		Run:   cmdDBNew(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:setup",
		Short: "Setup database",
		Long:  "This command will create, migrate and seed the database",
		Run:   cmdDBSetup(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:reset",
		Short: "Reset database",
		Long:  "This command will drop, create, migrate and seed the database (won't run in production)",
		Run:   cmdDBReset(servConf),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "new APP-NAME",
		Short: "Create a new application",
		Long:  "Generate all the required files to start on a new GraphJin app",
		Run:   cmdNew(servConf),
	})

	// rootCmd.AddCommand(&cobra.Command{
	// 	Use:   fmt.Sprintf("conf:dump [%s]", strings.Join(viper.SupportedExts, "|")),
	// 	Short: "Dump config to file",
	// 	Long:  "Dump current config to a file in the selected format",
	// 	Run:   cmdConfDump,
	// })

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "GraphJin binary version information",
		Run:   cmdVersion,
	})

	rootCmd.PersistentFlags().StringVar(&servConf.confPath,
		"path", "./config", "path to config files")

	if err := rootCmd.Execute(); err != nil {
		servConf.log.Fatalf("Error: %s", err)
	}
}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

func BuildDetails() string {
	if buildInfo.Version == "" {
		return `
GraphJin (unknown version)
For documentation, visit https://graphjin.com

To build with version information please use the Makefile
> git clone https://github.com/dosco/graphjin
> cd graphjin && make install

Licensed under the Apache Public License 2.0
Copyright 2021, Vikram Rangnekar
`
	}

	return fmt.Sprintf(`
GraphJin %v 
For documentation, visit https://graphjin.com

Commit SHA-1          : %v
Commit timestamp      : %v
Go version            : %v

Licensed under the Apache Public License 2.0
Copyright 2021, Vikram Rangnekar
`,
		buildInfo.Version,
		buildInfo.Commit,
		buildInfo.Date,
		runtime.Version())
}

func newLogger(sc *ServConfig) *zap.Logger {
	econf := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		NameKey:        "logger",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	var core zapcore.Core

	if sc != nil && sc.conf.LogFormat == "json" {
		core = zapcore.NewCore(zapcore.NewJSONEncoder(econf), os.Stdout, zap.DebugLevel)
	} else {
		econf.EncodeLevel = zapcore.CapitalColorLevelEncoder
		core = zapcore.NewCore(zapcore.NewConsoleEncoder(econf), os.Stdout, zap.DebugLevel)
	}
	return zap.New(core)
}
