package openapi

import "testing"

// ResolveCallParams is the seam between GraphQL field arguments and the
// outgoing HTTP request. The cases below cover the precedence rules
// (arg wins over default), the missing-required guard, and the optional
// query-param pass-through behaviour the bridge previously had inline.
func TestResolveCallParams_DefaultFillsMissingPathParam(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "listWidgets",
		PathTemplate: "/api/region/{region}/widgets.json",
		PathParams: []ParamSpec{
			{Name: "region", In: ParamInPath, Required: true, Type: "string"},
		},
		Defaults: map[string]string{"region": "us-east"},
	}

	p, err := op.ResolveCallParams(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PathValues["region"] != "us-east" {
		t.Errorf("PathValues[region] = %q, want us-east (default)", p.PathValues["region"])
	}
}

func TestResolveCallParams_ArgOverridesDefault(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "listWidgets",
		PathTemplate: "/api/region/{region}/widgets.json",
		PathParams: []ParamSpec{
			{Name: "region", In: ParamInPath, Required: true, Type: "string"},
		},
		Defaults: map[string]string{"region": "us-east"},
	}

	p, err := op.ResolveCallParams(map[string]string{"region": "eu-central"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PathValues["region"] != "eu-central" {
		t.Errorf("PathValues[region] = %q, want eu-central (arg overrides default)", p.PathValues["region"])
	}
}

func TestResolveCallParams_MissingRequiredNoDefaultErrors(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "getWidgetById",
		PathTemplate: "/widgets/{id}",
		PathParams: []ParamSpec{
			{Name: "id", In: ParamInPath, Required: true, Type: "string"},
		},
	}

	if _, err := op.ResolveCallParams(nil); err == nil {
		t.Fatal("expected error for missing required path param with no default")
	}
}

func TestResolveCallParams_QueryParamDefaultsAndOverrides(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "listWidgets",
		PathTemplate: "/widgets",
		QueryParams: []ParamSpec{
			{Name: "limit", In: ParamInQuery, Type: "integer"},
			{Name: "sort", In: ParamInQuery, Type: "string"},
			{Name: "filter", In: ParamInQuery, Type: "string"},
		},
		Defaults: map[string]string{
			"limit": "25",
			"sort":  "asc",
		},
	}

	p, err := op.ResolveCallParams(map[string]string{"sort": "desc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.QueryValues["limit"] != "25" {
		t.Errorf("limit = %q, want 25 (default)", p.QueryValues["limit"])
	}
	if p.QueryValues["sort"] != "desc" {
		t.Errorf("sort = %q, want desc (caller arg overrides default)", p.QueryValues["sort"])
	}
	if _, present := p.QueryValues["filter"]; present {
		t.Errorf("filter unexpectedly present (no arg, no default)")
	}
}

func TestResolveCallParams_OptionalPathParamWithoutDefaultIsSkipped(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "listWidgets",
		PathTemplate: "/widgets/{region}",
		PathParams: []ParamSpec{
			{Name: "region", In: ParamInPath, Required: false, Type: "string"},
		},
	}

	p, err := op.ResolveCallParams(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := p.PathValues["region"]; present {
		t.Errorf("region unexpectedly present (optional, no arg, no default)")
	}
}
