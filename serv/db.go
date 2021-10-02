package serv

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"contrib.go.opencensus.io/integrations/ocsql"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/spf13/afero"
	"go.uber.org/zap"
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

func NewDB(conf *Config, openDB bool, log *zap.SugaredLogger, fs afero.Fs) (*sql.DB, error) {
	return newDB(conf, openDB, false, log, fs)
}

func newDB(
	conf *Config,
	openDB, useTelemetry bool,
	log *zap.SugaredLogger,
	fs afero.Fs) (*sql.DB, error) {

	var db *sql.DB
	var dc *dbConf
	var err error

	switch conf.Core.DBType {
	case "mysql":
		dc, err = initMysql(conf, openDB, useTelemetry, fs)
	default:
		dc, err = initPostgres(conf, openDB, useTelemetry, fs)
	}

	if err != nil {
		return nil, fmt.Errorf("database init: %v", err)
	}

	if useTelemetry && conf.telemetryEnabled() {
		dc.driverName, err = initTelemetry(conf, dc.driverName)
		if err != nil {
			return nil, err
		}

		var interval time.Duration

		if conf.Telemetry.Interval != nil {
			interval = *conf.Telemetry.Interval
		} else {
			interval = 5 * time.Second
		}

		defer ocsql.RecordStats(db, interval)()
		log.Infof("open-census telemetry enabled")
	}

	for i := 0; ; {
		if db, err = sql.Open(dc.driverName, dc.connString); err != nil {
			log.Warnf("database open: %s", err)
		}

		if err = db.Ping(); err != nil {
			log.Warnf("database ping: %s", err)
		} else {
			break
		}

		time.Sleep(time.Duration(i*100) * time.Millisecond)

		if i > 50 {
			i = 0
		} else {
			i++
		}
	}

	db.SetMaxIdleConns(100)
	return db, nil
}

func initTelemetry(conf *Config, driverName string) (string, error) {
	var err error
	var opts ocsql.TraceOptions

	if conf.Serv.Telemetry.Tracing.ExcludeHealthCheck {
		opts = ocsql.TraceOptions{
			AllowRoot:    true,
			Ping:         false,
			RowsNext:     true,
			RowsClose:    true,
			RowsAffected: true,
			LastInsertID: true,
			Query:        conf.Telemetry.Tracing.IncludeQuery,
			QueryParams:  conf.Telemetry.Tracing.IncludeParams,
		}
	} else {
		opts = ocsql.TraceOptions{
			AllowRoot:    true,
			Ping:         true,
			RowsNext:     true,
			RowsClose:    true,
			RowsAffected: true,
			LastInsertID: true,
			Query:        conf.Telemetry.Tracing.IncludeQuery,
			QueryParams:  conf.Telemetry.Tracing.IncludeParams,
		}
	}

	opt := ocsql.WithOptions(opts)
	name := ocsql.WithInstanceName(conf.AppName)

	driverName, err = ocsql.Register(driverName, opt, name)
	if err != nil {
		return "", fmt.Errorf("unable to register ocsql driver: %v", err)
	}
	ocsql.RegisterAllViews()
	return driverName, nil
}

func initPostgres(conf *Config, openDB, useTelemetry bool, fs afero.Fs) (*dbConf, error) {
	c := conf
	config, _ := pgx.ParseConfig("")
	config.Host = c.DB.Host
	config.Port = c.DB.Port
	config.User = c.DB.User
	config.Password = c.DB.Password

	config.RuntimeParams = map[string]string{
		"application_name": c.AppName,
		"search_path":      c.DB.Schema,
	}

	if openDB {
		config.Database = c.DB.DBName
	}

	if c.DB.EnableTLS {
		if len(c.DB.ServerName) == 0 {
			return nil, errors.New("tls: server_name is required")
		}
		if len(c.DB.ServerCert) == 0 {
			return nil, errors.New("tls: server_cert is required")
		}

		rootCertPool := x509.NewCertPool()
		var pem []byte
		var err error

		if strings.Contains(c.DB.ServerCert, pemSig) {
			pem = []byte(strings.ReplaceAll(c.DB.ServerCert, `\n`, "\n"))
		} else {
			pem, err = afero.ReadFile(fs, c.DB.ServerCert)
		}

		if err != nil {
			return nil, fmt.Errorf("tls: %w", err)
		}

		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			return nil, errors.New("tls: failed to append pem")
		}

		config.TLSConfig = &tls.Config{
			RootCAs:    rootCertPool,
			ServerName: c.DB.ServerName,
		}

		if len(c.DB.ClientCert) > 0 {
			if len(c.DB.ClientKey) == 0 {
				return nil, errors.New("tls: client_key is required")
			}

			clientCert := make([]tls.Certificate, 0, 1)
			var certs tls.Certificate

			if strings.Contains(c.DB.ClientCert, pemSig) {
				certs, err = tls.X509KeyPair(
					[]byte(strings.ReplaceAll(c.DB.ClientCert, `\n`, "\n")),
					[]byte(strings.ReplaceAll(c.DB.ClientKey, `\n`, "\n")),
				)
			} else {
				certs, err = loadX509KeyPair(fs, c.DB.ClientCert, c.DB.ClientKey)
			}

			if err != nil {
				return nil, fmt.Errorf("tls: %w", err)
			}

			clientCert = append(clientCert, certs)
			config.TLSConfig.Certificates = clientCert
		}
	}

	return &dbConf{"pgx", stdlib.RegisterConnConfig(config)}, nil
}

func initMysql(conf *Config, openDB, useTelemetry bool, fs afero.Fs) (*dbConf, error) {
	c := conf
	connString := fmt.Sprintf("%s:%s@tcp(%s:%d)/", c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port)

	if openDB {
		connString += c.DB.DBName
	}

	return &dbConf{"mysql", connString}, nil
}

func loadX509KeyPair(fs afero.Fs, certFile, keyFile string) (tls.Certificate, error) {
	certPEMBlock, err := afero.ReadFile(fs, certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEMBlock, err := afero.ReadFile(fs, keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEMBlock, keyPEMBlock)
}
