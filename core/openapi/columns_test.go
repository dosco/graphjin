package openapi

import "testing"

func TestSynthesiseArgsDefaultsRelaxNotNull(t *testing.T) {
	op := OpDescriptor{
		PathParams: []ParamSpec{
			{Name: "region", In: ParamInPath, Required: true, Type: "string"},
			{Name: "id", In: ParamInPath, Required: true, Type: "string"},
		},
		QueryParams: []ParamSpec{
			{Name: "limit", In: ParamInQuery, Required: false, Type: "integer"},
			{Name: "sort", In: ParamInQuery, Required: true, Type: "string"},
		},
		Defaults: map[string]string{
			"region": "us-east",
			"sort":   "asc",
		},
	}

	cols := SynthesiseArgs("public", "widgets", op)
	byName := map[string]bool{}
	for _, c := range cols {
		byName[c.Name] = c.NotNull
	}

	// Required + default → nullable in the synthesised schema.
	if byName["region"] {
		t.Errorf("region NotNull = true, want false (required path param with default)")
	}
	if byName["sort"] {
		t.Errorf("sort NotNull = true, want false (required query param with default)")
	}
	// Required + no default → still NotNull.
	if !byName["id"] {
		t.Errorf("id NotNull = false, want true (required, no default)")
	}
	// Optional → unaffected by defaults.
	if byName["limit"] {
		t.Errorf("limit NotNull = true, want false (optional param)")
	}
}

func TestSynthesiseArgsNoDefaultsUnchanged(t *testing.T) {
	op := OpDescriptor{
		PathParams: []ParamSpec{
			{Name: "id", In: ParamInPath, Required: true, Type: "string"},
		},
		QueryParams: []ParamSpec{
			{Name: "verbose", In: ParamInQuery, Required: false, Type: "boolean"},
		},
	}
	cols := SynthesiseArgs("public", "widgets", op)
	if len(cols) != 2 {
		t.Fatalf("cols = %d, want 2", len(cols))
	}
	for _, c := range cols {
		if c.Name == "id" && !c.NotNull {
			t.Errorf("id NotNull = false, want true")
		}
		if c.Name == "verbose" && c.NotNull {
			t.Errorf("verbose NotNull = true, want false")
		}
	}
}
