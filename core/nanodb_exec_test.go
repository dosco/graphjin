package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestNanoDBRoleFilterUsesTrustedIdentityVars(t *testing.T) {
	user1Ref := nanoDBTestIdentityRef("user_1")
	user2Ref := nanoDBTestIdentityRef("user_2")
	gj := newNanoDBIdentityFilterTestGraphJin(t, `{ or: { owner_ref: { eq: $user_ref }, visibility: { eq: "global" } } }`, user1Ref, user2Ref)
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"user_ref": user1Ref})
	res, err := gj.GraphQL(ctx, `query { docs(order_by: { title: asc }) { title } }`, nil, nil)
	assertNanoDBDocTitles(t, res, err, "global", "mine")
}

func TestNanoDBRoleFilterDoesNotTrustClientSuppliedUserRef(t *testing.T) {
	user1Ref := nanoDBTestIdentityRef("user_1")
	user2Ref := nanoDBTestIdentityRef("user_2")
	gj := newNanoDBIdentityFilterTestGraphJin(t, `{ or: { owner_ref: { eq: $user_ref }, visibility: { eq: "global" } } }`, user1Ref, user2Ref)
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserRoleKey, "user")
	vars := json.RawMessage(`{"user_ref":"` + user2Ref + `"}`)
	res, err := gj.GraphQL(ctx, `query { docs(order_by: { title: asc }) { title } }`, vars, nil)
	assertNanoDBDocTitles(t, res, err, "global")
}

func TestNanoDBRoleFilterTrustedUserRefBeatsClientOverride(t *testing.T) {
	user1Ref := nanoDBTestIdentityRef("user_1")
	user2Ref := nanoDBTestIdentityRef("user_2")
	gj := newNanoDBIdentityFilterTestGraphJin(t, `{ or: { owner_ref: { eq: $user_ref }, visibility: { eq: "global" } } }`, user1Ref, user2Ref)
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	ctx = context.WithValue(ctx, UserRoleKey, "user")
	ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"user_ref": user1Ref})
	vars := json.RawMessage(`{"user_ref":"` + user2Ref + `"}`)
	res, err := gj.GraphQL(ctx, `query { docs(order_by: { title: asc }) { title } }`, vars, nil)
	assertNanoDBDocTitles(t, res, err, "global", "mine")
}

func TestNanoDBRoleFilterInOperatorDoesNotTrustClientSuppliedUserRef(t *testing.T) {
	user1Ref := nanoDBTestIdentityRef("user_1")
	user2Ref := nanoDBTestIdentityRef("user_2")
	gj := newNanoDBIdentityFilterTestGraphJin(t, `{ or: { owner_ref: { in: $user_ref }, visibility: { eq: "global" } } }`, user1Ref, user2Ref)
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	ctx = context.WithValue(ctx, UserRoleKey, "user")
	ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"user_ref": user1Ref})
	vars := json.RawMessage(`{"user_ref":["` + user2Ref + `"]}`)
	res, err := gj.GraphQL(ctx, `query { docs(order_by: { title: asc }) { title } }`, vars, nil)
	assertNanoDBDocTitles(t, res, err, "global", "mine")
}

func newNanoDBIdentityFilterTestGraphJin(t *testing.T, filter string, user1Ref, user2Ref string) *GraphJin {
	t.Helper()
	db, err := NewNanoDB(NanoSnapshot{Schema: "main", Tables: []NanoTable{{
		Name: "docs",
		Columns: []NanoColumn{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "title", Type: "text"},
			{Name: "visibility", Type: "text", Index: true},
			{Name: "owner_ref", Type: "text", Index: true},
		},
		Rows: []NanoRow{
			{"id": "global", "title": "global", "visibility": "global", "owner_ref": ""},
			{"id": "mine", "title": "mine", "visibility": "user", "owner_ref": user1Ref},
			{"id": "theirs", "title": "theirs", "visibility": "user", "owner_ref": user2Ref},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
		Roles: []Role{{
			Name: "user",
			Tables: []RoleTable{{
				Name:     "docs",
				Database: DefaultDBName,
				Query: &Query{
					Filters: []string{filter},
					Columns: []string{"id", "title", "visibility", "owner_ref"},
				},
			}},
		}},
	}
	gj, err := NewGraphJin(conf, nil, OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}))
	if err != nil {
		t.Fatal(err)
	}
	return gj
}

func assertNanoDBDocTitles(t *testing.T, res *Result, err error, want ...string) {
	t.Helper()
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("GraphQL: err=%v errors=%+v", err, res.Errors)
	}
	var out struct {
		Docs []struct {
			Title string `json:"title"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode response: %v\n%s", err, res.Data)
	}
	if len(out.Docs) != len(want) {
		t.Fatalf("got %d docs, want %d: %s", len(out.Docs), len(want), res.Data)
	}
	for i, title := range want {
		if out.Docs[i].Title != title {
			t.Fatalf("doc[%d] = %q, want %q: %s", i, out.Docs[i].Title, title, res.Data)
		}
	}
}

func nanoDBTestIdentityRef(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}
