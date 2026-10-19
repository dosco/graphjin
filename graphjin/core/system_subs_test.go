package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type managedSubscriptionTestHandler struct {
	mu   sync.RWMutex
	rows []map[string]any
}

func (h *managedSubscriptionTestHandler) ManagedQueryTables() []ManagedTable {
	return managedCursorTestHandler{}.ManagedQueryTables()
}

func (h *managedSubscriptionTestHandler) ExecuteManagedQuery(
	_ context.Context,
	req ManagedQueryRequest,
) (json.RawMessage, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows := make([]map[string]any, 0, len(h.rows))
		for _, source := range h.rows {
			row := make(map[string]any, len(root.Fields))
			for _, field := range root.Fields {
				row[field.Name] = source[field.Column]
			}
			rows = append(rows, row)
		}
		out[root.FieldName] = rows
	}
	return json.Marshal(out)
}

func (h *managedSubscriptionTestHandler) replace(rows []map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rows = rows
}

func newNanoSystemSubscriptionTestGraphJin(t *testing.T) (*GraphJin, *NanoDB) {
	t.Helper()
	db, err := NewNanoDB(NanoSnapshot{
		Schema: "main",
		Tables: []NanoTable{{
			Name: "gj_event_test",
			Columns: []NanoColumn{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "value", Type: "text"},
			},
			Rows: []NanoRow{{"id": 1, "value": "one"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		SubsPollDuration: minPollDuration,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
	}
	gj, err := NewGraphJin(conf, nil, OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}))
	if err != nil {
		t.Fatal(err)
	}
	return gj, db
}

func TestNanoDBSystemSubscriptionPagesAcrossPolls(t *testing.T) {
	gj, db := newNanoSystemSubscriptionTestGraphJin(t)
	defer gj.Close()

	query := `subscription stream($gj_event_test_cursor: Cursor) {
		gj_event_test(first: 1, after: $gj_event_test_cursor, order_by: { id: asc }) {
			id
			value
		}
		gj_event_test_cursor
	}`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, err := gj.Subscribe(ctx, query, json.RawMessage(`{"gj_event_test_cursor":null}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer member.Unsubscribe()

	first := receiveSystemSubscriptionResult(t, ctx, member)
	if got := systemSubscriptionIDs(t, first, "gj_event_test"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("initial ids = %v, want [1]", got)
	}
	if first.SubscriptionCursors()["gj_event_test_cursor"] == "" {
		t.Fatalf("initial cursor missing: %v", first.SubscriptionCursors())
	}

	if err := UpdateNanoDB(db, func(update *NanoUpdate) error {
		return update.ReplaceTable("main", "gj_event_test", []NanoRow{
			{"id": 1, "value": "one"},
			{"id": 2, "value": "two"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	second := receiveSystemSubscriptionResult(t, ctx, member)
	if got := systemSubscriptionIDs(t, second, "gj_event_test"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("second ids = %v, want [2]", got)
	}
	if second.SubscriptionCursors()["gj_event_test_cursor"] == first.SubscriptionCursors()["gj_event_test_cursor"] {
		t.Fatal("cursor did not advance")
	}

	select {
	case result := <-member.Result:
		t.Fatalf("unchanged/empty page emitted unexpectedly: %s", result.Data)
	case <-time.After(2 * minPollDuration):
	}
}

func TestNanoDBSystemSubscriptionIdentityIsolation(t *testing.T) {
	const (
		user1 = "user-1"
		user2 = "user-2"
	)
	db, err := NewNanoDB(NanoSnapshot{
		Schema: "main",
		Tables: []NanoTable{{
			Name: "docs",
			Columns: []NanoColumn{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "owner_ref", Type: "text", Index: true},
				{Name: "title", Type: "text"},
			},
			Rows: []NanoRow{
				{"id": "a", "owner_ref": user1, "title": "one"},
				{"id": "b", "owner_ref": user2, "title": "two"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		SubsPollDuration: minPollDuration,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
		Roles: []Role{{
			Name: "user",
			Tables: []RoleTable{{
				Name:     "docs",
				Database: DefaultDBName,
				Query: &Query{
					Filters: []string{`{ owner_ref: { eq: $user_ref } }`},
					Columns: []string{"id", "owner_ref", "title"},
				},
			}},
		}},
	}
	gj, err := NewGraphJin(conf, nil, OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}))
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	query := `subscription shared_identity { docs(order_by: { id: asc }) { id title } }`
	ctx1 := context.WithValue(context.Background(), UserIDKey, user1)
	ctx1 = context.WithValue(ctx1, IdentityVarsKey, map[string]any{"user_ref": user1})
	ctx2 := context.WithValue(context.Background(), UserIDKey, user2)
	ctx2 = context.WithValue(ctx2, IdentityVarsKey, map[string]any{"user_ref": user2})

	member1, err := gj.Subscribe(ctx1, query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer member1.Unsubscribe()
	member2, err := gj.Subscribe(ctx2, query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer member2.Unsubscribe()

	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, timeout, member1), "docs"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("user 1 initial ids = %v, want [a]", got)
	}
	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, timeout, member2), "docs"); len(got) != 1 || got[0] != "b" {
		t.Fatalf("user 2 initial ids = %v, want [b]", got)
	}

	if err := UpdateNanoDB(db, func(update *NanoUpdate) error {
		return update.ReplaceTable("main", "docs", []NanoRow{
			{"id": "a", "owner_ref": user1, "title": "one"},
			{"id": "b", "owner_ref": user2, "title": "two"},
			{"id": "c", "owner_ref": user1, "title": "three"},
			{"id": "d", "owner_ref": user2, "title": "four"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, timeout, member1), "docs"); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("user 1 poll ids = %v, want [a c]", got)
	}
	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, timeout, member2), "docs"); len(got) != 2 || got[0] != "b" || got[1] != "d" {
		t.Fatalf("user 2 poll ids = %v, want [b d]", got)
	}
}

func TestManagedSystemSubscriptionUsesManagedExecutor(t *testing.T) {
	handler := &managedSubscriptionTestHandler{rows: []map[string]any{{
		"id": "a", "rank": 1, "title": "one",
	}}}
	gj := newManagedCursorTestGraphJin(t, handler)
	defer gj.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, err := gj.Subscribe(ctx, `subscription managed_stream($gj_managed_item_cursor: Cursor) {
		gj_managed_item(first: 1, after: $gj_managed_item_cursor, order_by: { rank: asc }) {
			id
			rank
		}
		gj_managed_item_cursor
	}`, json.RawMessage(`{"gj_managed_item_cursor":null}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer member.Unsubscribe()

	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, ctx, member), "gj_managed_item"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("initial managed ids = %v, want [a]", got)
	}
	handler.replace([]map[string]any{
		{"id": "a", "rank": 1, "title": "one"},
		{"id": "b", "rank": 2, "title": "two"},
	})
	if got := systemSubscriptionStringIDs(t, receiveSystemSubscriptionResult(t, ctx, member), "gj_managed_item"); len(got) != 1 || got[0] != "b" {
		t.Fatalf("polled managed ids = %v, want [b]", got)
	}
}

func TestNanoDBSystemSubscriptionExtractsNamedRootCursors(t *testing.T) {
	db, err := NewNanoDB(NanoSnapshot{
		Schema: "main",
		Tables: []NanoTable{
			{
				Name:    "gj_alpha",
				Columns: []NanoColumn{{Name: "id", Type: "integer", PrimaryKey: true}},
				Rows:    []NanoRow{{"id": 1}},
			},
			{
				Name:    "gj_beta",
				Columns: []NanoColumn{{Name: "id", Type: "integer", PrimaryKey: true}},
				Rows:    []NanoRow{{"id": 10}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		SubsPollDuration: minPollDuration,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
	}
	gj, err := NewGraphJin(conf, nil, OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}))
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, err := gj.Subscribe(ctx, `subscription roots($gj_alpha_cursor: Cursor, $gj_beta_cursor: Cursor) {
		gj_alpha(first: 1, after: $gj_alpha_cursor, where: { id: { gt: 0 } }, order_by: { id: asc }) { id }
		gj_alpha_cursor
		gj_beta(first: 1, after: $gj_beta_cursor, order_by: { id: asc }) { id }
		gj_beta_cursor
	}`, json.RawMessage(`{"gj_alpha_cursor":null,"gj_beta_cursor":null}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer member.Unsubscribe()
	result := receiveSystemSubscriptionResult(t, ctx, member)
	cursors := result.SubscriptionCursors()
	if cursors["gj_alpha_cursor"] == "" || cursors["gj_beta_cursor"] == "" {
		t.Fatalf("named cursors missing: %v", cursors)
	}
	if cursors["gj_alpha_cursor"] == cursors["gj_beta_cursor"] {
		t.Fatalf("named root cursors should be independent: %v", cursors)
	}
	roots := member.SubscriptionRoots()
	if len(roots) != 2 {
		t.Fatalf("subscription roots = %+v", roots)
	}
	var alpha *SubscriptionRootInfo
	for index := range roots {
		if roots[index].Table == "gj_alpha" {
			alpha = &roots[index]
			break
		}
	}
	if alpha == nil || alpha.CursorVar != "gj_alpha_cursor" {
		t.Fatalf("gj_alpha subscription root missing: %+v", roots)
	}
	if _, hasSyntheticSeek := alpha.Filter["or"]; hasSyntheticSeek {
		t.Fatalf("subscription filter retained synthetic cursor seek: %+v", alpha.Filter)
	}
	alpha.Filter["mutated"] = true
	for _, root := range member.SubscriptionRoots() {
		if _, changed := root.Filter["mutated"]; changed {
			t.Fatal("SubscriptionRoots returned mutable internal filter state")
		}
	}
}

func receiveSystemSubscriptionResult(t *testing.T, ctx context.Context, member *Member) *Result {
	t.Helper()
	select {
	case result := <-member.Result:
		return result
	case <-ctx.Done():
		t.Fatal("timed out waiting for system subscription result")
		return nil
	}
}

func systemSubscriptionIDs(t *testing.T, result *Result, field string) []int {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode subscription result: %v\n%s", err, result.Data)
	}
	var rows []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data[field], &rows); err != nil {
		t.Fatalf("decode subscription rows: %v\n%s", err, result.Data)
	}
	ids := make([]int, len(rows))
	for index := range rows {
		ids[index] = rows[index].ID
	}
	return ids
}

func systemSubscriptionStringIDs(t *testing.T, result *Result, field string) []string {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode subscription result: %v\n%s", err, result.Data)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data[field], &rows); err != nil {
		t.Fatalf("decode subscription rows: %v\n%s", err, result.Data)
	}
	ids := make([]string, len(rows))
	for index := range rows {
		ids[index] = rows[index].ID
	}
	return ids
}
