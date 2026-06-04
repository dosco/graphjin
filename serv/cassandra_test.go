package serv

import (
	"reflect"
	"testing"

	"github.com/gocql/gocql"
)

func TestNewCassandraCluster_Password(t *testing.T) {
	db := &Database{Host: "127.0.0.1", DBName: "gjtest", Port: 9042, User: "cassandra", Password: "secret"}
	cluster, ks, err := newCassandraCluster(db, nil)
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if ks != "gjtest" {
		t.Fatalf("keyspace: %q", ks)
	}
	if cluster.Port != 9042 {
		t.Fatalf("port: %d", cluster.Port)
	}
	if cluster.SslOpts != nil {
		t.Fatalf("without enable_tls there should be no TLS")
	}
	if cluster.Consistency != gocql.LocalQuorum {
		t.Fatalf("default consistency should be LOCAL_QUORUM, got %v", cluster.Consistency)
	}
	pa, ok := cluster.Authenticator.(gocql.PasswordAuthenticator)
	if !ok || pa.Username != "cassandra" {
		t.Fatalf("expected password authenticator, got %#v", cluster.Authenticator)
	}
}

func TestNewCassandraCluster_TLS(t *testing.T) {
	// Keyspaces: TLS on 9142 with system-trusted Amazon CA + service credentials.
	db := &Database{
		Host:       "cassandra.us-east-1.amazonaws.com",
		Port:       9142,
		DBName:     "app",
		EnableTLS:  true,
		ServerName: "cassandra.us-east-1.amazonaws.com",
		User:       "keyspaces-user",
		Password:   "keyspaces-pass",
	}
	cluster, _, err := newCassandraCluster(db, nil)
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if cluster.Port != 9142 {
		t.Fatalf("port: %d", cluster.Port)
	}
	if cluster.SslOpts == nil {
		t.Fatalf("enable_tls should configure TLS")
	}
	if !cluster.SslOpts.EnableHostVerification {
		t.Fatalf("host verification should be on when server_name is set")
	}
	if _, ok := cluster.Authenticator.(gocql.PasswordAuthenticator); !ok {
		t.Fatalf("expected password authenticator, got %T", cluster.Authenticator)
	}
}

func TestNewCassandraCluster_NoAuthWithoutUser(t *testing.T) {
	db := &Database{Host: "127.0.0.1", DBName: "gjtest"}
	cluster, _, err := newCassandraCluster(db, nil)
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if cluster.Authenticator != nil {
		t.Fatalf("no user → no authenticator, got %#v", cluster.Authenticator)
	}
}

func TestNewCassandraCluster_RequiresKeyspaceAndHost(t *testing.T) {
	if _, _, err := newCassandraCluster(&Database{Host: "h"}, nil); err == nil {
		t.Fatal("missing keyspace should error")
	}
	if _, _, err := newCassandraCluster(&Database{DBName: "app"}, nil); err == nil {
		t.Fatal("missing host should error")
	}
}

func TestCassandraHosts(t *testing.T) {
	cases := []struct {
		db   Database
		want []string
	}{
		{Database{ConnString: "cassandra://h1,h2/app"}, []string{"h1", "h2"}},
		{Database{ConnString: "cassandra://h1"}, []string{"h1"}},
		{Database{Host: "127.0.0.1"}, []string{"127.0.0.1"}},
	}
	for _, tc := range cases {
		got := cassandraHosts(&tc.db)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("cassandraHosts(%+v) = %v, want %v", tc.db, got, tc.want)
		}
	}
}

func TestCassandraConsistency(t *testing.T) {
	if cassandraConsistency("") != gocql.LocalQuorum {
		t.Fatal("empty should default to LOCAL_QUORUM")
	}
	if cassandraConsistency("one") != gocql.One {
		t.Fatal("one")
	}
	if cassandraConsistency("garbage") != gocql.LocalQuorum {
		t.Fatal("unknown should default to LOCAL_QUORUM")
	}
}

func TestDetectCassandraURL(t *testing.T) {
	conf := &Config{}
	conf.DB.ConnString = "cassandra://127.0.0.1/app"
	detectDBType(conf)
	if conf.DBType != "cassandra" {
		t.Fatalf("expected cassandra dbtype, got %q", conf.DBType)
	}
}
