package serv

import (
	"database/sql"
	"path"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
	//_ "github.com/jackc/pgx/v4/stdlib"
)

func initConf() (*Config, error) {
	c, err := ReadInConfig(path.Join(confPath, GetConfigName()))
	if err != nil {
		return nil, err
	}

	switch c.LogLevel {
	case "debug":
		logLevel = LogLevelDebug
	case "error":
		logLevel = LogLevelError
	case "warn":
		logLevel = LogLevelWarn
	case "info":
		logLevel = LogLevelInfo
	default:
		logLevel = LogLevelNone
	}

	// Auths: validate and sanitize
	am := make(map[string]struct{})

	for i := 0; i < len(c.Auths); i++ {
		a := &c.Auths[i]
		a.Name = sanitize(a.Name)

		if _, ok := am[a.Name]; ok {
			c.Auths = append(c.Auths[:i], c.Auths[i+1:]...)
			log.Printf("WRN duplicate auth found: %s", a.Name)
		}
		am[a.Name] = struct{}{}
	}

	// Actions: validate and sanitize
	axm := make(map[string]struct{})

	for i := 0; i < len(c.Actions); i++ {
		a := &c.Actions[i]
		a.Name = sanitize(a.Name)
		a.AuthName = sanitize(a.AuthName)

		if _, ok := axm[a.Name]; ok {
			c.Actions = append(c.Actions[:i], c.Actions[i+1:]...)
			log.Printf("WRN duplicate action found: %s", a.Name)
		}

		if _, ok := am[a.AuthName]; !ok {
			c.Actions = append(c.Actions[:i], c.Actions[i+1:]...)
			log.Printf("WRN invalid auth_name '%s' for auth: %s", a.AuthName, a.Name)
		}
		axm[a.Name] = struct{}{}
	}

	var anonFound bool

	for _, r := range c.Roles {
		if sanitize(r.Name) == "anon" {
			anonFound = true
		}
	}

	if !anonFound {
		log.Printf("WRN unauthenticated requests will be blocked. no role 'anon' defined")
		c.AuthFailBlock = false
	}

	return c, nil
}

func initDB(c *Config, useDB bool) (*sql.DB, error) {
	var db *sql.DB
	var err error

	// cs := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
	// 	c.DB.Host, c.DB.Port,
	// 	c.DB.User, c.DB.Password,
	// 	c.DB.DBName)

	// fmt.Println(">>", cs)

	// for i := 1; i < 10; i++ {
	// 	db, err = sql.Open("pgx", cs)
	// 	if err == nil {
	// 		break
	// 	}
	// 	time.Sleep(time.Duration(i*100) * time.Millisecond)
	// }

	// if err != nil {
	// 	return nil, err
	// }

	// return db, nil

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

	// switch c.LogLevel {
	// case "debug":
	// 	config.LogLevel = pgx.LogLevelDebug
	// case "info":
	// 	config.LogLevel = pgx.LogLevelInfo
	// case "warn":
	// 	config.LogLevel = pgx.LogLevelWarn
	// case "error":
	// 	config.LogLevel = pgx.LogLevelError
	// default:
	// 	config.LogLevel = pgx.LogLevelNone
	// }

	//config.Logger = NewSQLLogger(logger)

	// if c.DB.MaxRetries != 0 {
	// 	opt.MaxRetries = c.DB.MaxRetries
	// }

	// if c.DB.PoolSize != 0 {
	// 	config.MaxConns = conf.DB.PoolSize
	// }

	for i := 1; i < 10; i++ {
		db = stdlib.OpenDB(*config)
		if db == nil {
			break
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	if err != nil {
		return nil, err
	}

	return db, nil
}
