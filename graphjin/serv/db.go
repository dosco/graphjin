package serv

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"contrib.go.opencensus.io/integrations/ocsql"
	"github.com/apex/log"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

const (
	pemSig = "--BEGIN "
)

const (
	logLevelNone int = iota
	logLevelInfo
	logLevelWarn
	logLevelError
	logLevelDebug
)

type dbConf struct {
	driverName string
	connString string
}

func (s *Service) NewDB() (*sql.DB, error) {
	return s.newDB(false, false)
}

func (s *Service) newDB(useDB, useTelemetry bool) (*sql.DB, error) {
	var db *sql.DB
	var dc *dbConf
	var err error

	switch s.conf.DBType {
	case "mysql":
		dc, err = initMysql(s.conf, useDB, useTelemetry)
	default:
		dc, err = initPostgres(s.conf, useDB, useTelemetry)
	}

	if useTelemetry && s.conf.telemetryEnabled() {
		dc.driverName, err = initTelemetry(s.conf, db, dc.driverName)
		if err != nil {
			return nil, err
		}

		var interval time.Duration

		if s.conf.Telemetry.Interval != nil {
			interval = *s.conf.Telemetry.Interval
		} else {
			interval = 5 * time.Second
		}

		defer ocsql.RecordStats(db, interval)()
	}

	for i := 1; i < 10; i++ {
		if db, err = sql.Open(dc.driverName, dc.connString); err == nil {
			break
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to open db connection: %v", err)
	}

	db.SetMaxIdleConns(100)
	return db, nil
}

func initPostgres(c *Config, useDB, useTelemetry bool) (*dbConf, error) {
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
			pem, err = ioutil.ReadFile(c.RelPath(c.DB.ServerCert))
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
			certs, err = tls.LoadX509KeyPair(c.RelPath(c.DB.ClientCert), c.RelPath(c.DB.ClientKey))
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

func initMysql(c *Config, useDB, useTelemetry bool) (*dbConf, error) {
	connString := fmt.Sprintf("%s:%s@tcp(%s:%d)/", c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port)

	if useDB {
		connString += c.DB.DBName
	}

	return &dbConf{"mysql", connString}, nil
}

func initTelemetry(c *Config, db *sql.DB, driverName string) (string, error) {
	var err error

	opts := ocsql.TraceOptions{
		AllowRoot:    true,
		Ping:         true,
		RowsNext:     true,
		RowsClose:    true,
		RowsAffected: true,
		LastInsertID: true,
		Query:        c.Telemetry.Tracing.IncludeQuery,
		QueryParams:  c.Telemetry.Tracing.IncludeParams,
	}
	opt := ocsql.WithOptions(opts)
	name := ocsql.WithInstanceName(c.AppName)

	driverName, err = ocsql.Register(driverName, opt, name)
	if err != nil {
		return "", fmt.Errorf("unable to register ocsql driver: %v", err)
	}
	ocsql.RegisterAllViews()

	log.Info("OpenCensus telemetry enabled")
	return driverName, nil
}
