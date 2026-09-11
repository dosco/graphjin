package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestPhysicalIdentifiersDoNotLeakAcrossTables(t *testing.T) {
	for _, dbType := range []string{"snowflake", "bigquery", "redshift", "mssql"} {
		t.Run(dbType, func(t *testing.T) {
			cols := []sdata.DBColumn{
				{Schema: "public", Table: "da", OrigSchema: "public", OrigTable: "da", Name: "crm_id", OrigName: "crm_id", Type: "int", FKeySchema: "public", FKeyTable: "crm", FKeyCol: "id", OrigFKeySchema: "public", OrigFKeyTable: "CRM", OrigFKeyCol: "Id"},
				{Schema: "public", Table: "da", OrigSchema: "public", OrigTable: "da", Name: "id", OrigName: "id", Type: "int", PrimaryKey: true},
				{Schema: "public", Table: "da", OrigSchema: "public", OrigTable: "da", Name: "name", OrigName: "name", Type: "text"},
				{Schema: "public", Table: "crm", OrigSchema: "public", OrigTable: "CRM", Name: "id", OrigName: "Id", Type: "int", PrimaryKey: true},
				{Schema: "public", Table: "crm", OrigSchema: "public", OrigTable: "CRM", Name: "name", OrigName: "Name", Type: "text"},
			}
			// A second schema has a conflicting table and spelling. Explicit
			// schema selection must keep it out of this query's identifier lookup.
			cols = append(cols, sdata.DBColumn{Schema: "archive", OrigSchema: "Archive", Table: "da", OrigTable: "DA", Name: "id", OrigName: "ID", Type: "int", PrimaryKey: true})
			info := sdata.NewDBInfo(dbType, 2022, "public", "db", cols, nil, nil)
			schema, err := sdata.NewDBSchema(info, map[string][]string{"crm": {"customer"}})
			if err != nil {
				t.Fatal(err)
			}
			qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: "public"})
			if err != nil {
				t.Fatal(err)
			}
			pc := psql.NewCompiler(psql.Config{DBType: dbType, DBVersion: 2022})
			pc.SetSchemaInfo(schema.GetTables())
			quote := pc.GetDialect().QuoteIdentifier
			multi, err := qc.Compile([]byte(`query { current: da @schema(name: "public") { id } old: da @schema(name: "archive") { id } }`), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			_, multiSQL, err := pc.CompileEx(multi)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{quote("public") + "." + quote("da"), quote("Archive") + "." + quote("DA"), quote("ID")} {
				if !strings.Contains(string(multiSQL), want) {
					t.Fatalf("cross-schema query missing %s:\n%s", want, multiSQL)
				}
			}
			joined, err := qc.Compile([]byte(`query { da { id name crm { id name } } }`), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			_, joinedSQL, err := pc.CompileEx(joined)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(joinedSQL), quote("da")+"."+quote("Id")) || !strings.Contains(string(joinedSQL), quote("public")+"."+quote("CRM")) {
				t.Fatalf("relationship lost physical identifier ownership:\n%s", joinedSQL)
			}
			for _, table := range []string{"da", "crm", "customer"} {
				physical := table
				id, name := "id", "name"
				if table != "da" {
					physical = "CRM"
					id = "Id"
					name = "Name"
				}
				for _, query := range []string{
					`mutation { ` + table + `(where: {id: {eq: 1}}, delete: true) { id name } }`,
					`query { ` + table + ` { name: id id: name } }`,
					`query { ` + table + ` { name: count_id } }`,
					`query { ` + table + `(where: {id: {eq: 1}}, order_by: {name: asc}) { id name } }`,
					`mutation { ` + table + `(insert: {id: 1, name: "test"}) { id name } }`,
					`mutation { ` + table + `(where: {id: {eq: 1}}, update: {name: "test"}) { id name } }`,
				} {
					compiled, err := qc.Compile([]byte(query), nil, "user", "")
					if err != nil {
						t.Fatal(err)
					}
					_, sqlBytes, err := pc.CompileEx(compiled)
					if err != nil {
						t.Fatal(err)
					}
					sql := string(sqlBytes)
					t.Logf("%s\n%s", query, sql)
					for _, want := range []string{quote("public") + "." + quote(physical), quote(id)} {
						if !strings.Contains(sql, want) {
							t.Fatalf("%s missing %s:\n%s", query, want, sql)
						}
					}
					if strings.Contains(query, "name: count_id") && dbType != "mssql" && !strings.Contains(sql, quote(compiled.Selects[0].Table+"_0")+"."+quote("name")) {
						t.Fatalf("aggregate alias was remapped to a physical name:\n%s", sql)
					}
					if strings.Contains(query, "update:") && !strings.Contains(sql, "WHERE (("+quote(physical)+"."+quote(id)+")") {
						t.Fatalf("mutation filter must qualify its physical target:\n%s", sql)
					}
					if table == "da" && (strings.Contains(sql, quote("Id")) || strings.Contains(sql, quote("Name"))) {
						t.Fatalf("foreign casing leaked:\n%s", sql)
					}
					if (strings.Contains(query, "insert:") || strings.Contains(query, "update:")) && !strings.Contains(sql, quote(name)+" =") && !strings.Contains(sql, "("+quote(id)+", "+quote(name)+")") && !strings.Contains(sql, "("+quote(name)+", "+quote(id)+")") {
						t.Fatalf("mutation did not use physical columns:\n%s", sql)
					}
				}
			}
		})
	}
}
