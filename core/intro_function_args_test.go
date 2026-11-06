package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Issue #446: a zero-argument function produced an INPUT_OBJECT with null
// inputFields, which GraphQL clients reject during introspection.
func TestIntrospectionOmitsEmptyFunctionArgsTypes(t *testing.T) {
	cols := []sdata.DBColumn{{Schema: "public", Table: "products", Name: "id", Type: "bigint", PrimaryKey: true}}
	fns := []sdata.DBFunction{
		{Schema: "public", Name: "id_generator", Type: "bigint"},
		{Schema: "public", Name: "add_one", Type: "bigint", Inputs: []sdata.DBFuncParam{{ID: 1, Name: "n", Type: "bigint"}}},
	}
	result := introspectDBInfo(t, sdata.NewDBInfo("postgres", 0, "public", "", cols, fns, nil), nil)

	for _, ty := range result.Schema.Types {
		if ty.Kind == KIND_INPUT_OBJ && len(ty.InputFields) == 0 {
			t.Errorf("input object %s has no input fields", ty.Name)
		}
		if ty.Name == "productsid_generatorArgsInput" {
			t.Errorf("unexpected args type for zero-argument function: %s", ty.Name)
		}
	}
	requireIntroType(t, result, "productsadd_oneArgsInput")
}
