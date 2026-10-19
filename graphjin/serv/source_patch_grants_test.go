package serv

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestApplySourceAccessPatch_Grants(t *testing.T) {
	source := core.SourceConfig{Name: "shop"}
	var set sourceAccessConfigPatch
	if err := json.Unmarshal([]byte(`{"grants":[{"role":"og_all","tables":[{"name":"orders","columns":["id"]}]}]}`), &set); err != nil {
		t.Fatal(err)
	}
	if _, err := applySourceAccessPatch(&source, set); err != nil {
		t.Fatal(err)
	}
	if len(source.Access.Grants) != 1 || source.Access.Grants[0].Role != "og_all" {
		t.Fatalf("grants not set: %+v", source.Access.Grants)
	}

	var keep sourceAccessConfigPatch
	if err := json.Unmarshal([]byte(`{"read":"account"}`), &keep); err != nil {
		t.Fatal(err)
	}
	if _, err := applySourceAccessPatch(&source, keep); err != nil {
		t.Fatal(err)
	}
	if len(source.Access.Grants) != 1 {
		t.Fatalf("a patch without grants changed them: %+v", source.Access.Grants)
	}

	var clear sourceAccessConfigPatch
	if err := json.Unmarshal([]byte(`{"grants":null}`), &clear); err != nil {
		t.Fatal(err)
	}
	if _, err := applySourceAccessPatch(&source, clear); err != nil {
		t.Fatal(err)
	}
	if len(source.Access.Grants) != 0 {
		t.Fatalf("null did not clear grants: %+v", source.Access.Grants)
	}
}
