package serv

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/dosco/super-graph/allow"
	"github.com/dosco/super-graph/crypto"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rs/zerolog"
)

func initLog() {
	out := zerolog.ConsoleWriter{Out: os.Stderr}
	logger = zerolog.New(out).With().Timestamp().Logger()
	errlog = logger.With().Caller().Logger()
}

func initConf() (*config, error) {
	vi := newConfig(getConfigName())

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	inherits := vi.GetString("inherits")
	if len(inherits) != 0 {
		vi = newConfig(inherits)

		if err := vi.ReadInConfig(); err != nil {
			return nil, err
		}

		if vi.IsSet("inherits") {
			errlog.Fatal().Msgf("inherited config (%s) cannot itself inherit (%s)",
				inherits,
				vi.GetString("inherits"))
		}

		vi.SetConfigName(getConfigName())

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	c := &config{}

	if err := c.Init(vi); err != nil {
		return nil, fmt.Errorf("unable to decode config, %v", err)
	}

	logLevel, err := zerolog.ParseLevel(c.LogLevel)
	if err != nil {
		errlog.Error().Err(err).Msg("error setting log_level")
	}
	zerolog.SetGlobalLevel(logLevel)

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

	config.Logger = NewSQLLogger(logger)

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

	config.ConnConfig.Logger = NewSQLLogger(logger)

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
		errlog.Fatal().Err(err).Msg("failed to initialize compilers")
	}
}

func initAllowList(cpath string) {
	var ac allow.Config
	var err error

	if !conf.Production {
		ac = allow.Config{CreateIfNotExists: true, Persist: true}
	}

	allowList, err = allow.New(cpath, ac)
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to initialize allow list")
	}
}

func initCrypto() {
	if len(conf.SecretKey) != 0 {
		secretKey = sha256.Sum256([]byte(conf.SecretKey))
		conf.SecretKey = ""
		internalKey = secretKey

	} else {
		internalKey = crypto.NewEncryptionKey()
	}
}
