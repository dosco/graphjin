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

	// postgres drivers
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"

	// mysql drivers
	_ "github.com/go-sql-driver/mysql"
)

const (
	pemSig = "--BEGIN "
)

func initConf(servConfig *ServConfig) (*Config, error) {
	cp, err := filepath.Abs(servConfig.confPath)
	if err != nil {
		return nil, err
	}

	c, err := ReadInConfig(path.Join(cp, GetConfigName()))
	if err != nil {
		return nil, err
	}

	switch c.LogLevel {
	case "debug":
		servConfig.logLevel = LogLevelDebug
	case "error":
		servConfig.logLevel = LogLevelError
	case "warn":
		servConfig.logLevel = LogLevelWarn
	case "info":
		servConfig.logLevel = LogLevelInfo
	default:
		servConfig.logLevel = LogLevelNone
	}

	// copy over db_type from database.type
	if c.Core.DBType == "" {
		c.Core.DBType = c.DB.Type
	}

	// Auths: validate and sanitize
	am := make(map[string]struct{})

	for i := 0; i < len(c.Auths); i++ {
		a := &c.Auths[i]

		if _, ok := am[a.Name]; ok {
			return nil, fmt.Errorf("Duplicate auth found: %s", a.Name)
		}
		am[a.Name] = struct{}{}
	}

	// Actions: validate and sanitize
	axm := make(map[string]struct{})

	for i := 0; i < len(c.Actions); i++ {
		a := &c.Actions[i]

		if _, ok := axm[a.Name]; ok {
			return nil, fmt.Errorf("Duplicate action found: %s", a.Name)
		}

		if _, ok := am[a.AuthName]; !ok {
			return nil, fmt.Errorf("Invalid auth name: %s, For auth: %s", a.AuthName, a.Name)
		}
		axm[a.Name] = struct{}{}
	}

	var anonFound bool

	for _, r := range c.Roles {
		if r.Name == "anon" {
			anonFound = true
		}
	}

	if c.Auth.Type == "" || c.Auth.Type == "none" {
		c.DefaultBlock = false
	}

	if !anonFound && c.DefaultBlock {
		servConfig.log.Warn("Unauthenticated requests will be blocked. no role 'anon' defined")
		c.AuthFailBlock = false
	}

	if c.AllowListFile == "" {
		c.AllowListFile = c.relPath("./allow.list")
	}

	return c, nil
}

type dbConf struct {
	driverName string
	connString string
}

func initDB(servConfig *ServConfig, useDB, useTelemetry bool) (*sql.DB, error) {
	var db *sql.DB
	var dc *dbConf
	var err error

	switch servConfig.conf.DBType {
	case "mysql":
		dc, err = initMysql(servConfig, useDB, useTelemetry)
	default:
		dc, err = initPostgres(servConfig, useDB, useTelemetry)
	}

	if useTelemetry && servConfig.conf.telemetryEnabled() {
		dc.driverName, err = initTelemetry(servConfig, db, dc.driverName)
		if err != nil {
			return nil, err
		}

		var interval time.Duration

		if servConfig.conf.Telemetry.Interval != nil {
			interval = *servConfig.conf.Telemetry.Interval
		} else {
			interval = 5 * time.Second
		}

		defer ocsql.RecordStats(db, interval)()
	}

	for i := 1; i < 10; i++ {
		db, err = sql.Open(dc.driverName, dc.connString)
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

func initPostgres(servConfig *ServConfig, useDB, useTelemetry bool) (*dbConf, error) {
	c := servConfig.conf

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

		if strings.Contains(c.DB.ServerCert, pemSig) {
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

		if strings.Contains(c.DB.ClientCert, pemSig) {
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

	return &dbConf{"pgx", stdlib.RegisterConnConfig(config)}, nil
}

func initMysql(servConfig *ServConfig, useDB, useTelemetry bool) (*dbConf, error) {
	c := servConfig.conf
	connString := fmt.Sprintf("%s:%s@tcp(%s:%d)/", c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port)

	if useDB {
		connString += c.DB.DBName
	}

	return &dbConf{"mysql", connString}, nil
}

func initTelemetry(servConfig *ServConfig, db *sql.DB, driverName string) (string, error) {
	var err error

	opts := ocsql.TraceOptions{
		AllowRoot:    true,
		Ping:         true,
		RowsNext:     true,
		RowsClose:    true,
		RowsAffected: true,
		LastInsertID: true,
		Query:        servConfig.conf.Telemetry.Tracing.IncludeQuery,
		QueryParams:  servConfig.conf.Telemetry.Tracing.IncludeParams,
	}
	opt := ocsql.WithOptions(opts)
	name := ocsql.WithInstanceName(servConfig.conf.AppName)

	driverName, err = ocsql.Register(driverName, opt, name)
	if err != nil {
		return "", fmt.Errorf("unable to register ocsql driver: %v", err)
	}
	ocsql.RegisterAllViews()

	servConfig.log.Info("OpenCensus telemetry enabled")
	return driverName, nil
}
