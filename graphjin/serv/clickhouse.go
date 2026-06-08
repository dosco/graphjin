package serv

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/dosco/graphjin/clickhousedriver"
	"github.com/dosco/graphjin/core/v3"
)

// initClickhouse builds a clickhouse-go *sql.DB and wraps it in the
// clickhousedriver connector (the same connector path MongoDB/Cassandra use).
// ClickHouse has no FKs/transactions; GraphJin's source-mode access controls
// handle request authorization, not the driver.
func initClickhouse(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	opts, err := newClickhouseOptions(&conf.DB, fs)
	if err != nil {
		return nil, err
	}
	if !openDB {
		return &dbConf{driverName: "clickhouse-gj"}, nil
	}
	inner := clickhouse.OpenDB(opts)
	return &dbConf{
		driverName: "clickhouse-gj",
		connector:  clickhousedriver.NewConnector(inner, opts.Auth.Database),
	}, nil
}

// newClickhouseOptions builds clickhouse.Options from a clickhouse:// DSN or the
// discrete host/port/db/user/password fields.
func newClickhouseOptions(db *Database, fs core.FS) (*clickhouse.Options, error) {
	var opts *clickhouse.Options
	if cs := db.ConnString; cs != "" {
		o, err := clickhouse.ParseDSN(cs)
		if err != nil {
			return nil, fmt.Errorf("clickhouse: %w", err)
		}
		opts = o
	} else {
		if db.Host == "" {
			return nil, fmt.Errorf("clickhouse requires a host or connection string")
		}
		port := db.Port
		if port == 0 {
			port = 9000
		}
		opts = &clickhouse.Options{
			Addr: []string{fmt.Sprintf("%s:%d", db.Host, port)},
			Auth: clickhouse.Auth{Database: db.DBName, Username: db.User, Password: db.Password},
		}
	}
	if opts.Auth.Database == "" {
		opts.Auth.Database = db.DBName
	}
	if opts.Auth.Database == "" {
		return nil, fmt.Errorf("clickhouse requires a database name")
	}
	if db.EnableTLS && opts.TLS == nil {
		tlsCfg, err := clickhouseTLS(db, fs)
		if err != nil {
			return nil, err
		}
		opts.TLS = tlsCfg
	}
	return opts, nil
}

func clickhouseTLS(db *Database, fs core.FS) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if db.ServerCert != "" {
		var pem []byte
		if strings.Contains(db.ServerCert, pemSig) {
			pem = []byte(strings.ReplaceAll(db.ServerCert, `\n`, "\n"))
		} else if fs != nil {
			pem, err = fs.Get(db.ServerCert)
			if err != nil {
				return nil, fmt.Errorf("clickhouse tls: %w", err)
			}
		}
		if len(pem) > 0 && !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("clickhouse tls: failed to append server_cert")
		}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: db.ServerName,
	}, nil
}
