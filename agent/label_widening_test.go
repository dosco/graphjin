package agent

import "testing"

func TestLabelWidening(t *testing.T) {
	labels := map[string]string{"accounts": "name", "tickets": "title"}
	which := "Which account has the soonest upcoming renewal?"
	for _, tc := range []struct {
		name, instruction, query, want string
	}{
		{"which, id only", which,
			`query { accounts(order_by: {renewal_date: asc}, limit: 1) { id renewal_date } }`,
			`query { accounts(order_by: {renewal_date: asc}, limit: 1) { name id renewal_date } }`},
		{"aliased root keeps its alias", "Who has the most open tickets?",
			`{ top: tickets(limit: 1) { id } }`, `{ top: tickets(limit: 1) { title id } }`},
		{"two roots, only the unlabelled one changes", which,
			`{ accounts { id } tickets { title } }`, `{ accounts { name id } tickets { title } }`},
		// Left alone: not a which/who question, already labelled, grouped or
		// aggregated (adding a column would change the grouping), distinct,
		// no label column known, or a mutation.
		{"not asking which", "How many accounts renew this month?", `{ accounts { id } }`, `{ accounts { id } }`},
		{"already named", which, `{ accounts { id name } }`, `{ accounts { id name } }`},
		{"grouped count", "Which plan tier has the most accounts?",
			`{ accounts(order_by: {count_id: desc}, limit: 1) { plan count_id } }`,
			`{ accounts(order_by: {count_id: desc}, limit: 1) { plan count_id } }`},
		{"distinct", which, `{ accounts(distinct: [plan]) { plan } }`, `{ accounts(distinct: [plan]) { plan } }`},
		{"unknown table", which, `{ invoices { id } }`, `{ invoices { id } }`},
		{"mutation", which, `mutation { accounts(insert: {id: 9}) { id } }`, `mutation { accounts(insert: {id: 9}) { id } }`},
	} {
		got, changed := labelWidening(tc.query, tc.instruction, labels)
		if got != tc.want || changed != (tc.query != tc.want) {
			t.Errorf("%s:\n got %q (changed %v)\nwant %q", tc.name, got, changed, tc.want)
		}
	}
}

func TestTableCardsRecordLabelColumns(t *testing.T) {
	state := newDiscoveryState("Which account renews first?")
	state.recordTableColumns(map[string]any{"kind": "table", "table_name": "accounts",
		"summary": "7 columns; primary key id; columns: id, title, name, plan; Customer accounts"})
	state.recordTableColumns(map[string]any{"kind": "table", "table_name": "payments",
		"summary": "40 columns; primary key id; key columns: id, invoice_id, amount_cents"})
	labels := state.labelColumnsByTable()
	if labels["accounts"] != "name" || labels["payments"] != "" {
		t.Fatalf("labels = %v, want accounts=name (preferred over title) and none for a wide table without one", labels)
	}
}
