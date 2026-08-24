package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

	if err := newRootCmd().Execute(); err != nil {
		var exitErr *evalExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, exitErr.Err)
			os.Exit(exitErr.Code)
		}
		log.Fatalf("%s", err)
	}
}

// newRootCmd builds the CLI command tree. Cmd executes it; tests parse
// generated command lines against it so a command the CLI prints for an
// operator to retype is checked against the flags the binary really exposes.
func newRootCmd() *cobra.Command {
	cobra.EnableCommandSorting = false
	rootCmd := &cobra.Command{
		Use:   "graphjin",
		Short: BuildDetails(),
	}

	rootCmd.PersistentFlags().StringVar(&cpath,
		"path", "./config", "path to config files")

	// Add --config as an alias for --path
	rootCmd.PersistentFlags().StringVar(&cpath,
		"config", "./config", "alias for --path")
	rootCmd.PersistentFlags().MarkHidden("config")

	// Top-level commands. Server-side lifecycle commands (new, db, test) are
	// children of `serve` — see servCmd() in cmd_serv.go.
	rootCmd.AddCommand(servCmd())
	rootCmd.AddCommand(mcpCmd())
	rootCmd.AddCommand(cliCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(evalCmd())
	rootCmd.AddCommand(versionCmd())

	// rootCmd.AddCommand(&cobra.Command{
	// 	Use:   fmt.Sprintf("conf:dump [%s]", strings.Join(viper.SupportedExts, "|")),
	// 	Short: "Dump config to file",
	// 	Long:  "Dump current config to a file in the selected format",
	// 	Run:   cmdConfDump,
	// })

	return rootCmd
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

	// Auto-create config directory and default config file only if the
	// config directory itself does not exist. If the directory is already
	// present we preserve the original behaviour and let ReadInConfig
	// report any missing file errors.
	if _, err := os.Stat(cp); os.IsNotExist(err) {
		if err := os.MkdirAll(cp, os.ModePerm); err != nil {
			log.Fatalf("Failed to create config directory: %s", err)
		}

		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		appNameSlug := strings.ToLower(filepath.Base(cwd))
		en := cases.Title(language.English)
		appName := en.String(appNameSlug)

		tmpl := newTempl(map[string]interface{}{
			"AppName":     appName,
			"AppNameSlug": appNameSlug,
			"DBType":      "postgres",
			"DBHost":      "db",
			"DBPort":      "5432",
			"DBUser":      "postgres",
			"DBPass":      "postgres",
			"DBName":      "",
		})

		templateNames := []string{cn + ".yml"}
		if cn == "dev" || cn == "prod" || cn == "agentic" {
			templateNames = []string{"dev.yml", "prod.yml", "agentic.yml"}
		}
		for _, templateName := range templateNames {
			configFile := filepath.Join(cp, templateName)
			v, err := tmpl.get(templateName)
			if err != nil {
				log.Fatalf("Failed to generate default config: %s", err)
			}
			if err := os.WriteFile(configFile, v, 0o600); err != nil {
				log.Fatalf("Failed to write default config: %s", err)
			}
			log.Infof("Created default config: %s", configFile)
		}

		// Schema referenced by the modeline in the generated config files.
		schemaFile := filepath.Join(cp, configSchemaFile)
		if err := os.WriteFile(schemaFile, serv.ConfigJSONSchema(), 0o600); err != nil {
			log.Fatalf("Failed to write config schema: %s", err)
		}
		log.Infof("Created config schema: %s", schemaFile)
	}

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
	return newLoggerWithOutput(json, os.Stdout)
}

// newLoggerWithOutput creates a new logger with a custom output
func newLoggerWithOutput(json bool, output zapcore.WriteSyncer) *zap.Logger {
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
		core = zapcore.NewCore(zapcore.NewJSONEncoder(econf), output, zap.DebugLevel)
	} else {
		econf.EncodeLevel = zapcore.CapitalColorLevelEncoder
		core = zapcore.NewCore(zapcore.NewConsoleEncoder(econf), output, zap.DebugLevel)
	}
	return zap.New(core)
}
