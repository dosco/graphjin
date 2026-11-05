package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func threadGraphQL(t *testing.T, svc *graphjinService, ctx context.Context, query string) (map[string]json.RawMessage, string) {
	t.Helper()
	res, err := svc.gj.GraphQL(svc.applyIdentityContext(ctx), query, nil, &core.RequestConfig{})
	if err != nil {
		return nil, err.Error()
	}
	if len(res.Errors) != 0 {
		return nil, catalogGraphQLErrors(res.Errors)
	}
	var out map[string]json.RawMessage
	_ = json.Unmarshal(res.Data, &out)
	return out, ""
}

func threadRows(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode rows %s: %v", raw, err)
	}
	return rows
}

// Threads, messages and runs are owner-scoped GraphQL roots: a GUI creates,
// renames, marks read and deletes threads, and reads their messages and runs,
// with no thread REST endpoints.
func TestThreadsAreGraphQLRoots(t *testing.T) {
	svc := newSQLiteThreadService(t)
	alice := threadUserCtx("alice")
	bob := threadUserCtx("bob")
	admin := contextWithUserRole(artifactUserCtx("root"), "admin")

	out, errText := threadGraphQL(t, svc, alice, `mutation { gj_thread(insert: { title: "Churn review" }) { id title unread } }`)
	if errText != "" {
		t.Fatalf("create thread: %s", errText)
	}
	var created map[string]any
	_ = json.Unmarshal(out["gj_thread"], &created)
	id, _ := created["id"].(string)
	if id == "" || created["title"] != "Churn review" {
		t.Fatalf("created thread = %s", out["gj_thread"])
	}
	thread, err := svc.ownedThreadStoreRow(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"how many accounts churned?", "3 accounts churned."} {
		role := "user"
		if i == 1 {
			role = "assistant"
		}
		if _, err := svc.appendMessage(alice, messageSpec{ThreadID: id, Role: role, Content: content, Status: "answered",
			AccountID: stringMapValue(thread, "account_id"), OwnerID: stringMapValue(thread, "owner_id")}); err != nil {
			t.Fatal(err)
		}
	}

	if _, errText := threadGraphQL(t, svc, alice, `mutation { gj_thread(where: { id: { eq: "`+id+`" } }, update: { title: "Churn, Q3" }) { id title } }`); errText != "" {
		t.Fatalf("rename: %s", errText)
	}
	out, _ = threadGraphQL(t, svc, alice, `query { gj_thread(where: { id: { eq: "`+id+`" } }) { id title unread } }`)
	rows := threadRows(t, out["gj_thread"])
	if len(rows) != 1 || rows[0]["title"] != "Churn, Q3" || rows[0]["unread"] != true {
		t.Fatalf("thread after rename = %v, want renamed and unread", rows)
	}
	if _, errText := threadGraphQL(t, svc, alice, `mutation { gj_thread(where: { id: { eq: "`+id+`" } }, update: { last_seen_at: "now" }) { id unread } }`); errText != "" {
		t.Fatalf("mark read: %s", errText)
	}
	out, _ = threadGraphQL(t, svc, alice, `query { gj_thread { id unread } }`)
	if rows := threadRows(t, out["gj_thread"]); len(rows) != 1 || rows[0]["unread"] != false {
		t.Fatalf("thread after mark read = %v", rows)
	}

	out, _ = threadGraphQL(t, svc, alice, `query { gj_thread_message(where: { thread_id: { eq: "`+id+`" } }, order_by: { seq: asc }) { seq role content } }`)
	messages := threadRows(t, out["gj_thread_message"])
	if len(messages) != 2 || messages[1]["content"] != "3 accounts churned." {
		t.Fatalf("messages = %v", messages)
	}

	for _, ctx := range []context.Context{bob} {
		out, _ = threadGraphQL(t, svc, ctx, `query { gj_thread { id } gj_thread_message { id } }`)
		if len(threadRows(t, out["gj_thread"])) != 0 || len(threadRows(t, out["gj_thread_message"])) != 0 {
			t.Fatalf("bob sees alice's conversation: %s", out)
		}
	}
	out, _ = threadGraphQL(t, svc, admin, `query { gj_thread { id } }`)
	if len(threadRows(t, out["gj_thread"])) != 1 {
		t.Fatalf("admin threads = %s, want alice's", out["gj_thread"])
	}

	if _, errText := threadGraphQL(t, svc, alice, `mutation { gj_thread_message(where: { thread_id: { eq: "`+id+`" } }, update: { content: "rewritten" }) { id } }`); errText == "" {
		t.Fatal("a message was rewritten through GraphQL")
	}
	if _, errText := threadGraphQL(t, svc, bob, `mutation { gj_thread(where: { id: { eq: "`+id+`" } }, delete: true) { id } }`); errText == "" {
		t.Fatal("bob deleted alice's thread")
	}
	if _, errText := threadGraphQL(t, svc, alice, `mutation { gj_thread(where: { id: { eq: "`+id+`" } }, delete: true) { id deleted } }`); errText != "" {
		t.Fatalf("delete: %s", errText)
	}
	out, _ = threadGraphQL(t, svc, alice, `query { gj_thread { id } gj_thread_message { id } }`)
	if len(threadRows(t, out["gj_thread"])) != 0 || len(threadRows(t, out["gj_thread_message"])) != 0 {
		t.Fatalf("a deleted thread left rows: %s", out)
	}
	if !strings.HasPrefix(id, "th_") {
		t.Fatalf("thread id %q", id)
	}
}
