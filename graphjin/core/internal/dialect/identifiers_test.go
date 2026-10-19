package dialect

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestIdentifierNamesAreScopedAndAliasesAreLiteral(t *testing.T) {
	for _, d := range []Dialect{&SnowflakeDialect{}, &BigQueryDialect{}, &RedshiftDialect{}, &MSSQLDialect{}} {
		t.Run(d.Name(), func(t *testing.T) {
			tables := []sdata.DBTable{
				{Schema: "prod", OrigSchema: "Prod", Name: "account", OrigName: "Account", Columns: []sdata.DBColumn{{Name: "id", OrigName: "Id"}, {Name: "name", OrigName: "Name"}}},
				{Schema: "analytics", OrigSchema: "analytics", Name: "account", OrigName: "account", Columns: []sdata.DBColumn{{Name: "id", OrigName: "id"}, {Name: "name", OrigName: "name"}}},
				{Schema: "prod", Name: "account_0", Columns: []sdata.DBColumn{{Name: "id", OrigName: "OtherID"}}},
				{Schema: "warehouse", OrigSchema: "WAREHOUSE", Name: "account", OrigName: "ACCOUNT", Columns: []sdata.DBColumn{{Name: "id", OrigName: "ID"}, {Name: "name", OrigName: "NAME"}}},
			}
			setter := d.(NameMapSetter)
			quoter := d.(ScopedColumnQuoter)
			for _, reverse := range []bool{false, true} {
				if reverse {
					tables[0], tables[2] = tables[2], tables[0]
				}
				setter.SetNameMap(tables)
				for _, tc := range []struct{ schema, table, name, want string }{
					{"prod", "account", "id", "Id"}, {"analytics", "account", "id", "id"},
					{"prod", "account", "name", "Name"}, {"analytics", "account", "name", "name"},
					{"warehouse", "account", "id", "ID"}, {"warehouse", "account", "name", "NAME"},
					{"", "account_0", "id", "OtherID"}, {"prod", "account", "sum_id", "sum_id"},
					{"", "__cur", "id", "id"},
				} {
					got, err := quoter.QuoteColumn(tc.schema, tc.table, tc.name)
					if err != nil || got != d.QuoteIdentifier(tc.want) {
						t.Fatalf("%+v: got %s, %v", tc, got, err)
					}
				}
				if _, err := quoter.QuoteColumn("", "account", "id"); err == nil {
					t.Fatal("ambiguous table must require schema")
				}
				// A field alias named id must not acquire another table's Id spelling.
				want := `"id"`
				if d.Name() == "bigquery" {
					want = "`id`"
				}
				if d.Name() == "mssql" {
					want = "[id]"
				}
				if got := d.QuoteIdentifier("id"); got != want {
					t.Fatalf("alias renamed: %s", got)
				}
			}
			setter.SetNameMap(nil)
			got, err := quoter.QuoteColumn("prod", "account", "id")
			if err != nil || got != d.QuoteIdentifier("id") {
				t.Fatal("reload retained removed identifiers")
			}
		})
	}
}
