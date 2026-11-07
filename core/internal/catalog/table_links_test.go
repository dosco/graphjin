package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// linkedSnapshot is a small schema with every relationship shape the table
// cards must handle: a named parent (accounts), a child reaching it through
// one foreign key (subscriptions), a table holding two keys to the same
// parent (transfers), and a self-reference (employees.manager_id).
func linkedSnapshot(dbType string) *MetadataSnapshot {
	col := func(table, name, typ string, ord int, mods ...func(*MetadataColumn)) MetadataColumn {
		c := MetadataColumn{ID: "app:main." + table + "." + name, TableID: "app:main." + table, DatabaseName: "app",
			SchemaName: "main", TableName: table, ColumnName: name, Type: typ, Ordinal: ord}
		for _, m := range mods {
			m(&c)
		}
		return c
	}
	pk := func(c *MetadataColumn) { c.PrimaryKey = true }
	fk := func(from, to string) MetadataRelationship {
		f, t := strings.Split(from, "."), strings.Split(to, ".")
		return MetadataRelationship{
			ID: "app:main." + from + "->app:main." + to, Source: "foreign_key", RelType: "one_to_many",
			FromDatabaseName: "app", FromSchemaName: "main", FromTableName: f[0], FromColumnName: f[1], FromColumnID: "app:main." + from,
			ToDatabaseName: "app", ToSchemaName: "main", ToTableName: t[0], ToColumnName: t[1], ToColumnID: "app:main." + to,
		}
	}
	cols := []MetadataColumn{
		col("accounts", "id", "bigint", 0, pk), col("accounts", "name", "text", 1), col("accounts", "plan", "text", 2),
		col("subscriptions", "id", "bigint", 0, pk), col("subscriptions", "account_id", "bigint", 1),
		col("subscriptions", "status", "text", 2), col("subscriptions", "mrr_cents", "bigint", 3),
		col("transfers", "id", "bigint", 0, pk), col("transfers", "from_account_id", "bigint", 1),
		col("transfers", "to_account_id", "bigint", 2), col("transfers", "amount", "numeric", 3),
		col("employees", "id", "bigint", 0, pk), col("employees", "full_name", "text", 1), col("employees", "manager_id", "bigint", 2),
	}
	var tables []MetadataTable
	for _, name := range []string{"accounts", "subscriptions", "transfers", "employees"} {
		n := 0
		for _, c := range cols {
			if c.TableName == name {
				n++
			}
		}
		tables = append(tables, MetadataTable{ID: "app:main." + name, DatabaseName: "app", SchemaName: "main", TableName: name, ColumnCount: n, PrimaryKey: "id"})
	}
	return &MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: dbType}},
		Tables:    tables,
		Columns:   cols,
		Relationships: []MetadataRelationship{
			fk("subscriptions.account_id", "accounts.id"),
			fk("transfers.from_account_id", "accounts.id"),
			fk("transfers.to_account_id", "accounts.id"),
			fk("employees.manager_id", "employees.id"),
		},
	}
}

func tableCardExamples(t *testing.T, snap *Snapshot, table string) []string {
	t.Helper()
	card, ok := findCatalogCard(snap, "table:app:main."+table)
	if !ok {
		t.Fatalf("%s card missing", table)
	}
	var out []string
	if err := json.Unmarshal([]byte(card.ExamplesJSON), &out); err != nil {
		t.Fatalf("%s examples: %v", table, err)
	}
	return out
}

func tableCardRelationships(t *testing.T, snap *Snapshot, table string) []relatedTable {
	t.Helper()
	for _, d := range snap.Details {
		if d.ID == "table:app:main."+table+":relationships" {
			var data struct {
				Relationships []relatedTable `json:"relationships"`
			}
			if err := json.Unmarshal([]byte(d.DataJSON), &data); err != nil {
				t.Fatalf("%s relationships: %v", table, err)
			}
			return data.Relationships
		}
	}
	return nil
}

