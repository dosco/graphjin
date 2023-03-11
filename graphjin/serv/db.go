package serv

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	_ "github.com/go-sql-driver/mysql"
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

func NewDB(conf *Config, openDB bool, log *zap.SugaredLogger, fs core.FS) (*sql.DB, error) {
	return newDB(conf, openDB, false, log, fs)
}

func newDB(
	conf *Config,
	openDB, useTelemetry bool,
	log *zap.SugaredLogger,
	fs core.FS,
) (*sql.DB, error) {
	var db *sql.DB
	var dc *dbConf
	var err error

	if cs := conf.DB.ConnString; cs != "" {
		if strings.HasPrefix(cs, "postgres://") {
			conf.Core.DBType = "postgres"
		}
		if strings.HasPrefix(cs, "mysql://") {
			conf.Core.DBType = "mysql"
			conf.DB.ConnString = strings.TrimPrefix(cs, "mysql://")
		}
	}

	switch conf.Core.DBType {
	case "mysql":
		dc, err = initMysql(conf, openDB, useTelemetry, fs)
	default:
		dc, err = initPostgres(conf, openDB, useTelemetry, fs)
	}

	if err != nil {
		return nil, fmt.Errorf("database init: %v", err)
	}

	for i := 0; ; {
		if db, err = sql.Open(dc.driverName, dc.connString); err == nil {
			db.SetMaxIdleConns(conf.DB.PoolSize)
			db.SetMaxOpenConns(conf.DB.MaxConnections)
			db.SetConnMaxIdleTime(conf.DB.MaxConnIdleTime)
			db.SetConnMaxLifetime(conf.DB.MaxConnLifeTime)

			if err := db.Ping(); err == nil {
				return db, nil
			} else {
				db.Close()
				log.Warnf("database ping: %s", err)
			}

		} else {
			log.Warnf("database open: %s", err)
		}

		time.Sleep(time.Duration(i*100) * time.Millisecond)

		if i > 50 {
			return nil, err
		} else {
			i++
		}
	}
}

func initPostgres(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	c := conf
	config, _ := pgx.ParseConfig(c.DB.ConnString)
	if c.DB.Host != "" {
		config.Host = c.DB.Host
	}
	if c.DB.Port != 0 {
		config.Port = c.DB.Port
	}
	if c.DB.User != "" {
		config.User = c.DB.User
	}
	if c.DB.Password != "" {
		config.Password = c.DB.Password
	}

	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}

	if c.DB.Schema != "" {
		config.RuntimeParams["search_path"] = c.DB.Schema
	}

	if c.AppName != "" {
		config.RuntimeParams["application_name"] = c.AppName
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
			pem, err = fs.Get(c.DB.ServerCert)
		}

		if err != nil {
			return nil, fmt.Errorf("tls: %w", err)
		}

		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			return nil, errors.New("tls: failed to append pem")
		}

		config.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
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

func initMysql(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	var connString string
	c := conf

	if c.DB.ConnString == "" {
		connString = fmt.Sprintf("%s:%s@tcp(%s:%d)/", c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port)
	} else {
		connString = c.DB.ConnString
	}

	if openDB {
		connString += c.DB.DBName
	}

	return &dbConf{"mysql", connString}, nil
}

func loadX509KeyPair(fs core.FS, certFile, keyFile string) (
	cert tls.Certificate, err error,
) {
	certPEMBlock, err := fs.Get(certFile)
	if err != nil {
		return cert, err
	}
	keyPEMBlock, err := fs.Get(keyFile)
	if err != nil {
		return cert, err
	}
	return tls.X509KeyPair(certPEMBlock, keyPEMBlock)
}
