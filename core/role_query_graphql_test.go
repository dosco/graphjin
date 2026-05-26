package core_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	_ "github.com/mattn/go-sqlite3"
)

func TestGraphQLRoleQueryMatchesConfiguredRole(t *testing.T) {
	db := newGraphQLRoleTestDB(t)
	defer db.Close() //nolint:errcheck

	conf := graphQLRoleTestConfig()
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err := gj.GraphQL(ctx, `{ users(id: 1) { id email } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Role(); got != "admin" {
		t.Fatalf("role = %q, want admin", got)
	}
}

func TestGraphQLRoleQueryDefaultsToUser(t *testing.T) {
	db := newGraphQLRoleTestDB(t)
	defer db.Close() //nolint:errcheck

	conf := graphQLRoleTestConfig()
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	tests := []struct {
		name   string
		userID int
	}{
		{name: "row does not match", userID: 20},
		{name: "returned role name is not special", userID: 30},
		{name: "no row", userID: 999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), core.UserIDKey, tt.userID)
			res, err := gj.GraphQL(ctx, `{ users(id: 20) { id email } }`, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Role(); got != "user" {
				t.Fatalf("role = %q, want user", got)
			}
		})
	}
}

func TestGraphQLRoleQueryMultipleRowsError(t *testing.T) {
	db := newGraphQLRoleTestDB(t)
	defer db.Close() //nolint:errcheck

	conf := &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		RolesQuery: `query {
			users(where: { id: { gt: $user_id } }) {
				userid: id
			}
		}`,
		Roles: []core.Role{
			{Name: "admin", Match: "userid = 1"},
		},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx := context.WithValue(context.Background(), core.UserIDKey, 0)
	_, err = gj.GraphQL(ctx, `{ users(id: 1) { id } }`, nil, nil)
	if err == nil {
		t.Fatal("GraphQL() expected multiple rows error")
	}
	if !strings.Contains(err.Error(), "roles_query returned multiple rows") {
		t.Fatalf("GraphQL() error = %v, want multiple rows roles_query error", err)
	}
}

func TestGraphQLRoleQueryMalformedMatchErrorsAtInit(t *testing.T) {
	db := newGraphQLRoleTestDB(t)
	defer db.Close() //nolint:errcheck

	conf := &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		RolesQuery:       `query { users(id: $user_id) { userid: id } }`,
		Roles: []core.Role{
			{Name: "admin", Match: "userid <"},
		},
	}

	_, err := core.NewGraphJin(conf, db)
	if err == nil {
		t.Fatal("NewGraphJin() expected malformed match error")
	}
	if !strings.Contains(err.Error(), `role "admin" match`) {
		t.Fatalf("NewGraphJin() error = %v, want role match error", err)
	}
}

func TestSubscriptionGraphQLRoleQueryWithNilRequestConfig(t *testing.T) {
	db := newGraphQLRoleTestDB(t)
	defer db.Close() //nolint:errcheck

	conf := graphQLRoleTestConfig()
	conf.SubsPollDuration = 10 * time.Millisecond

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	m, err := gj.Subscribe(ctx, `subscription { users(id: 1) { id email } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe()
}

func graphQLRoleTestConfig() *core.Config {
	return &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		SecretKey:        "not_a_real_secret",
		RolesQuery: `query {
			users(id: $user_id) {
				role
				userid: id
			}
		}`,
		Roles: []core.Role{
			{Name: "admin", Match: "role = 'the_admin_dude' or userid < 10"},
		},
	}
}

func newGraphQLRoleTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", t.TempDir()+"/roles_graphql.sqlite3")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT,
			role TEXT
		);
		INSERT INTO users (id, email, role) VALUES
			(1, 'admin@test.com', 'the_admin_dude'),
			(20, 'member@test.com', 'member'),
			(30, 'role-name-only@test.com', 'admin');
	`)
	if err != nil {
		db.Close() //nolint:errcheck
		t.Fatal(err)
	}
	return db
}
