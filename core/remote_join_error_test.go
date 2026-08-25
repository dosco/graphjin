package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

type failingRemoteResolver struct{}

func (failingRemoteResolver) Resolve(context.Context, ResolverReq) ([]byte, error) {
	return nil, errors.New("upstream unavailable")
}

func TestExecRemoteJoinPreservesHealthyRootsOnResolverError(t *testing.T) {
	selects := []qcode.Select{{
		Field: qcode.Field{
			ParentID:   -1,
			FieldName:  "automations",
			SkipRender: qcode.SkipTypeRemote,
		},
		Table: "automations",
		Fields: []qcode.Field{{
			FieldName: "id",
		}},
	}}
	state := &gstate{
		gj: &graphjinEngine{
			trace: &tracer{},
			rmap: map[string]resItem{
				"automations": {Fn: failingRemoteResolver{}, Source: "openapi", Scope: "automations"},
			},
		},
		cs: &cstate{st: stmt{qc: &qcode.QCode{
			Selects: selects,
			Roots:   []int32{0},
			Remotes: 1,
		}}},
		data: []byte(`{"project":[{"id":"central-ops"}],"automations":"__remote__:automations"}`),
	}

	err := state.execRemoteJoin(context.Background())
	if err == nil || err.Error() != "automations: upstream unavailable" {
		t.Fatalf("execRemoteJoin error = %v", err)
	}

	var got struct {
		Project     []map[string]string `json:"project"`
		Automations json.RawMessage     `json:"automations"`
	}
	if err := json.Unmarshal(state.data, &got); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, state.data)
	}
	if len(got.Project) != 1 || got.Project[0]["id"] != "central-ops" {
		t.Fatalf("healthy project root was not preserved: %s", state.data)
	}
	if string(got.Automations) != "null" {
		t.Fatalf("failed remote root = %s, want null; response=%s", got.Automations, state.data)
	}
}
