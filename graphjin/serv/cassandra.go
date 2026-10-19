package serv

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/cassandradriver"
	"github.com/dosco/graphjin/core/v3"
	"github.com/gocql/gocql"
)

// initCassandra builds a tuned gocql session and wraps it in the cassandradriver
// connector — the same connector path MongoDB uses (no driverForType entry).
// Connection auth is standard username/password (service-specific credentials for
// Amazon Keyspaces); request authorization is handled by GraphJin's source-mode
// access controls, not the driver.
func initCassandra(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	cluster, keyspace, err := newCassandraCluster(&conf.DB, fs)
	if err != nil {
		return nil, err
	}
	if !openDB {
		// Health/dry-run path: nothing to connect yet.
		return &dbConf{driverName: "cassandra"}, nil
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandra connect: %w", err)
	}
	return &dbConf{driverName: "cassandra", connector: cassandradriver.NewConnector(session, keyspace)}, nil
}

// newCassandraCluster assembles a production-tuned gocql.ClusterConfig. Split out
// so the TLS / auth / tuning path is unit-testable without a live cluster.
func newCassandraCluster(db *Database, fs core.FS) (*gocql.ClusterConfig, string, error) {
	hosts := cassandraHosts(db)
	if len(hosts) == 0 {
		return nil, "", fmt.Errorf("cassandra requires a host or connection string")
	}
	keyspace := db.DBName
	if keyspace == "" {
		return nil, "", fmt.Errorf("cassandra requires a keyspace (database name)")
	}

	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.ProtoVersion = 4
	cluster.Consistency = cassandraConsistency(db.Consistency)
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 15 * time.Second
	cluster.ReconnectInterval = time.Second
	cluster.RetryPolicy = &gocql.SimpleRetryPolicy{NumRetries: 3}
	if db.PoolSize > 0 {
		cluster.NumConns = db.PoolSize
	}
	if db.Port != 0 {
		cluster.Port = int(db.Port)
	}

	// TLS (Amazon Keyspaces requires it on port 9142 with the Amazon/Starfield CA).
	if db.EnableTLS {
		tlsCfg, err := cassandraTLS(db, fs)
		if err != nil {
			return nil, "", err
		}
		cluster.SslOpts = &gocql.SslOptions{
			Config:                 tlsCfg,
			EnableHostVerification: db.ServerName != "",
		}
	}

	// Standard username/password auth (service-specific credentials for Keyspaces).
	if db.User != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: db.User, Password: db.Password}
	}

	return cluster, keyspace, nil
}

// cassandraHosts extracts contact points from the connection string
// (cassandra://h1,h2[:port]/keyspace) or the host field.
func cassandraHosts(db *Database) []string {
	if cs := db.ConnString; cs != "" {
		cs = strings.TrimPrefix(cs, "cassandra://")
		if i := strings.IndexByte(cs, '/'); i >= 0 {
			cs = cs[:i]
		}
		if cs == "" {
			return nil
		}
		return strings.Split(cs, ",")
	}
	if db.Host != "" {
		return []string{db.Host}
	}
	return nil
}

func cassandraConsistency(s string) gocql.Consistency {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "LOCAL_QUORUM":
		return gocql.LocalQuorum
	case "QUORUM":
		return gocql.Quorum
	case "LOCAL_ONE":
		return gocql.LocalOne
	case "ONE":
		return gocql.One
	case "TWO":
		return gocql.Two
	case "THREE":
		return gocql.Three
	case "ALL":
		return gocql.All
	case "EACH_QUORUM":
		return gocql.EachQuorum
	case "ANY":
		return gocql.Any
	default:
		return gocql.LocalQuorum
	}
}

func cassandraTLS(db *Database, fs core.FS) (*tls.Config, error) {
	// Start from system roots — the Amazon/Starfield CA that Keyspaces presents is
	// already trusted there — and append a custom server_cert if provided.
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
				return nil, fmt.Errorf("cassandra tls: %w", err)
			}
		}
		if len(pem) > 0 && !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("cassandra tls: failed to append server_cert")
		}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: db.ServerName,
	}, nil
}
