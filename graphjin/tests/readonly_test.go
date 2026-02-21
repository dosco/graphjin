package tests_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestReadOnlyDB(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: true},
		},
		Tables: []core.Table{
			{Name: "users"},
		},
		Roles: []core.Role{
			{
				Name:   "user",
				Tables: []core.RoleTable{{Name: "users"}},
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)

	t.Run("insert_blocked", func(t *testing.T) {
		gql := `mutation {
			users(insert: { id: $id, email: $email, full_name: $fullName, stripe_id: $stripeID, category_counts: $categoryCounts }) {
				id
			}
		}`
		vars := json.RawMessage(`{
			"id": 9001,
			"email": "readonly@test.com",
			"fullName": "Read Only",
			"stripeID": "stripe_readonly",
			"categoryCounts": [{"category_id": 1, "count": 1}]
		}`)

		_, err := gj.GraphQL(ctx, gql, vars, nil)
		if err == nil {
			t.Fatal("expected insert to be blocked")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("expected 'blocked' error, got: %s", err)
		}
	})

	t.Run("update_blocked", func(t *testing.T) {
		gql := `mutation {
			users(update: { full_name: $fullName }, where: { id: { eq: 3 } }) {
				id
			}
		}`
		vars := json.RawMessage(`{"fullName": "Updated Name"}`)

		_, err := gj.GraphQL(ctx, gql, vars, nil)
		if err == nil {
			t.Fatal("expected update to be blocked")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("expected 'blocked' error, got: %s", err)
		}
	})

	t.Run("delete_blocked", func(t *testing.T) {
		gql := `mutation {
			users(delete: true, where: { id: { eq: 9001 } }) {
				id
			}
		}`

		_, err := gj.GraphQL(ctx, gql, nil, nil)
		if err == nil {
			t.Fatal("expected delete to be blocked")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("expected 'blocked' error, got: %s", err)
		}
	})

	t.Run("query_allowed", func(t *testing.T) {
		gql := `query {
			users(where: { id: { eq: 3 } }) {
				id
				email
			}
		}`

		res, err := gj.GraphQL(ctx, gql, nil, nil)
		if err != nil {
			t.Fatalf("expected query to succeed, got: %s", err)
		}
		if len(res.Data) == 0 {
			t.Fatal("expected non-empty query result")
		}
	})
}
