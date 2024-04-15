package main

import (
	"database/sql"
	"os"
	"path"
	"path/filepath"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// These variables are set using -ldflags
	version string
	commit  string
	date    string
)

var (
	log      *zap.SugaredLogger
	db       *sql.DB
	dbOpened bool
	conf     *serv.Config
	cpath    string
)

// Cmd is the entry point for the CLI
func Cmd() {
	log = newLogger(false).Sugar()

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

// setup is a helper function to read the config file
func setup(cpath string) {
	if conf != nil {
		return
	}

	cp, err := filepath.Abs(cpath)
	if err != nil {
		log.Fatal(err)
	}

	cn := serv.GetConfigName()
	if conf, err = serv.ReadInConfig(path.Join(cp, cn)); err != nil {
		log.Fatal(err)
	}
}

// initDB is a helper function to initialize the database connection
func initDB(openDB bool) {
	var err error

	if db != nil && openDB == dbOpened {
		return
	}
	fs := core.NewOsFS(cpath)

	if db, err = serv.NewDB(conf, openDB, log, fs); err != nil {
		log.Fatalf("Failed to connect to database: %s", err)
	}
	dbOpened = openDB
}

// newLogger creates a new logger
func newLogger(json bool) *zap.Logger {
	econf := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		NameKey:        "logger",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	var core zapcore.Core

	if json {
		core = zapcore.NewCore(zapcore.NewJSONEncoder(econf), os.Stdout, zap.DebugLevel)
	} else {
		econf.EncodeLevel = zapcore.CapitalColorLevelEncoder
		core = zapcore.NewCore(zapcore.NewConsoleEncoder(econf), os.Stdout, zap.DebugLevel)
	}
	return zap.New(core)
}
