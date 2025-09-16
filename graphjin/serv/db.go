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

// Config holds the configuration for the service
func NewDB(conf *Config, openDB bool, log *zap.SugaredLogger, fs core.FS) (*sql.DB, error) {
	return newDB(conf, openDB, false, log, fs)
}

// newDB initializes the database
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
		// Check if the connection string has the required prefix or type is set to postgres
		if strings.HasPrefix(cs, "postgres://") || strings.HasPrefix(cs, "postgresql://") || conf.DB.Type == "postgres" {
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

// initPostgres initializes the postgres database
func initPostgres(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	confCopy := conf
	config, _ := pgx.ParseConfig(confCopy.DB.ConnString)

	// Check if the connection string is empty, if it, look at the other fields
	if confCopy.DB.ConnString == "" {
		if confCopy.DB.Host != "" {
			config.Host = confCopy.DB.Host
		}
		if confCopy.DB.Port != 0 {
			config.Port = confCopy.DB.Port
		}
		if confCopy.DB.User != "" {
			config.User = confCopy.DB.User
		}
		if confCopy.DB.Password != "" {
			config.Password = confCopy.DB.Password
		}
	}

	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}

	if confCopy.DB.Schema != "" {
		config.RuntimeParams["search_path"] = confCopy.DB.Schema
	}

	if confCopy.AppName != "" {
		config.RuntimeParams["application_name"] = confCopy.AppName
	}

	if openDB {
		config.Database = confCopy.DB.DBName
	}

	if confCopy.DB.EnableTLS {
		if len(confCopy.DB.ServerName) == 0 {
			return nil, errors.New("tls: server_name is required")
		}
		if len(confCopy.DB.ServerCert) == 0 {
			return nil, errors.New("tls: server_cert is required")
		}

		rootCertPool := x509.NewCertPool()
		var pem []byte
		var err error

		if strings.Contains(confCopy.DB.ServerCert, pemSig) {
			pem = []byte(strings.ReplaceAll(confCopy.DB.ServerCert, `\n`, "\n"))
		} else {
			pem, err = fs.Get(confCopy.DB.ServerCert)
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
			ServerName: confCopy.DB.ServerName,
		}

		if len(confCopy.DB.ClientCert) > 0 {
			if len(confCopy.DB.ClientKey) == 0 {
				return nil, errors.New("tls: client_key is required")
			}

			clientCert := make([]tls.Certificate, 0, 1)
			var certs tls.Certificate

			if strings.Contains(confCopy.DB.ClientCert, pemSig) {
				certs, err = tls.X509KeyPair(
					[]byte(strings.ReplaceAll(confCopy.DB.ClientCert, `\n`, "\n")),
					[]byte(strings.ReplaceAll(confCopy.DB.ClientKey, `\n`, "\n")),
				)
			} else {
				certs, err = loadX509KeyPair(fs, confCopy.DB.ClientCert, confCopy.DB.ClientKey)
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

// initMysql initializes the mysql database
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

// loadX509KeyPair loads a X509 key pair from a file system
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
