package psql_test

import (
	"regexp"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// TestMSSQLIdentifierMappingIsScopedByTable guards #571 with a deliberately
// synthetic schema. The columns normalize to the same GraphQL names even
// though their physical SQL Server identifiers use incompatible spellings.
// The renderer must recover each name from its selected table.
func TestMSSQLIdentifierMappingIsScopedByTable(t *testing.T) {
	cols := []sdata.DBColumn{
		{Schema: "dbo", Table: "synthetic_alpha", OrigSchema: "dbo", OrigTable: "SyntheticAlpha", Name: "row_id", OrigName: "Row_ID", Type: "int", PrimaryKey: true, UniqueKey: true},
		{Schema: "dbo", Table: "synthetic_alpha", OrigSchema: "dbo", OrigTable: "SyntheticAlpha", Name: "flag_value", OrigName: "Flag_Value", Type: "bit"},
		{Schema: "dbo", Table: "synthetic_alpha", OrigSchema: "dbo", OrigTable: "SyntheticAlpha", Name: "parent_id", OrigName: "parent_id", Type: "int"},
		{Schema: "dbo", Table: "synthetic_alpha", OrigSchema: "dbo", OrigTable: "SyntheticAlpha", Name: "payload_kind", OrigName: "Payload_Kind", Type: "nvarchar"},
		{Schema: "dbo", Table: "synthetic_beta", OrigSchema: "dbo", OrigTable: "SyntheticBeta", Name: "row_id", OrigName: "RowID", Type: "int", PrimaryKey: true, UniqueKey: true},
		{Schema: "dbo", Table: "synthetic_beta", OrigSchema: "dbo", OrigTable: "SyntheticBeta", Name: "flag_value", OrigName: "FlagValue", Type: "bit"},
		{Schema: "dbo", Table: "synthetic_beta", OrigSchema: "dbo", OrigTable: "SyntheticBeta", Name: "parent_id", OrigName: "parentId", Type: "int"},
	}

	info := sdata.NewDBInfo("mssql", 2022, "dbo", "db", cols, nil, nil)
	schema, err := sdata.NewDBSchema(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: "dbo"})
	if err != nil {
		t.Fatal(err)
	}
	for table, allowed := range map[string][]string{
		"synthetic_alpha": {"row_id", "flag_value", "parent_id", "payload_kind"},
		"synthetic_beta":  {"row_id", "flag_value", "parent_id"},
	} {
		if err := qc.AddRole("user", "dbo", table, qcode.TRConfig{Query: qcode.QueryConfig{Columns: allowed}}); err != nil {
			t.Fatal(err)
		}
	}

	compiled, err := qc.Compile([]byte(`query {
		synthetic_alpha { flag_value parent_id payload_kind }
		synthetic_beta { flag_value parent_id }
	}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "mssql", DBVersion: 2022})
	pc.SetSchemaInfo(schema.GetTables())
	_, sqlBytes, err := pc.CompileEx(compiled)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(sqlBytes)

	for _, want := range []string{
		`\[synthetic_alpha_[0-9]+\]\.\[Flag_Value\]`,
		`\[synthetic_alpha_[0-9]+\]\.\[parent_id\]`,
		`\[synthetic_alpha_[0-9]+\]\.\[Payload_Kind\]`,
		`\[synthetic_beta_[0-9]+\]\.\[FlagValue\]`,
		`\[synthetic_beta_[0-9]+\]\.\[parentId\]`,
	} {
		if !regexp.MustCompile(want).MatchString(sql) {
			t.Fatalf("MSSQL SQL missing table-scoped identifier %q:\n%s", want, sql)
		}
	}
}
