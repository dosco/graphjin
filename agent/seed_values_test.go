package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// A seed-digest table lists the values its columns hold, for the columns the
// catalog has sampled a closed set for and no others.
func TestSeedDigestListsTheValuesItsColumnsHold(t *testing.T) {
	runtime, _ := ticketValueRuntime(t)
	digest := []any{
		map[string]any{"id": "table:app:main.support_tickets", "kind": "table", "name": "support_tickets", "columns": []string{"id:integer", "status:text", "subject:text"}},
		// No column card of this table carries sampled values.
		map[string]any{"id": "table:app:main.accounts", "kind": "table", "name": "accounts", "columns": []string{"id:integer", "status:text"}},
		// A table the seed named without any of its columns.
		map[string]any{"id": "table:app:main.invoices", "kind": "table", "name": "invoices"},
		map[string]any{"id": "workflow:dunning_retry_check", "kind": "workflow", "name": "dunning_retry_check"},
	}
	got := runtime.withStoredValues(context.Background(), digest)
	if len(got) != len(digest) {
		t.Fatalf("digest length changed: %v", got)
	}
	tickets := mapValue(got[0])
	want := map[string]any{"status": []string{"open", "pending", "resolved"}}
	if !reflect.DeepEqual(tickets["stored_values"], want) {
		t.Fatalf("support_tickets stored_values = %v, want %v", tickets["stored_values"], want)
	}
	if !reflect.DeepEqual(tickets["columns"], []string{"id:integer", "status:text", "subject:text"}) {
		t.Fatalf("the column list changed: %v", tickets["columns"])
	}
	for _, index := range []int{1, 2, 3} {
		if _, has := mapValue(got[index])["stored_values"]; has {
			t.Errorf("entry %d has stored values it should not: %v", index, got[index])
		}
	}
	// A column the digest does not list is not added through its values.
	narrow := runtime.withStoredValues(context.Background(), []any{
		map[string]any{"id": "table:app:main.support_tickets", "kind": "table", "name": "support_tickets", "columns": []string{"id:integer", "subject:text"}},
	})
	if _, has := mapValue(narrow[0])["stored_values"]; has {
		t.Errorf("values were listed for a column the digest does not name: %v", narrow[0])
	}
}

// seedValueRuntime answers the seed search with the ticket table and its
// status column, and serves that column's sampled values the way the live
// catalog does: only on a lookup by id.
type seedValueRuntime struct {
	columnCardRuntime
}

func (r *seedValueRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if stringArg(args, "search") != "" {
		return map[string]any{"count": 2, "cards": []any{
			map[string]any{"id": "table:app:main.support_tickets", "kind": "table", "table_name": "support_tickets"},
			map[string]any{"id": "column:app:main.support_tickets.status", "kind": "column", "table_name": "support_tickets", "column_name": "status", "summary": "text, not null"},
		}}, nil
	}
	return r.columnCardRuntime.QueryCatalog(ctx, args)
}

// End to end, through Ax's own prompt assembly: the first model call of a
// plain question shows how the status column's values are spelled, before any
// query is written.
func TestAPlainQuestionIsShownHowAColumnsValuesAreSpelled(t *testing.T) {
	rec := &recordingClient{}
	rt := &seedValueRuntime{columnCardRuntime{values: map[string][]string{"status": {"open", "pending", "resolved"}}}}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
		rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
	)
	_, _ = runner.Run(context.Background(), Request{
		Instruction:  "How many tickets are still waiting on us?",
		Capabilities: profileWithRoleAndRoots("user"),
	})
	if len(rec.calls) == 0 {
		t.Fatal("no model call")
	}
	first := dumpAXValue(rec.calls[0].values)
	for _, want := range []string{"seed_digest", "status:text", "stored_values", "open", "pending", "resolved"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first model call does not carry %q", want)
		}
	}
}
