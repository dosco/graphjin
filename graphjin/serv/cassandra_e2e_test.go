package serv

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/cassandradriver"
	"github.com/dosco/graphjin/core/v3"
	"github.com/gocql/gocql"
)

func cassandraLiveSession(t *testing.T) *gocql.Session {
	t.Helper()
	hosts := os.Getenv("CASSANDRA_TEST_HOSTS")
	if hosts == "" {
		hosts = "127.0.0.1:9042"
	}
	cluster := gocql.NewCluster(strings.Split(hosts, ",")...)
	cluster.ProtoVersion = 4
	cluster.Consistency = gocql.Quorum
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 10 * time.Second
	s, err := cluster.CreateSession()
	if err != nil {
		t.Skipf("skipping Cassandra e2e — no server at %s: %v", hosts, err)
	}
	return s
}

// TestCassandraEndToEnd drives the whole stack: core.NewGraphJin introspects the
// keyspace through the driver, compiles a nested GraphQL query via the Cassandra
// dialect, and the driver executes it (root SELECT + N+1 child fetch) against a
// live Cassandra.
func TestCassandraEndToEnd(t *testing.T) {
	s := cassandraLiveSession(t)
	defer s.Close()

	for _, q := range []string{
		`CREATE KEYSPACE IF NOT EXISTS gje2e WITH replication = {'class':'SimpleStrategy','replication_factor':1}`,
		`CREATE TABLE IF NOT EXISTS gje2e.users (id text PRIMARY KEY, name text)`,
		`CREATE TABLE IF NOT EXISTS gje2e.posts (user_id text, id text, title text, PRIMARY KEY (user_id, id))`,
		`TRUNCATE gje2e.users`,
		`TRUNCATE gje2e.posts`,
		`INSERT INTO gje2e.users (id, name) VALUES ('u1','amit')`,
		`INSERT INTO gje2e.posts (user_id, id, title) VALUES ('u1','p1','first')`,
		`INSERT INTO gje2e.posts (user_id, id, title) VALUES ('u1','p2','second')`,
	} {
		if err := s.Query(q).Exec(); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}

	db := sql.OpenDB(cassandradriver.NewConnector(s, "gje2e"))
	defer db.Close()

	conf := &core.Config{
		DBType:           "cassandra",
		DisableAllowList: true,
		DefaultBlock:     false,
		Tables: []core.Table{
			{Name: "posts", Columns: []core.Column{{Name: "user_id", ForeignKey: "users.id"}}},
		},
	}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatalf("NewGraphJin: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := gj.GraphQL(ctx,
		`query getUser($id: String!) { users(where: { id: { eq: $id } }) { id name posts { title } } }`,
		[]byte(`{"id":"u1"}`), nil)
	if err != nil {
		t.Fatalf("GraphQL: %v (errors=%v)", err, res)
	}
	data := string(res.Data)
	if !strings.Contains(data, "amit") || !strings.Contains(data, "first") || !strings.Contains(data, "second") {
		t.Fatalf("unexpected e2e result: %s", data)
	}

	// Insert + read-after-write through the full stack (CompileFullMutation →
	// driver INSERT → follow-up SELECT-by-PK).
	mres, err := gj.GraphQL(ctx,
		`mutation addUser($data: users_insert_input!) { users(insert: $data) { id name } }`,
		[]byte(`{"data":{"id":"u2","name":"neha"}}`), nil)
	if err != nil {
		t.Fatalf("insert mutation: %v (errors=%v)", err, mres)
	}
	if !strings.Contains(string(mres.Data), "neha") {
		t.Fatalf("read-after-write should return the inserted row: %s", mres.Data)
	}

	// Confirm it persisted.
	var name string
	if err := s.Query(`SELECT name FROM gje2e.users WHERE id = 'u2'`).Scan(&name); err != nil || name != "neha" {
		t.Fatalf("insert did not persist: name=%q err=%v", name, err)
	}
}
