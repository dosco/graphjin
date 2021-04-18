package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/dosco/graphjin/internal/util"
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	s      *serv.Service
	bi     serv.BuildInfo
	log    *zap.SugaredLogger
	db     *sql.DB
	dbUsed bool
	conf   *serv.Config
	cpath  string
)

func Cmd() {
	bi = serv.GetBuildInfo()
	log = util.NewLogger(false).Sugar()

	rootCmd := &cobra.Command{
		Use:   "graphjin",
		Short: BuildDetails(),
	}

	rootCmd.PersistentFlags().StringVar(&cpath,
		"path", "./config", "path to config files")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "serv",
		Short: "Run the graphjin service",
		Run:   cmdServ(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:create",
		Short: "Create database",
		Run:   cmdDBCreate(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:drop",
		Short: "Drop database",
		Run:   cmdDBDrop(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:seed",
		Short: "Run the seed script to seed the database",
		Run:   cmdDBSeed(),
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
		Run: cmdDBMigrate(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:status",
		Short: "Print current migration status",
		Run:   cmdDBStatus(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:new NAME",
		Short: "Generate a new migration",
		Long:  "Generate a new migration with the next sequence number and provided name",
		Run:   cmdDBNew(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:setup",
		Short: "Setup database",
		Long:  "This command will create, migrate and seed the database",
		Run:   cmdDBSetup(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "db:reset",
		Short: "Reset database",
		Long:  "This command will drop, create, migrate and seed the database (won't run in production)",
		Run:   cmdDBReset(),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "new APP-NAME",
		Short: "Create a new application",
		Long:  "Generate all the required files to start on a new GraphJin app",
		Run:   cmdNew(),
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

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error: %s", err)
	}

}

func cmdVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s\n", BuildDetails())
}

func BuildDetails() string {
	if bi.Version == "" {
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
		bi.Version,
		bi.Commit,
		bi.Date,
		runtime.Version())
}

func initCmd(cpath string) {
	if conf != nil {
		return
	}
	cp, err := filepath.Abs(cpath)
	if err != nil {
		log.Fatal(err)
	}

	if conf, err = serv.ReadInConfig(path.Join(cp, GetConfigName())); err != nil {
		log.Fatal(err)
	}

	if s, err = serv.NewGraphJinService(conf); err != nil {
		log.Fatal(err)
	}
}

func initDB(useDB bool) {
	var err error

	if db != nil && useDB == dbUsed {
		return
	}

	if db, err = s.NewDB(useDB); err != nil {
		log.Fatalf("Failed to connect to database: %s", err)
	}
	dbUsed = useDB
}

func GetConfigName() string {
	if os.Getenv("GO_ENV") == "" {
		return "dev"
	}

	ge := strings.ToLower(os.Getenv("GO_ENV"))

	switch {
	case strings.HasPrefix(ge, "pro"):
		return "prod"

	case strings.HasPrefix(ge, "sta"):
		return "stage"

	case strings.HasPrefix(ge, "tes"):
		return "test"

	case strings.HasPrefix(ge, "dev"):
		return "dev"
	}

	return ge
}

func fatalInProd(err error) {
	var wg sync.WaitGroup

	if isDev() {
		log.Error(err)
	} else {
		log.Fatal(err)
	}

	wg.Add(1)
	wg.Wait()
}

func isDev() bool {
	return strings.HasPrefix(os.Getenv("GO_ENV"), "dev")
}
