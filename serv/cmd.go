package serv

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/gobuffalo/flect"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

//go:generate rice embed-go

const (
	serverName = "Super Graph"

	authFailBlockAlways = iota + 1
	authFailBlockPerQuery
	authFailBlockNever
)

var (
	logger        *zerolog.Logger
	conf          *config
	confPath      string
	db            *pgxpool.Pool
	qcompile      *qcode.Compiler
	pcompile      *psql.Compiler
	authFailBlock int
)

func Init() {
	logger = initLog()

	rootCmd := &cobra.Command{
		Use:   "super-graph",
		Short: "An instant high-performance GraphQL API. No code needed. https://supergraph.dev",
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

	rootCmd.Flags().StringVar(&confPath,
		"path", "./config", "path to config files")

	if err := rootCmd.Execute(); err != nil {
		logger.Fatal().Err(err).Send()
	}
}

func initLog() *zerolog.Logger {
	out := zerolog.ConsoleWriter{Out: os.Stderr}
	logger := zerolog.New(out).
		With().
		Timestamp().
		Caller().
		Logger()

	return &logger
}

func initConf() (*config, error) {
	vi := newConfig()

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	c := &config{}

	if err := vi.Unmarshal(c); err != nil {
		return nil, fmt.Errorf("unable to decode config, %v", err)
	}

	if len(c.Tables) == 0 {
		c.Tables = c.DB.Tables
	}

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	for i := range c.Tables {
		t := c.Tables[i]
		t.Name = flect.Pluralize(strings.ToLower(t.Name))
	}

	authFailBlock = getAuthFailBlock(c)

	logLevel, err := zerolog.ParseLevel(c.LogLevel)
	if err != nil {
		logger.Error().Err(err).Msg("error setting log_level")
	}
	zerolog.SetGlobalLevel(logLevel)

	for k, v := range c.DB.Vars {
		c.DB.Vars[k] = sanitize(v)
	}

	c.RolesQuery = sanitize(c.RolesQuery)

	rolesMap := make(map[string]struct{})

	for i := range c.Roles {
		role := &c.Roles[i]

		if _, ok := rolesMap[role.Name]; ok {
			logger.Fatal().Msgf("duplicate role '%s' found", role.Name)
		}
		role.Name = sanitize(role.Name)
		role.Match = sanitize(role.Match)
		rolesMap[role.Name] = struct{}{}
	}

	if _, ok := rolesMap["user"]; !ok {
		c.Roles = append(c.Roles, configRole{Name: "user"})
	}

	if _, ok := rolesMap["anon"]; !ok {
		c.Roles = append(c.Roles, configRole{Name: "anon"})
	}

	return c, nil
}

func initDB(c *config, useDB bool) (*pgx.Conn, error) {
	config, _ := pgx.ParseConfig("")
	config.Host = c.DB.Host
	config.Port = c.DB.Port
	config.User = c.DB.User
	config.Password = c.DB.Password
	config.RuntimeParams = map[string]string{
		"application_name": c.AppName,
		"search_path":      c.DB.Schema,
	}

	if useDB {
		config.Database = c.DB.DBName
	}

	switch c.LogLevel {
	case "debug":
		config.LogLevel = pgx.LogLevelDebug
	case "info":
		config.LogLevel = pgx.LogLevelInfo
	case "warn":
		config.LogLevel = pgx.LogLevelWarn
	case "error":
		config.LogLevel = pgx.LogLevelError
	default:
		config.LogLevel = pgx.LogLevelNone
	}

	config.Logger = NewSQLLogger(*logger)

	db, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func initDBPool(c *config) (*pgxpool.Pool, error) {
	config, _ := pgxpool.ParseConfig("")
	config.ConnConfig.Host = c.DB.Host
	config.ConnConfig.Port = c.DB.Port
	config.ConnConfig.Database = c.DB.DBName
	config.ConnConfig.User = c.DB.User
	config.ConnConfig.Password = c.DB.Password
	config.ConnConfig.RuntimeParams = map[string]string{
		"application_name": c.AppName,
		"search_path":      c.DB.Schema,
	}

	switch c.LogLevel {
	case "debug":
		config.ConnConfig.LogLevel = pgx.LogLevelDebug
	case "info":
		config.ConnConfig.LogLevel = pgx.LogLevelInfo
	case "warn":
		config.ConnConfig.LogLevel = pgx.LogLevelWarn
	case "error":
		config.ConnConfig.LogLevel = pgx.LogLevelError
	default:
		config.ConnConfig.LogLevel = pgx.LogLevelNone
	}

	config.ConnConfig.Logger = NewSQLLogger(*logger)

	// if c.DB.MaxRetries != 0 {
	// 	opt.MaxRetries = c.DB.MaxRetries
	// }

	if c.DB.PoolSize != 0 {
		config.MaxConns = conf.DB.PoolSize
	}

	db, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func initCompiler() {
	var err error

	qcompile, pcompile, err = initCompilers(conf)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize compilers")
	}

	if err := initResolvers(); err != nil {
		logger.Fatal().Err(err).Msg("failed to initialized resolvers")
	}
}
