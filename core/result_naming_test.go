package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestNameColumnPrefersNamesThenUniqueText(t *testing.T) {
	text := func(name string) sdata.DBColumn { return sdata.DBColumn{Name: name, Type: "text"} }
	cases := []struct {
		name  string
		table sdata.DBTable
		want  string
	}{
		{"a name column", sdata.DBTable{Columns: []sdata.DBColumn{{Name: "id", Type: "bigint", PrimaryKey: true}, text("email"), text("name")}}, "name"},
		{"a person's full name", sdata.DBTable{Columns: []sdata.DBColumn{text("full_name"), {Name: "email", Type: "text", UniqueKey: true}}}, "full_name"},
		{"a unique reference", sdata.DBTable{Columns: []sdata.DBColumn{{Name: "amount", Type: "bigint"}, {Name: "reference", Type: "character varying", UniqueKey: true}}}, "reference"},
		{"nothing names the rows", sdata.DBTable{Columns: []sdata.DBColumn{{Name: "id", Type: "bigint", PrimaryKey: true}, text("subject")}}, ""},
		{"a numeric name is not a name", sdata.DBTable{Columns: []sdata.DBColumn{{Name: "name", Type: "integer"}}}, ""},
		{"a blocked name is not offered", sdata.DBTable{Columns: []sdata.DBColumn{{Name: "name", Type: "text", Blocked: true}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nameColumn(tc.table); got != tc.want {
				t.Fatalf("nameColumn = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNamingGapsReportOnlyPathsWithRows(t *testing.T) {
	gap := func(path ...string) namingGapCandidate {
		return namingGapCandidate{info: NamingGapInfo{Path: joinPath(path), Table: "accounts", KeyColumn: "account_id", NameColumn: "name", Relationship: "accounts"}, path: path}
	}
	res := &Result{
		Data:       []byte(`{"subscriptions":[{"account_id":1}],"invoices":[],"tickets":[{"id":2,"accounts":{"name":"x"}}]}`),
		namingGaps: []namingGapCandidate{gap("subscriptions"), gap("invoices"), gap("missing")},
	}
	got := res.NamingGaps()
	if len(got) != 1 || got[0].Path != "subscriptions" {
		t.Fatalf("NamingGaps = %+v, want only subscriptions", got)
	}
	if (&Result{}).NamingGaps() != nil {
		t.Fatal("empty result reported gaps")
	}
}
