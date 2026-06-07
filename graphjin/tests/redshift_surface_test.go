package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestRedshiftDiscoveryAndQuerySmoke(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	users, err := gj.GetTableSchema("users")
	if err != nil {
		t.Fatal(err)
	}
	if users.PrimaryKey != "id" {
		t.Fatalf("users primary key = %q, want id", users.PrimaryKey)
	}

	res, err := gj.GraphQL(context.Background(), `
	query {
		users(limit: 1, order_by: { id: asc }) {
			id
			email
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"email":"ada@example.com"`)) {
		t.Fatalf("unexpected result: %s", res.Data)
	}
}

func TestRedshiftSubscriptionBatchedPollingUser(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	if _, err := db.Exec(`UPDATE users SET phone = '555-redshift-initial' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
		SubsPollDuration: 200 * time.Millisecond,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	m, err := gj.Subscribe(context.Background(), `
	subscription redshift_user {
		users(id: $id) {
			id
			email
			phone
		}
	}`, json.RawMessage(`{"id":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe()

	initial := waitRedshiftSubResult(t, m, 5*time.Second)
	if !strings.Contains(initial.SQL(), `JSON_PARSE(?)`) || !strings.Contains(initial.SQL(), `UNNEST(_gj_sub_input._gj_params) WITH OFFSET`) {
		t.Fatalf("redshift subscription did not use batched wrapper:\n%s", initial.SQL())
	}
	if !bytes.Contains(initial.Data, []byte(`"email":"ada@example.com"`)) ||
		!bytes.Contains(initial.Data, []byte(`"phone":"555-redshift-initial"`)) {
		t.Fatalf("unexpected initial subscription result: %s", initial.Data)
	}

	assertNoRedshiftSubResult(t, m, 700*time.Millisecond)

	if _, err := db.Exec(`UPDATE users SET phone = '555-redshift-updated' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	changed := waitRedshiftSubResult(t, m, 5*time.Second)
	if !bytes.Contains(changed.Data, []byte(`"phone":"555-redshift-updated"`)) {
		t.Fatalf("unexpected changed subscription result: %s", changed.Data)
	}
}

func TestRedshiftSubscriptionCursorBatchedPolling(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	if _, err := db.Exec(`DELETE FROM chats WHERE id >= 720000`); err != nil {
		t.Fatal(err)
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
		SubsPollDuration: 200 * time.Millisecond,
		SecretKey:        "not_a_real_secret",
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	m, err := gj.Subscribe(context.Background(), `
	subscription redshift_chats {
		chats(first: 1, after: $cursor, order_by: { id: asc }) {
			id
			body
		}
	}`, json.RawMessage(`{"cursor":null}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe()

	for _, want := range []string{`"id":1`, `"id":2`, `"id":3`} {
		msg := waitRedshiftSubResult(t, m, 5*time.Second)
		if !bytes.Contains(msg.Data, []byte(want)) {
			t.Fatalf("cursor subscription got %s, want %s", msg.Data, want)
		}
	}

	if _, err := db.Exec(`INSERT INTO chats (id, body, created_at) VALUES (720001, 'New redshift chat message', '2024-01-04 00:10:00')`); err != nil {
		t.Fatal(err)
	}
	msg := waitRedshiftSubResult(t, m, 5*time.Second)
	if !bytes.Contains(msg.Data, []byte(`"id":720001`)) ||
		!bytes.Contains(msg.Data, []byte(`"body":"New redshift chat message"`)) {
		t.Fatalf("unexpected inserted cursor result: %s", msg.Data)
	}
}

func TestRedshiftSubscriptionMultipleSubscribersBatched(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	if _, err := db.Exec(`UPDATE users SET phone = NULL WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
		SubsPollDuration: 200 * time.Millisecond,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	gql := `
	subscription redshift_multi {
		users(id: $id) {
			id
			email
			phone
		}
	}`
	m1, err := gj.Subscribe(context.Background(), gql, json.RawMessage(`{"id":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m1.Unsubscribe()
	m2, err := gj.Subscribe(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Unsubscribe()

	if msg := waitRedshiftSubResult(t, m1, 5*time.Second); !bytes.Contains(msg.Data, []byte(`"email":"ada@example.com"`)) {
		t.Fatalf("subscriber 1 initial result: %s", msg.Data)
	}
	if msg := waitRedshiftSubResult(t, m2, 5*time.Second); !bytes.Contains(msg.Data, []byte(`"email":"grace@example.com"`)) {
		t.Fatalf("subscriber 2 initial result: %s", msg.Data)
	}

	if _, err := db.Exec(`UPDATE users SET phone = CASE WHEN id = 1 THEN '555-redshift-one' WHEN id = 2 THEN '555-redshift-two' ELSE phone END WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}
	if msg := waitRedshiftSubResult(t, m1, 5*time.Second); !bytes.Contains(msg.Data, []byte(`"phone":"555-redshift-one"`)) {
		t.Fatalf("subscriber 1 changed result: %s", msg.Data)
	}
	if msg := waitRedshiftSubResult(t, m2, 5*time.Second); !bytes.Contains(msg.Data, []byte(`"phone":"555-redshift-two"`)) {
		t.Fatalf("subscriber 2 changed result: %s", msg.Data)
	}
}

func waitRedshiftSubResult(t *testing.T, m *core.Member, timeout time.Duration) *core.Result {
	t.Helper()
	select {
	case msg := <-m.Result:
		if len(msg.Errors) != 0 {
			t.Fatalf("subscription returned errors: %+v", msg.Errors)
		}
		return msg
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for redshift subscription result after %s", timeout)
	}
	return nil
}

func assertNoRedshiftSubResult(t *testing.T, m *core.Member, timeout time.Duration) {
	t.Helper()
	select {
	case msg := <-m.Result:
		t.Fatalf("unexpected idle subscription result: %s", msg.Data)
	case <-time.After(timeout):
	}
}

func TestRedshiftMutationInsertWithSuppliedPK(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `
	mutation {
		users(insert: { id: 710001, org_id: 1, email: "insert-redshift@example.com" }) {
			id
			email
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"id":710001`)) || !bytes.Contains(res.Data, []byte(`"email":"insert-redshift@example.com"`)) {
		t.Fatalf("unexpected insert result: %s", res.Data)
	}
}

func TestRedshiftMutationUpdateByPK(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	_, err = gj.GraphQL(context.Background(), `
	mutation {
		users(insert: { id: 710002, org_id: 1, email: "update-redshift@example.com" }) {
			id
		}
	}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := gj.GraphQL(context.Background(), `
	mutation {
		users(id: 710002, update: { phone: "555-7102" }) {
			id
			phone
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"id":710002`)) || !bytes.Contains(res.Data, []byte(`"phone":"555-7102"`)) {
		t.Fatalf("unexpected update result: %s", res.Data)
	}
}

func TestRedshiftMutationDeleteReturnsPreDeleteRow(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	_, err = gj.GraphQL(context.Background(), `
	mutation {
		users(insert: { id: 710003, org_id: 1, email: "delete-redshift@example.com", phone: "555-7103" }) {
			id
		}
	}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := gj.GraphQL(context.Background(), `
	mutation {
		users(id: 710003, delete: true) {
			id
			email
			phone
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"id":710003`)) || !bytes.Contains(res.Data, []byte(`"email":"delete-redshift@example.com"`)) {
		t.Fatalf("unexpected delete result: %s", res.Data)
	}

	check, err := gj.GraphQL(context.Background(), `
	query {
		users(id: 710003) {
			id
		}
	}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(check.Data, []byte(`710003`)) {
		t.Fatalf("deleted row still returned: %s", check.Data)
	}
}

func TestRedshiftLimitedSearch(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
		Tables: []core.Table{
			{
				Name: "users",
				Columns: []core.Column{
					{Name: "email", FullText: true},
				},
			},
		},
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `
	query($q: String!) {
		users(search: $q) {
			id
			email
		}
	}`, json.RawMessage(`{"q":"ada"}`), nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"email":"ada@example.com"`)) {
		t.Fatalf("unexpected search result: %s", res.Data)
	}
}

func TestRedshiftLimitedGIS(t *testing.T) {
	if dbType != "redshift" {
		t.Skip("redshift-only test")
	}

	gj, err := core.NewGraphJin(newConfig(&core.Config{
		DBType:           "redshift",
		DisableAllowList: true,
	}), db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `
	query {
		users(where: { shape: { st_dwithin: { point: [0, 0], distance: 1000 } } }, order_by: { id: asc }, limit: 1) {
			id
			email
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"email":"ada@example.com"`)) {
		t.Fatalf("unexpected GIS result: %s", res.Data)
	}
}
