package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/cassandradriver"
	core "github.com/dosco/graphjin/core/v3"
	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cassandraKeyspace = "gjtest"

// startCassandraDB boots a cassandra:5 container, seeds a partition-key-first
// schema, and returns a *sql.DB backed by the cassandradriver connector — the
// same dbFunc connector path MongoDB uses in this harness.
func startCassandraDB(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "cassandra:5",
			ExposedPorts: []string{"9042/tcp"},
			Env: map[string]string{
				"CASSANDRA_DC":              "dc1",
				"CASSANDRA_ENDPOINT_SNITCH": "GossipingPropertyFileSnitch",
				"MAX_HEAP_SIZE":             "512M",
				"HEAP_NEWSIZE":              "128M",
			},
			WaitingFor: wait.ForListeningPort("9042/tcp").WithStartupTimeout(300 * time.Second),
		},
		Started: true,
	}
	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("cassandra container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	port, err := container.MappedPort(ctx, "9042")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}

	cluster := gocql.NewCluster(host)
	cluster.Port = port.Int()
	cluster.ProtoVersion = 4
	cluster.Consistency = gocql.Quorum
	cluster.ConnectTimeout = 20 * time.Second
	cluster.Timeout = 20 * time.Second

	// The CQL port can listen before the node is query-ready; retry the session.
	var session *gocql.Session
	for i := 0; i < 90; i++ {
		if session, err = cluster.CreateSession(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("cassandra session: %w", err)
	}

	if err := seedCassandra(session); err != nil {
		session.Close()
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("cassandra seed: %w", err)
	}

	sqlDB := sql.OpenDB(cassandradriver.NewConnector(session, cassandraKeyspace))
	cleanup := func(ctx context.Context) error {
		_ = sqlDB.Close()
		session.Close()
		return container.Terminate(ctx)
	}
	return cleanup, sqlDB, nil
}

func seedCassandra(s *gocql.Session) error {
	ddl := []string{
		`CREATE KEYSPACE IF NOT EXISTS gjtest WITH replication = {'class':'SimpleStrategy','replication_factor':1}`,
		`CREATE TABLE IF NOT EXISTS gjtest.users (id text PRIMARY KEY, name text, email text)`,
		`CREATE TABLE IF NOT EXISTS gjtest.posts (user_id text, id text, title text, PRIMARY KEY ((user_id), id))`,
		`CREATE TABLE IF NOT EXISTS gjtest.profiles (user_id text PRIMARY KEY, bio text)`,
		`CREATE TABLE IF NOT EXISTS gjtest.post_stats (post_id text PRIMARY KEY, views counter)`,
	}
	for _, q := range ddl {
		if err := s.Query(q).Exec(); err != nil {
			return err
		}
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("u%d", i)
		if err := s.Query(`INSERT INTO gjtest.users (id, name, email) VALUES (?, ?, ?)`,
			id, fmt.Sprintf("User %d", i), fmt.Sprintf("user%d@test.com", i)).Exec(); err != nil {
			return err
		}
		if err := s.Query(`INSERT INTO gjtest.profiles (user_id, bio) VALUES (?, ?)`,
			id, fmt.Sprintf("bio for %s", id)).Exec(); err != nil {
			return err
		}
		for j := 1; j <= 3; j++ {
			if err := s.Query(`INSERT INTO gjtest.posts (user_id, id, title) VALUES (?, ?, ?)`,
				id, fmt.Sprintf("p%d", j), fmt.Sprintf("Post %d of %s", j, id)).Exec(); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- helpers ---

func cassandraGJ(t *testing.T) *core.GraphJin {
	t.Helper()
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultBlock:     false,
	})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatalf("NewGraphJin: %v", err)
	}
	return gj
}

func cassandraQuery(t *testing.T, gj *core.GraphJin, gql string, vars string) (json.RawMessage, error) {
	t.Helper()
	var v json.RawMessage
	if vars != "" {
		v = json.RawMessage(vars)
	}
	res, err := gj.GraphQL(context.Background(), gql, v, nil)
	if res != nil {
		return res.Data, err
	}
	return nil, err
}

func requireContains(t *testing.T, data json.RawMessage, subs ...string) {
	t.Helper()
	s := string(data)
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("result %s missing %q", s, sub)
		}
	}
}

// --- integration tests (run via: -db=cassandra -run Cassandra) ---

func TestCassandraSinglePartitionRead(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	data, err := cassandraQuery(t, gj, `query { users(where: { id: { eq: "u1" } }) { id name email } }`, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	requireContains(t, data, `"name":"User 1"`, `"email":"user1@test.com"`)
}

func TestCassandraNestedOneToMany(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	data, err := cassandraQuery(t, gj,
		`query { users(where: { id: { eq: "u1" } }) { id posts(order_by: { id: asc }) { id title } } }`, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	requireContains(t, data, `"Post 1 of u1"`, `"Post 2 of u1"`, `"Post 3 of u1"`)
}

func TestCassandraNestedOneToOne(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	data, err := cassandraQuery(t, gj,
		`query { users(where: { id: { eq: "u2" } }) { id name profiles { bio } } }`, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	requireContains(t, data, `"bio for u2"`)
}

func TestCassandraInsertReadAfterWrite(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	data, err := cassandraQuery(t, gj,
		`mutation ($data: users_insert_input!) { users(insert: $data) { id name } }`,
		`{"data":{"id":"u100","name":"Inserted","email":"u100@test.com"}}`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	requireContains(t, data, `"Inserted"`)

	got, err := cassandraQuery(t, gj, `query { users(where: { id: { eq: "u100" } }) { name } }`, "")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	requireContains(t, got, `"Inserted"`)
}

func TestCassandraUpdateByPK(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	_, err := cassandraQuery(t, gj,
		`mutation ($data: users_update_input!) { users(update: $data, where: { id: { eq: "u3" } }) { id name } }`,
		`{"data":{"name":"Renamed"}}`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := cassandraQuery(t, gj, `query { users(where: { id: { eq: "u3" } }) { name } }`, "")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	requireContains(t, got, `"Renamed"`)
}

func TestCassandraServabilityRejection(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()

	// != is not expressible in CQL WHERE — must be a compile error.
	if _, err := cassandraQuery(t, gj,
		`query { users(where: { id: { neq: "u1" } }) { id } }`, ""); err == nil {
		t.Fatal("expected rejection for neq on Cassandra")
	}

	// Filtering on a non-key column without allow_filtering is a full scan.
	if _, err := cassandraQuery(t, gj,
		`query { users(where: { email: { eq: "user1@test.com" } }) { id } }`, ""); err == nil {
		t.Fatal("expected rejection for non-key filter on Cassandra")
	}
}

func TestCassandraPaging(t *testing.T) {
	if dbType != "cassandra" {
		t.Skip("cassandra-only integration test")
	}
	gj := cassandraGJ(t)
	defer gj.Close()
	data, err := cassandraQuery(t, gj,
		`query { posts(where: { user_id: { eq: "u1" } }, first: 2, order_by: { id: asc }) { id } }`, "")
	if err != nil {
		t.Fatalf("paged query: %v", err)
	}
	var res struct {
		Posts []struct {
			ID string `json:"id"`
		} `json:"posts"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	if len(res.Posts) != 2 {
		t.Fatalf("first:2 should return 2 posts, got %d: %s", len(res.Posts), data)
	}
}
