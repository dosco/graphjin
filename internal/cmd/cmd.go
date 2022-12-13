package cmd

import (
	"database/sql"
	"path"
	"path/filepath"

	"github.com/dosco/graphjin/internal/util"
	"github.com/dosco/graphjin/plugin/fs"
	"github.com/dosco/graphjin/serv"
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
	rootCmd.AddCommand(upgradeCmd())

	if v := cmdSecrets(); v != nil {
		rootCmd.AddCommand(v)
	}

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
	setupAgain(cpath)
}

func setupAgain(cpath string) {
	cp, err := filepath.Abs(cpath)
	if err != nil {
		log.Fatal(err)
	}

	cn := serv.GetConfigName()
	if conf, err = serv.ReadInConfig(path.Join(cp, cn)); err != nil {
		log.Fatal(err)
	}
}

func initDB(openDB bool) {
	var err error

	if db != nil && openDB == dbOpened {
		return
	}

	fs := fs.NewOsFSWithBase(cpath)

	if db, err = serv.NewDB(conf, openDB, log, fs); err != nil {
		log.Fatalf("Failed to connect to database: %s", err)
	}
	dbOpened = openDB
}
