package catalog

import (
	"testing"
)

// questionSnapshot is a small organizational schema of the shape a question
// in ordinary words is asked against: plural table names, status columns with
// a closed set of values, and several timestamp columns ending in _at.
func questionSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	tables := map[string][]string{
		"accounts":        {"id", "name", "plan", "status", "renewal_date", "last_active_at"},
		"support_tickets": {"id", "account_id", "subject", "status", "opened_at", "resolved_at", "sla_due_at"},
		"invoices":        {"id", "account_id", "amount_cents", "status", "due_at", "last_attempt_at"},
		"payments":        {"id", "invoice_id", "amount_cents", "recorded_at"},
	}
	md := &MetadataSnapshot{Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}}}
	for _, name := range []string{"accounts", "support_tickets", "invoices", "payments"} {
		tableID := "app:main." + name
		md.Tables = append(md.Tables, MetadataTable{
			ID: tableID, DatabaseName: "app", SchemaName: "main", TableName: name,
			ColumnCount: len(tables[name]), PrimaryKey: "id",
		})
		for ordinal, column := range tables[name] {
			md.Columns = append(md.Columns, MetadataColumn{
				ID: tableID + "." + column, TableID: tableID,
				DatabaseName: "app", SchemaName: "main", TableName: name,
				ColumnName: column, Type: "text", PrimaryKey: column == "id", Ordinal: ordinal,
			})
		}
	}
	return BuildWithOptions(md, nil, BuildOptions{ObservedColumnValues: map[string][]string{
		"column:app:main.accounts.status":        {"active", "cancelled", "churn_risk"},
		"column:app:main.support_tickets.status": {"open", "pending", "resolved"},
		"column:app:main.invoices.status":        {"failed", "open", "paid"},
	}})
}

// schemaRanks is each table and column card's place among the table and
// column cards a search returns, best first, from 1.
func schemaRanks(t *testing.T, snap *Snapshot, search string) map[string]int {
	t.Helper()
	result, err := snap.Query(Query{Search: search, Limit: maxQueryLimit})
	if err != nil {
		t.Fatal(err)
	}
	ranks := map[string]int{}
	for _, card := range result.Cards {
		if card.Kind == "table" || card.Kind == "column" {
			ranks[card.ID] = len(ranks) + 1
		}
	}
	return ranks
}

// A question names the value, not the column: "at churn risk" is the status
// column's value churn_risk. The words around it are ordinary ones that
// identifiers are made of as well, and "at" is a token of every *_at column.
// Counted the same as "churn", they put the timestamp columns first and the
// column that answers the question near the bottom.
func TestSearchFindsTheColumnWhoseValueAQuestionNames(t *testing.T) {
	snap := questionSnapshot(t)
	const status = "column:app:main.accounts.status"
	for _, question := range []string{
		"Is the account of support ticket 7 at churn risk?",
		"For support ticket 31, identify its associated account and whether that account is at churn risk.",
	} {
		ranks := schemaRanks(t, snap, question)
		if ranks[status] == 0 {
			t.Fatalf("%q: the accounts status column was not returned", question)
		}
		for _, other := range []string{"column:app:main.accounts.last_active_at", "column:app:main.invoices.due_at", "column:app:main.payments.recorded_at"} {
			if ranks[other] != 0 && ranks[other] < ranks[status] {
				t.Errorf("%q: %s (rank %d) is ahead of the status column (rank %d)", question, other, ranks[other], ranks[status])
			}
		}
		// It need not beat the ticket's own columns: the question is about a
		// ticket. It has to be among the first cards a run is shown.
		if ranks[status] > 12 {
			t.Errorf("%q: the status column ranks %d of %d table and column cards", question, ranks[status], len(ranks))
		}
	}
}

