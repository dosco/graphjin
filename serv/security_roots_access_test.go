package serv

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// access.roots overrides on the graphjin source must gate the direct GraphQL
// path, not just MCP capability profiles: an admin-only root is denied with an
// error for role "user" (deny, not a silently null root) and served for an
// admin role. The artifacts-enabled variant covers the control-plane managed
// query handler, which used to serve gj_config to any role because it ignored
// the compiled role block (the coffee-roastery regression).
func TestSourceRootsAccessEnforcedOnDirectGraphQL(t *testing.T) {
	configure := func(artifactsEnabled bool) func(*Config) {
		return func(conf *Config) {
			conf.Core.Mode = "agentic"
			conf.Core.Artifacts.Enabled = artifactsEnabled
			for i := range conf.Core.Sources {
				if conf.Core.Sources[i].Kind == "graphjin" {
					conf.Core.Sources[i].Access.Roots = map[string]string{"gj_config": core.AccessModeAdmin}
					conf.Core.Sources[i].Capabilities = map[string]bool{"config.read": true}
				}
			}
		}
	}

	for _, tc := range []struct {
		name      string
		dbFile    string
		artifacts bool
	}{
		{name: "nano path", dbFile: "roots-access.sqlite3"},
		{name: "managed handler path (artifacts enabled)", dbFile: "roots-access-artifacts.sqlite3", artifacts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{},
				createSQLiteDBFile(t, tc.dbFile, true), configure(tc.artifacts))

			query := `query { gj_config(id: "current") { id } }`

			res, err := svc.gj.GraphQL(sourceModeUserTestContext(), query, nil, &core.RequestConfig{})
			if err == nil {
				t.Fatalf("expected role user to be denied gj_config via access.roots admin, got %s", string(res.Data))
			}
			if !strings.Contains(err.Error(), "blocked") {
				t.Fatalf("expected a blocked error for role user, got: %v", err)
			}

			res, err = svc.gj.GraphQL(sourceModeAdminTestContext(), query, nil, &core.RequestConfig{})
			if err != nil {
				t.Fatalf("expected admin to read gj_config with access.roots admin: %v", err)
			}
			// The nano path returns a singular object, the managed handler a
			// one-row list; either way the admin must see the current row.
			var out map[string]json.RawMessage
			if err := json.Unmarshal(res.Data, &out); err != nil {
				t.Fatalf("decode admin gj_config response: %v\n%s", err, string(res.Data))
			}
			if config := string(out["gj_config"]); config == "null" || !strings.Contains(config, `"current"`) {
				t.Fatalf("expected admin gj_config row, got %s", string(res.Data))
			}
		})
	}
}

// A roots override can also widen access: authenticated lets role "user" read
// a root that agentic mode otherwise reserves for admins.
func TestSourceRootsAccessAuthenticatedOverrideAllowsUser(t *testing.T) {
	svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{},
		createSQLiteDBFile(t, "roots-access-authenticated.sqlite3", true), func(conf *Config) {
			conf.Core.Mode = "agentic"
			for i := range conf.Core.Sources {
				if conf.Core.Sources[i].Kind == "graphjin" {
					conf.Core.Sources[i].Access.Roots = map[string]string{"gj_config": core.AccessModeAuthenticated}
					conf.Core.Sources[i].Capabilities = map[string]bool{"config.read": true}
				}
			}
		})

	res, err := svc.gj.GraphQL(sourceModeUserTestContext(), `query { gj_config(id: "current") { id } }`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("expected authenticated override to allow role user gj_config: %v", err)
	}
	var out struct {
		Config *struct {
			ID string `json:"id"`
		} `json:"gj_config"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode user gj_config response: %v\n%s", err, string(res.Data))
	}
	if out.Config == nil || out.Config.ID != "current" {
		t.Fatalf("expected user gj_config row with authenticated override, got %s", string(res.Data))
	}
}

func TestSourceRootsAccessReplacesGeneratedRuntimeDefaults(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{{
				Name: "graphjin",
				Kind: "graphjin",
				Access: core.SourceAccessConfig{
					Roots: map[string]string{"gj_config": core.AccessModeAdmin},
				},
				Capabilities: map[string]bool{"config.read": true},
			}},
		},
	}
	runtimeCore := &core.Config{
		Roles: []core.Role{{
			Name: "user",
			Tables: []core.RoleTable{{
				Name:      "gj_config",
				Database:  "graphjin",
				Generated: true,
				Query:     &core.Query{Block: false},
			}},
		}},
	}

	applySystemRoleQueryDefaults(conf, runtimeCore, "graphjin")

	rt, ok := roleTableFor(runtimeCore, "user", "gj_config", "graphjin")
	if !ok {
		t.Fatalf("expected generated gj_config role table")
	}
	if rt.Query == nil || !rt.Query.Block {
		t.Fatalf("expected access.roots admin to replace stale generated allow with block, got %+v", rt)
	}

	conf.Core.Roles = []core.Role{{
		Name: "user",
		Tables: []core.RoleTable{{
			Name:     "gj_config",
			Database: "graphjin",
			Query:    &core.Query{Block: false},
		}},
	}}
	runtimeCore = &core.Config{Roles: conf.Core.Roles}
	applySystemRoleQueryDefaults(conf, runtimeCore, "graphjin")
	rt, ok = roleTableFor(runtimeCore, "user", "gj_config", "graphjin")
	if !ok {
		t.Fatalf("expected explicit gj_config role table")
	}
	if rt.Generated || rt.Query == nil || rt.Query.Block {
		t.Fatalf("expected explicit role table to be preserved, got %+v", rt)
	}
}

func roleTableFor(conf *core.Config, role, table, database string) (core.RoleTable, bool) {
	for _, r := range conf.Roles {
		if !strings.EqualFold(r.Name, role) {
			continue
		}
		for _, rt := range r.Tables {
			if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
				continue
			}
			if strings.EqualFold(rt.Name, table) {
				return rt, true
			}
		}
	}
	return core.RoleTable{}, false
}
