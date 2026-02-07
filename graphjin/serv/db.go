package serv

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/mongodriver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
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
	connector  driver.Connector // for drivers that require sql.OpenDB (e.g. MongoDB)
}

// Config holds the configuration for the service
func NewDB(conf *Config, openDB bool, log *zap.SugaredLogger, fs core.FS) (*sql.DB, error) {
	return newDB(conf, openDB, false, log, fs)
}

// detectDBType detects the database type from the connection string and updates conf.DBType
func detectDBType(conf *Config) {
	if cs := conf.DB.ConnString; cs != "" {
		if strings.HasPrefix(cs, "postgres://") || strings.HasPrefix(cs, "postgresql://") || conf.DB.Type == "postgres" {
			conf.DBType = "postgres"
		}
		if strings.HasPrefix(cs, "mysql://") {
			conf.DBType = "mysql"
			conf.DB.ConnString = strings.TrimPrefix(cs, "mysql://")
		}
		if strings.HasPrefix(cs, "sqlserver://") {
			conf.DBType = "mssql"
		}
		if strings.HasPrefix(cs, "oracle://") {
			conf.DBType = "oracle"
		}
		if strings.HasPrefix(cs, "mongodb://") || strings.HasPrefix(cs, "mongodb+srv://") {
			conf.DBType = "mongodb"
		}
	}
}

// initDBDriver initializes the database driver config based on the DB type
func initDBDriver(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	detectDBType(conf)

	var dc *dbConf
	var err error

	switch conf.DBType {
	case "", "postgres":
		dc, err = initPostgres(conf, openDB, useTelemetry, fs)
	case "mysql", "mariadb":
		dc, err = initMysql(conf, openDB, useTelemetry, fs)
	case "mssql":
		dc, err = initMssql(conf, openDB, useTelemetry, fs)
	case "sqlite":
		dc, err = initSqlite(conf, openDB, useTelemetry, fs)
	case "oracle":
		dc, err = initOracle(conf, openDB, useTelemetry, fs)
	case "mongodb":
		dc, err = initMongo(conf, openDB, useTelemetry, fs)
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, mssql, sqlite, oracle, mongodb", conf.DBType)
	}

	if err != nil {
		return nil, fmt.Errorf("database init: %v", err)
	}
	return dc, nil
}

// newDB initializes the database with a retry loop
func newDB(
	conf *Config,
	openDB, useTelemetry bool,
	log *zap.SugaredLogger,
	fs core.FS,
) (*sql.DB, error) {
	var db *sql.DB
	var err error

	dc, err := initDBDriver(conf, openDB, useTelemetry, fs)
	if err != nil {
		return nil, err
	}

	for i := 0; ; {
		if dc.connector != nil {
			db = sql.OpenDB(dc.connector)
			err = nil
		} else {
			db, err = sql.Open(dc.driverName, dc.connString)
		}
		if err == nil {
			db.SetMaxIdleConns(conf.DB.PoolSize)
			db.SetMaxOpenConns(conf.DB.MaxConnections)
			db.SetConnMaxIdleTime(conf.DB.MaxConnIdleTime)
			db.SetConnMaxLifetime(conf.DB.MaxConnLifeTime)

			if err := db.Ping(); err == nil {
				return db, nil
			} else {
				db.Close() //nolint:errcheck
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

// newDBOnce attempts a single database connection without retries
func newDBOnce(
	conf *Config,
	openDB, useTelemetry bool,
	log *zap.SugaredLogger,
	fs core.FS,
) (*sql.DB, error) {
	dc, err := initDBDriver(conf, openDB, useTelemetry, fs)
	if err != nil {
		return nil, err
	}

	var db *sql.DB
	if dc.connector != nil {
		db = sql.OpenDB(dc.connector)
	} else {
		db, err = sql.Open(dc.driverName, dc.connString)
		if err != nil {
			return nil, fmt.Errorf("database open: %w", err)
		}
	}

	db.SetMaxIdleConns(conf.DB.PoolSize)
	db.SetMaxOpenConns(conf.DB.MaxConnections)
	db.SetConnMaxIdleTime(conf.DB.MaxConnIdleTime)
	db.SetConnMaxLifetime(conf.DB.MaxConnLifeTime)

	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("database ping: %w", err)
	}

	return db, nil
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

	return &dbConf{driverName: "pgx", connString: stdlib.RegisterConnConfig(config)}, nil
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

	return &dbConf{driverName: "mysql", connString: connString}, nil
}

// initMssql initializes the mssql database
func initMssql(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	var connString string
	c := conf

	if c.DB.ConnString == "" {
		port := c.DB.Port
		if port == 0 || (c.DB.Type != "postgres" && port == 5432) {
			port = 1433
		}
		connString = fmt.Sprintf("sqlserver://%s:%s@%s:%d",
			c.DB.User, c.DB.Password, c.DB.Host, port)
	} else {
		connString = c.DB.ConnString
	}

	if openDB && c.DB.DBName != "" {
		if strings.Contains(connString, "?") {
			connString += "&database=" + c.DB.DBName
		} else {
			connString += "?database=" + c.DB.DBName
		}
	}

	return &dbConf{driverName: "sqlserver", connString: connString}, nil
}

// initSqlite initializes the sqlite database
func initSqlite(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	connString := conf.DB.ConnString
	if connString == "" {
		connString = conf.DB.Path
	}
	if connString == "" {
		return nil, fmt.Errorf("sqlite requires a connection string or path")
	}

	return &dbConf{driverName: "sqlite", connString: connString}, nil
}

// initOracle initializes the oracle database
func initOracle(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	var connString string
	c := conf

	if c.DB.ConnString == "" {
		port := c.DB.Port
		if port == 0 || (c.DB.Type != "postgres" && port == 5432) {
			port = 1521
		}
		connString = fmt.Sprintf("oracle://%s:%s@%s:%d",
			c.DB.User, c.DB.Password, c.DB.Host, port)
	} else {
		connString = c.DB.ConnString
	}

	if openDB && c.DB.DBName != "" {
		connString += "/" + c.DB.DBName
	}

	return &dbConf{driverName: "oracle", connString: connString}, nil
}

// initMongo initializes the mongodb database using the mongodriver connector
func initMongo(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	connString := conf.DB.ConnString
	if connString == "" {
		if conf.DB.Host == "" {
			return nil, fmt.Errorf("mongodb requires a connection string or host")
		}
		port := conf.DB.Port
		if port == 0 || (conf.DB.Type != "postgres" && port == 5432) {
			port = 27017
		}
		connString = fmt.Sprintf("mongodb://%s:%d", conf.DB.Host, port)
		if conf.DB.User != "" {
			connString = fmt.Sprintf("mongodb://%s:%s@%s:%d",
				conf.DB.User, conf.DB.Password, conf.DB.Host, port)
		}
	}

	dbName := conf.DB.DBName
	if dbName == "" {
		dbName = "graphjin"
	}

	client, err := mongo.Connect(options.Client().ApplyURI(connString))
	if err != nil {
		return nil, fmt.Errorf("mongodb connect: %w", err)
	}

	connector := mongodriver.NewConnector(client, dbName)
	return &dbConf{driverName: "mongodb", connector: connector}, nil
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