// A question says "invoice"; the table is invoices. Compared as written, the
// only whole-word match was another table's invoice_id column, and the table
// asked about matched as a fragment at a third of the strength.
func TestSearchMatchesAQuestionsWordsInTheSingular(t *testing.T) {
	snap := questionSnapshot(t)
	ranks := schemaRanks(t, snap, "Does the account linked to support ticket 25 have at least one failed invoice?")
	const (
		table   = "table:app:main.invoices"
		status  = "column:app:main.invoices.status"
		foreign = "column:app:main.payments.invoice_id"
	)
	if ranks[table] == 0 || ranks[status] == 0 {
		t.Fatalf("invoices table rank %d, its status column rank %d; both must be returned", ranks[table], ranks[status])
	}
	if ranks[foreign] != 0 && ranks[foreign] < ranks[status] {
		t.Errorf("payments.invoice_id (rank %d) is ahead of invoices.status (rank %d), whose value the question names", ranks[foreign], ranks[status])
	}
	if ranks[status] > 12 {
		t.Errorf("invoices.status ranks %d of %d table and column cards", ranks[status], len(ranks))
	}

	// The plural finds the singular as well.
	plural := schemaRanks(t, snap, "payments for invoices")
	if plural[foreign] == 0 {
		t.Error(`"invoices" did not find the invoice_id column`)
	}
}

// A word found on one card counts in full, a word found on most cards for
// little, and a word no card holds whole is not discounted: nothing says it
// is common.
func TestTermWeightFollowsHowRareAWordIs(t *testing.T) {
	idx := newSearchIndex(questionSnapshot(t))
	rare, common, unknown := idx.termWeight("churn"), idx.termWeight("at"), idx.termWeight("zzzunseen")
	if df := len(idx.postings["churn"]); df != 1 {
		t.Fatalf("churn is on %d cards, want the one status column", df)
	}
	if rare != 1 {
		t.Errorf("a word on one card weighs %v, want 1", rare)
	}
	if common <= 0 || common >= rare/2 {
		t.Errorf("at (on %d of %d cards) weighs %v, want well under churn's %v and above 0", len(idx.postings["at"]), len(idx.cards), common, rare)
	}
	if unknown != 1 {
		t.Errorf("a word on no card weighs %v, want 1", unknown)
	}
	for term := range idx.postings {
		if w := idx.termWeight(term); w <= 0 || w > 1 {
			t.Fatalf("termWeight(%q) = %v, want it in (0, 1]", term, w)
		}
	}
}

// Sampled values are searched as the column's own vocabulary, and only for a
// column that has them.
func TestSampledValuesAreSearchedLikeTheColumnsName(t *testing.T) {
	snap := questionSnapshot(t)
	card, ok := findCatalogCard(snap, "column:app:main.accounts.status")
	if !ok {
		t.Fatal("status column card missing")
	}
	if got := observedValuesText(card); got != "active cancelled churn_risk" {
		t.Fatalf("observedValuesText = %q", got)
	}
	plain, _ := findCatalogCard(snap, "column:app:main.accounts.name")
	if got := observedValuesText(plain); got != "" {
		t.Fatalf("a column with no sampled values has search text %q", got)
	}
	var names, values float64
	for _, field := range cardSearchFields(card, "") {
		switch field.field {
		case "names":
			names = field.weight
		case "values":
			values = field.weight
		}
	}
	if values == 0 || values != names {
		t.Errorf("values weigh %v and names %v, want them equal", values, names)
	}
	// The value alone finds the column first.
	result, err := snap.Query(Query{Search: "churn risk", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cards) == 0 || result.Cards[0].ID != card.ID {
		t.Errorf(`"churn risk" did not return the status column first: %v`, result.Cards)
	}
}

func TestSingularStripsAPlainPluralEnding(t *testing.T) {
	for word, want := range map[string]string{
		"invoices": "invoice", "tickets": "ticket", "accounts": "account", "users": "user",
		"categories": "category", "addresses": "address", "batches": "batch", "boxes": "box",
		// Not plurals, or too short to tell.
		"status": "status", "address": "address", "analysis": "analysis", "is": "is", "its": "its", "has": "has",
		"invoice": "invoice", "at": "at",
	} {
		if got := singular(word); got != want {
			t.Errorf("singular(%q) = %q, want %q", word, got, want)
		}
	}
}
