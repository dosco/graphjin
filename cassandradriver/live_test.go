package cassandradriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gocql/gocql"
)

// liveSession connects to a Cassandra reachable at CASSANDRA_TEST_HOSTS (default
// 127.0.0.1:9042) and skips the test when none is available — matching the
// mongodriver live-test convention.
func liveSession(t *testing.T) *gocql.Session {
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
		t.Skipf("skipping live Cassandra test — no server at %s: %v", hosts, err)
	}
	return s
}

func mustExec(t *testing.T, s *gocql.Session, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if err := s.Query(q).Exec(); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
}

func setupLiveSchema(t *testing.T, s *gocql.Session) {
	t.Helper()
	mustExec(t, s,
		`CREATE KEYSPACE IF NOT EXISTS gjtest WITH replication = {'class':'SimpleStrategy','replication_factor':1}`,
		`CREATE TABLE IF NOT EXISTS gjtest.users (id text PRIMARY KEY, name text)`,
		`CREATE TABLE IF NOT EXISTS gjtest.posts (user_id text, id text, title text, PRIMARY KEY (user_id, id))`,
		`TRUNCATE gjtest.users`,
		`TRUNCATE gjtest.posts`,
		`INSERT INTO gjtest.users (id, name) VALUES ('u1','amit')`,
		`INSERT INTO gjtest.posts (user_id, id, title) VALUES ('u1','p1','first')`,
		`INSERT INTO gjtest.posts (user_id, id, title) VALUES ('u1','p2','second')`,
	)
}

func TestLive_NestedRead(t *testing.T) {
	s := liveSession(t)
	defer s.Close()
	setupLiveSchema(t, s)

	db := sql.OpenDB(NewConnector(s, "gjtest"))
	defer db.Close()

	dsl := `{"operation":"query","root":{
		"keyspace":"gjtest","table":"users","columns":["id","name"],"partition_keys":["id"],
		"field_name":"users","filters":[{"col":"id","op":"eq","param":"$1"}],
		"children":[{"keyspace":"gjtest","table":"posts","columns":["user_id","id","title"],
			"partition_keys":["user_id"],"clustering_keys":["id"],"field_name":"posts",
			"rel":{"parent_col":"id","child_col":"user_id"}}]}}`

	var raw []byte
	row := db.QueryRowContext(context.Background(), dsl, "u1")
	if err := row.Scan(&raw); err != nil {
		t.Fatalf("query: %v", err)
	}
	var res struct {
		Users []struct {
			Name  string `json:"name"`
			Posts []struct {
				Title string `json:"title"`
			} `json:"posts"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(res.Users) != 1 || res.Users[0].Name != "amit" || len(res.Users[0].Posts) != 2 {
		t.Fatalf("nested read wrong: %s", raw)
	}
}

func TestLive_InsertReadAfterWrite(t *testing.T) {
	s := liveSession(t)
	defer s.Close()
	setupLiveSchema(t, s)

	db := sql.OpenDB(NewConnector(s, "gjtest"))
	defer db.Close()

	dsl := `{"operation":"insert","mutation":{
		"type":"insert","keyspace":"gjtest","table":"users","partition_keys":["id"],
		"column_types":{"id":"text","name":"text"},
		"set":[{"col":"id","param":"$1"},{"col":"name","param":"$2"}],
		"returning":{"keyspace":"gjtest","table":"users","columns":["id","name"],
			"partition_keys":["id"],"field_name":"user","singular":true,
			"filters":[{"col":"id","op":"eq","param":"$1"}]}}}`

	var raw []byte
	row := db.QueryRowContext(context.Background(), dsl, "u2", "neha")
	if err := row.Scan(&raw); err != nil {
		t.Fatalf("insert query: %v", err)
	}
	var res struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if res.User.Name != "neha" {
		t.Fatalf("read-after-write wrong: %s", raw)
	}
}

func TestLive_Introspect(t *testing.T) {
	s := liveSession(t)
	defer s.Close()
	setupLiveSchema(t, s)

	conn := &Conn{session: s, keyspace: "gjtest"}
	ctx := context.Background()

	colRows, err := conn.introspect(ctx, &QueryDSL{Operation: OpIntrospectColumns})
	if err != nil {
		t.Fatalf("introspect columns: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range drain(t, colRows) {
		seen[r[1].(string)+"."+r[2].(string)] = true
	}
	for _, want := range []string{"users.id", "users.name", "posts.user_id", "posts.id", "posts.title"} {
		if !seen[want] {
			t.Fatalf("introspect columns missing %s; got %#v", want, seen)
		}
	}

	keyRows, err := conn.introspect(ctx, &QueryDSL{Operation: OpIntrospectKeys})
	if err != nil {
		t.Fatalf("introspect keys: %v", err)
	}
	roles := map[string]string{}
	for _, r := range drain(t, keyRows) {
		roles[r[0].(string)+"."+r[1].(string)] = r[2].(string)
	}
	if roles["posts.user_id"] != "partition_key" || roles["posts.id"] != "clustering" || roles["users.id"] != "partition_key" {
		t.Fatalf("introspect key roles wrong: %#v", roles)
	}
}

// TestLive_Paging exercises the gocql PageState/PageSize round-trip the cursor
// codec maps to GraphJin cursors.
func TestLive_Paging(t *testing.T) {
	s := liveSession(t)
	defer s.Close()
	setupLiveSchema(t, s)
	mustExec(t, s, `INSERT INTO gjtest.posts (user_id, id, title) VALUES ('u1','p3','third')`)

	e := newGocqlExecutor(s)
	ctx := context.Background()

	first, err := e.Query(ctx, Statement{
		CQL:        "SELECT user_id, id, title FROM gjtest.posts WHERE user_id = ?",
		Args:       []any{"u1"},
		PageSize:   2,
		Idempotent: true,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(first.Rows) != 2 {
		t.Fatalf("page 1 should have 2 rows, got %d", len(first.Rows))
	}
	if len(first.PageState) == 0 {
		t.Fatalf("expected a page state after page 1")
	}

	// Cursor round-trip through the codec.
	cur := EncodeCursor(first.PageState)
	ps, err := DecodeCursor(cur)
	if err != nil {
		t.Fatalf("cursor decode: %v", err)
	}

	second, err := e.Query(ctx, Statement{
		CQL:        "SELECT user_id, id, title FROM gjtest.posts WHERE user_id = ?",
		Args:       []any{"u1"},
		PageSize:   2,
		PageState:  ps,
		Idempotent: true,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(second.Rows) != 1 {
		t.Fatalf("page 2 should have the remaining 1 row, got %d", len(second.Rows))
	}
}
