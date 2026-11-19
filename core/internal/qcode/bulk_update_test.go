package qcode_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func bulkUpdateCompiler(t *testing.T, dbType string) *qcode.Compiler {
	t.Helper()
	di := sdata.GetTestDBInfo()
	di.Type = dbType
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return qc
}

func compileBulkUpdate(qc *qcode.Compiler, gql, data string) (*qcode.QCode, error) {
	vars := map[string]json.RawMessage{"data": json.RawMessage(data)}
	return qc.Compile([]byte(gql), vars, "admin", "")
}

// A list update matches each row to its target by primary key, so the list
// scopes the update and the where clause becomes optional.
func TestBulkUpdateWithoutWhere(t *testing.T) {
	for _, dbType := range []string{"", "postgres", "mysql", "mariadb", "sqlite"} {
		t.Run(dbType, func(t *testing.T) {
			qc := bulkUpdateCompiler(t, dbType)
			res, err := compileBulkUpdate(qc, `mutation { products(update: $data) { id } }`,
				`[{"id": 1, "name": "a"}, {"id": 2, "name": "b"}]`)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Mutates) != 1 || !res.Mutates[0].Array {
				t.Fatalf("expected one list mutation, got %+v", res.Mutates)
			}

			// A single object still needs a where clause.
			_, err = compileBulkUpdate(qc, `mutation { products(update: $data) { id } }`,
				`{"id": 1, "name": "a"}`)
			if err == nil || err.Error() != "where clause required" {
				t.Fatalf("single-object update without where: got %v", err)
			}
		})
	}
}

// Snowflake joins list rows by primary key but still needs a where clause.
func TestBulkUpdateSnowflakeRequiresWhere(t *testing.T) {
	qc := bulkUpdateCompiler(t, "snowflake")
	data := `[{"id": 1, "name": "a"}, {"id": 2, "name": "b"}]`
	if _, err := compileBulkUpdate(qc, `mutation { products(update: $data) { id } }`, data); err == nil || err.Error() != "where clause required" {
		t.Fatalf("expected where clause required, got %v", err)
	}
	if _, err := compileBulkUpdate(qc, `mutation { products(update: $data, where: { id: { in: [1, 2] } }) { id } }`, data); err != nil {
		t.Fatal(err)
	}
}

// Databases that would apply one list item to every matched row reject list
// updates instead.
func TestBulkUpdateUnsupportedDatabases(t *testing.T) {
	for _, dbType := range []string{"mssql", "oracle", "mongodb"} {
		t.Run(dbType, func(t *testing.T) {
			qc := bulkUpdateCompiler(t, dbType)
			_, err := compileBulkUpdate(qc, `mutation { products(update: $data, where: { id: { in: [1, 2] } }) { id } }`,
				`[{"id": 1, "name": "a"}, {"id": 2, "name": "b"}]`)
			if err == nil || !strings.Contains(err.Error(), "not supported for "+dbType) {
				t.Fatalf("expected unsupported error, got %v", err)
			}
		})
	}
}

func TestBulkUpdateRejectsUnmatchableRows(t *testing.T) {
	qc := bulkUpdateCompiler(t, "postgres")
	cases := []struct {
		name, gql, data, want string
	}{
		{
			name: "missing primary key",
			gql:  `mutation { products(update: $data) { id } }`,
			data: `[{"name": "a"}, {"name": "b"}]`,
			want: `every row must include the primary key "id" of table "products"`,
		},
		{
			name: "missing primary key with where",
			gql:  `mutation { products(update: $data, where: { id: { in: [1, 2] } }) { id } }`,
			data: `[{"name": "a"}]`,
			want: `every row must include the primary key "id"`,
		},
		{
			name: "empty list",
			gql:  `mutation { products(update: $data) { id } }`,
			data: `[]`,
			want: "the list of rows is empty",
		},
		{
			name: "nested mutation",
			gql:  `mutation { products(update: $data) { id } }`,
			data: `[{"id": 1, "name": "a", "users": {"email": "a@b.c"}}]`,
			want: "nested mutations are not supported",
		},
		{
			name: "inline list",
			gql:  `mutation { products(update: [{ id: 1, name: "a" }, { id: 2, name: "b" }], where: { id: { in: [1, 2] } }) { id } }`,
			data: `null`,
			want: "pass the list of rows as a variable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileBulkUpdate(qc, tc.gql, tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}
