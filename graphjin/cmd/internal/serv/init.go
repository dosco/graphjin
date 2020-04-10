package serv

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/dosco/super-graph/config"
	_ "github.com/jackc/pgx/v4/stdlib"
)

func initConf() (*config.Config, error) {
	return config.NewConfigWithLogger(confPath, log)
}

func initDB(c *config.Config) (*sql.DB, error) {
	var db *sql.DB
	var err error

	cs := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		c.DB.User, c.DB.Password,
		c.DB.Host, c.DB.Port, c.DB.DBName)

	for i := 1; i < 10; i++ {
		db, err = sql.Open("pgx", cs)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	if err != nil {
		return nil, err
	}

	return db, nil

	// config, _ := pgxpool.ParseConfig("")
	// config.ConnConfig.Host = c.DB.Host
	// config.ConnConfig.Port = c.DB.Port
	// config.ConnConfig.Database = c.DB.DBName
	// config.ConnConfig.User = c.DB.User
	// config.ConnConfig.Password = c.DB.Password
	// config.ConnConfig.RuntimeParams = map[string]string{
	// 	"application_name": c.AppName,
	// 	"search_path":      c.DB.Schema,
	// }

	// switch c.LogLevel {
	// case "debug":
	// 	config.ConnConfig.LogLevel = pgx.LogLevelDebug
	// case "info":
	// 	config.ConnConfig.LogLevel = pgx.LogLevelInfo
	// case "warn":
	// 	config.ConnConfig.LogLevel = pgx.LogLevelWarn
	// case "error":
	// 	config.ConnConfig.LogLevel = pgx.LogLevelError
	// default:
	// 	config.ConnConfig.LogLevel = pgx.LogLevelNone
	// }

	// config.ConnConfig.Logger = NewSQLLogger(logger)

	// // if c.DB.MaxRetries != 0 {
	// // 	opt.MaxRetries = c.DB.MaxRetries
	// // }

	// if c.DB.PoolSize != 0 {
	// 	config.MaxConns = conf.DB.PoolSize
	// }

	// var db *pgxpool.Pool
	// var err error

	// for i := 1; i < 10; i++ {
	// 	db, err = pgxpool.ConnectConfig(context.Background(), config)
	// 	if err == nil {
	// 		break
	// 	}
	// 	time.Sleep(time.Duration(i*100) * time.Millisecond)
	// }

	// if err != nil {
	// 	return nil, err
	// }

	// return db, nil
}
