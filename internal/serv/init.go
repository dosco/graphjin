package serv

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"
	"time"

	"contrib.go.opencensus.io/integrations/ocsql"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
	//_ "github.com/jackc/pgx/v4/stdlib"
)

const (
	PEM_SIG = "--BEGIN "
)

func initConf() (*Config, error) {
	cp, err := filepath.Abs(confPath)
	if err != nil {
		return nil, err
	}

	c, err := ReadInConfig(path.Join(cp, GetConfigName()))
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

	// copy over db_schema from the core config
	if c.DB.Schema == "" {
		c.DB.Schema = c.DBSchema
	}

	// set default database schema
	if c.DB.Schema == "" {
		c.DB.Schema = "public"
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

	if c.AllowListFile == "" {
		c.AllowListFile = c.relPath("./allow.list")
	}

	if c.Production {
		c.UseAllowList = true
	}

	return c, nil
}

func initDB(c *Config, useDB, useTelemetry bool) (*sql.DB, error) {
	var db *sql.DB
	var err error

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

	if c.DB.EnableTLS {
		if len(c.DB.ServerName) == 0 {
			return nil, errors.New("server_name is required")
		}
		if len(c.DB.ServerCert) == 0 {
			return nil, errors.New("server_cert is required")
		}
		if len(c.DB.ClientCert) == 0 {
			return nil, errors.New("client_cert is required")
		}
		if len(c.DB.ClientKey) == 0 {
			return nil, errors.New("client_key is required")
		}

		rootCertPool := x509.NewCertPool()
		var pem []byte
		var err error

		if strings.Contains(c.DB.ServerCert, PEM_SIG) {
			pem = []byte(c.DB.ServerCert)
		} else {
			pem, err = ioutil.ReadFile(c.relPath(c.DB.ServerCert))
		}

		if err != nil {
			return nil, fmt.Errorf("db tls: %w", err)
		}

		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			return nil, errors.New("db tls: failed to append pem")
		}

		clientCert := make([]tls.Certificate, 0, 1)
		var certs tls.Certificate

		if strings.Contains(c.DB.ClientCert, PEM_SIG) {
			certs, err = tls.X509KeyPair([]byte(c.DB.ClientCert), []byte(c.DB.ClientKey))
		} else {
			certs, err = tls.LoadX509KeyPair(c.relPath(c.DB.ClientCert), c.relPath(c.DB.ClientKey))
		}

		if err != nil {
			return nil, fmt.Errorf("db tls: %w", err)
		}

		clientCert = append(clientCert, certs)
		config.TLSConfig = &tls.Config{
			RootCAs:      rootCertPool,
			Certificates: clientCert,
			ServerName:   c.DB.ServerName,
		}
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

	connString := stdlib.RegisterConnConfig(config)
	driverName := "pgx"
	// if db = stdlib.OpenDB(*config); db == nil {
	// 	return errors.New("failed to open db")
	// }

	if useTelemetry && conf.telemetryEnabled() {
		opts := ocsql.TraceOptions{
			AllowRoot:    true,
			Ping:         true,
			RowsNext:     true,
			RowsClose:    true,
			RowsAffected: true,
			LastInsertID: true,
			Query:        conf.Telemetry.Tracing.IncludeQuery,
			QueryParams:  conf.Telemetry.Tracing.IncludeParams,
		}
		opt := ocsql.WithOptions(opts)
		name := ocsql.WithInstanceName(conf.AppName)

		driverName, err = ocsql.Register(driverName, opt, name)
		if err != nil {
			return nil, fmt.Errorf("unable to register ocsql driver: %v", err)
		}
		ocsql.RegisterAllViews()

		var interval time.Duration

		if conf.Telemetry.Interval != nil {
			interval = *conf.Telemetry.Interval
		} else {
			interval = 5 * time.Second
		}

		defer ocsql.RecordStats(db, interval)()

		log.Println("INF OpenCensus telemetry enabled")
	}

	for i := 1; i < 10; i++ {
		db, err = sql.Open(driverName, connString)
		if err != nil {
			continue
		}

		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to open db connection: %v", err)
	}

	return db, nil
}
