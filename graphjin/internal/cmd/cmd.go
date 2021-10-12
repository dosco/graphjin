package cmd

import (
	"database/sql"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dosco/graphjin/internal/util"
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	bi       serv.BuildInfo
	log      *zap.SugaredLogger
	db       *sql.DB
	dbOpened bool
	conf     *serv.Config
	cpath    string
)

func Cmd() {
	bi = serv.GetBuildInfo()
	log = util.NewLogger(false).Sugar()

	cobra.EnableCommandSorting = false
	rootCmd := &cobra.Command{
		Use:   "graphjin",
		Short: BuildDetails(),
	}

	rootCmd.PersistentFlags().StringVar(&cpath,
		"path", "./config", "path to config files")

	rootCmd.AddCommand(newCmd())
	rootCmd.AddCommand(servCmd())
	rootCmd.AddCommand(versionCmd())

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(deployCmd())

	rootCmd.AddCommand(dbCmd())

	rootCmd.AddCommand(cmdSecrets())

	// rootCmd.AddCommand(&cobra.Command{
	// 	Use:   fmt.Sprintf("conf:dump [%s]", strings.Join(viper.SupportedExts, "|")),
	// 	Short: "Dump config to file",
	// 	Long:  "Dump current config to a file in the selected format",
	// 	Run:   cmdConfDump,
	// })

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("%s", err)
	}

}

func setup(cpath string) {
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
}

func initDB(openDB bool) {
	var err error

	if db != nil && openDB == dbOpened {
		return
	}

	fs := afero.NewBasePathFs(afero.NewOsFs(), cpath)

	if db, err = serv.NewDB(conf, openDB, log, fs); err != nil {
		log.Fatalf("Failed to connect to database: %s", err)
	}
	dbOpened = openDB
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