// Models copy the first example on the card they read. It used to be
// { subscriptions(limit: 10) { id account_id status mrr_cents } }: the
// id-and-foreign-key page that leaves an answer naming no account, and a row
// page to add up by hand. The card now teaches the parent's name through the
// join and a database aggregate beside the read.
func TestTableExampleNamesRowsAndAggregates(t *testing.T) {
	snap := Build(linkedSnapshot("postgres"), nil)

	got := tableCardExamples(t, snap, "subscriptions")
	if len(got) != 2 {
		t.Fatalf("want a read and an aggregate, got %q", got)
	}
	if want := "{ subscriptions(limit: 10) { id status mrr_cents accounts { name } } }"; got[0] != want {
		t.Fatalf("read example = %q, want %q", got[0], want)
	}
	if want := "{ subscriptions { count_id sum_mrr_cents } }"; got[1] != want {
		t.Fatalf("aggregate example = %q, want %q", got[1], want)
	}

	if got := tableCardExamples(t, snap, "accounts"); got[0] != "{ accounts(limit: 10) { id name plan } }" {
		t.Fatalf("a named table leads with id and its name: %q", got[0])
	}

	// Two keys to one parent make the nested field ambiguous, and a
	// self-reference needs recursive syntax: neither is taught as a join.
	if got := tableCardExamples(t, snap, "transfers"); strings.Contains(got[0], "accounts {") || !strings.Contains(got[0], "from_account_id") {
		t.Fatalf("ambiguous keys must stay plain columns: %q", got[0])
	}
	if got := tableCardExamples(t, snap, "employees"); strings.Contains(got[0], "employees {") {
		t.Fatalf("a self-reference is not a plain nesting: %q", got[0])
	}
}

// MongoDB does not aggregate through GraphJin yet, so its cards must not
// teach a count it would refuse.
func TestTableExampleSkipsAggregateWhereTheSourceCannot(t *testing.T) {
	got := tableCardExamples(t, Build(linkedSnapshot("mongodb"), nil), "subscriptions")
	if len(got) != 1 || strings.Contains(got[0], "count_") {
		t.Fatalf("mongodb card taught an aggregate: %q", got)
	}
}

// A table card lists the tables it joins to, so a model reading one table
// learns the field that names a related row without fetching relationship
// cards it rarely opens.
func TestTableCardListsItsRelationships(t *testing.T) {
	snap := Build(linkedSnapshot("postgres"), nil)

	sub := tableCardRelationships(t, snap, "subscriptions")
	if len(sub) != 1 {
		t.Fatalf("subscriptions relationships = %+v", sub)
	}
	if r := sub[0]; r.Field != "accounts" || r.Rows != "one" || r.Via != "subscriptions.account_id" || r.NameColumn != "name" ||
		r.Example != "{ subscriptions(limit: 10) { id accounts { name } } }" {
		t.Fatalf("subscriptions -> accounts = %+v", r)
	}

	acc := tableCardRelationships(t, snap, "accounts")
	if len(acc) != 1 {
		t.Fatalf("accounts must list only its unambiguous children: %+v", acc)
	}
	if r := acc[0]; r.Field != "subscriptions" || r.Rows != "many" || r.NameColumn != "" ||
		r.Example != "{ accounts(limit: 10) { name subscriptions(limit: 5) { id } } }" {
		t.Fatalf("accounts -> subscriptions = %+v", r)
	}

	if got := tableCardRelationships(t, snap, "employees"); got != nil {
		t.Fatalf("a self-reference only table has no relationships detail: %+v", got)
	}
}

// The column a table card teaches as the name must be the one core's naming
// check asks for, or the agent is told to select one column and then refused
// for lacking another.
func TestPickNameColumnPrefersNamesThenUniqueText(t *testing.T) {
	cases := []struct {
		cols []NamingColumn
		want string
	}{
		{[]NamingColumn{{Name: "title", Type: "text"}, {Name: "name", Type: "varchar(80)"}}, "name"},
		{[]NamingColumn{{Name: "name", Type: "text", Excluded: true}, {Name: "full_name", Type: "text"}}, "full_name"},
		{[]NamingColumn{{Name: "subject", Type: "text"}, {Name: "reference", Type: "text", Unique: true}}, "reference"},
		{[]NamingColumn{{Name: "name", Type: "integer"}, {Name: "code", Type: "bigint", Unique: true}}, ""},
	}
	for _, tc := range cases {
		if got := PickNameColumn(len(tc.cols), func(i int) NamingColumn { return tc.cols[i] }); got != tc.want {
			t.Errorf("PickNameColumn(%+v) = %q, want %q", tc.cols, got, tc.want)
		}
	}
}
