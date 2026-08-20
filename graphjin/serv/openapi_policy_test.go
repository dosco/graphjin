package serv

import (
	"context"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestPreserveProtectedReadOnlyAPISource(t *testing.T) {
	sources := []core.SourceConfig{
		{Name: "  PROTECTED_API  ", Kind: sourcecap.KindAPI, ReadOnly: false},
		{Name: "new_api", Kind: sourcecap.KindAPI, ReadOnly: false},
	}
	changes := preserveProtectedReadOnlySources(sources, map[string]bool{"protected_api": true})
	if !sources[0].ReadOnly || sources[1].ReadOnly || len(changes) != 1 {
		t.Fatalf("sources=%+v changes=%+v", sources, changes)
	}
}

func TestStartupReadOnlySourcePolicySurvivesLaterConfigChanges(t *testing.T) {
	conf := &Config{Core: core.Config{Sources: []core.SourceConfig{{Name: "Protected_API", Kind: sourcecap.KindAPI, ReadOnly: true}}}}
	svc := &graphjinService{conf: conf}
	svc.captureStartupReadOnlyPolicy()
	svc.conf.Core.Sources[0].ReadOnly = false
	svc.captureStartupReadOnlyPolicy()

	policy := cloneReadOnlyPolicy(svc.startupReadOnlySources)
	if !policy["protected_api"] {
		t.Fatalf("startup policy was lost: %+v", policy)
	}
	sources := append([]core.SourceConfig(nil), svc.conf.Core.Sources...)
	sources[0].Name = "protected_api"
	preserveProtectedReadOnlySources(sources, policy)
	if !sources[0].ReadOnly {
		t.Fatal("a later MCP transport could make the protected API source writable")
	}
}

func TestCloneCoreConfigKeepsOpenAPIProvenanceAndOwnership(t *testing.T) {
	contract := openapi.SpecConfig{
		SourceName: "api",
		Auth: openapi.AuthConfig{
			Scopes: []string{"write"},
			Request: &openapi.TokenExchangeRequest{
				Body: map[string]interface{}{"secret": "value"}, Headers: map[string]string{"X-Secret": "value"},
			},
		},
		Operations: map[string]openapi.OperationOverride{"create": {
			ExposeMutation: true, AllowedRoles: []string{"operator"}, Defaults: map[string]string{"store": "default"},
		}},
	}
	src := core.Config{
		OpenAPI: map[string]openapi.SpecConfig{"legacy": contract},
		Sources: []core.SourceConfig{{
			Name: "api", Kind: sourcecap.KindAPI,
			Access: core.SourceAccessConfig{PublicTables: []string{"reference"}},
			Specs:  map[string]openapi.SpecConfig{"contract": contract},
		}},
	}
	clone := cloneCoreConfig(src)
	spec := clone.Sources[0].Specs["contract"]
	if spec.SourceName != "api" {
		t.Fatalf("source provenance = %q", spec.SourceName)
	}
	op := spec.Operations["create"]
	op.AllowedRoles[0] = "changed"
	op.Defaults["store"] = "changed"
	spec.Operations["create"] = op
	clone.Sources[0].Specs["contract"] = spec
	clone.Sources[0].Access.PublicTables[0] = "changed"
	legacy := clone.OpenAPI["legacy"]
	legacy.Auth.Scopes[0] = "changed"
	legacy.Auth.Request.Body["secret"] = "changed"
	legacy.Auth.Request.Headers["X-Secret"] = "changed"
	original := src.Sources[0].Specs["contract"].Operations["create"]
	if original.AllowedRoles[0] != "operator" || original.Defaults["store"] != "default" || src.Sources[0].Access.PublicTables[0] != "reference" ||
		src.OpenAPI["legacy"].Auth.Scopes[0] != "write" || src.OpenAPI["legacy"].Auth.Request.Body["secret"] != "value" || src.OpenAPI["legacy"].Auth.Request.Headers["X-Secret"] != "value" {
		t.Fatal("clone aliases mutable OpenAPI/source access state")
	}
}

func TestCatalogOpenAPIMutationsAreCallerScoped(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode:     sourcecap.ModeAgentic,
			Identity: core.IdentityConfig{AdminRoles: []string{"admin"}},
			Sources: []core.SourceConfig{{
				Name: "api", Kind: sourcecap.KindAPI,
				Capabilities: map[string]bool{sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: true},
				Access:       core.SourceAccessConfig{Read: core.AccessModeAuthenticated, Write: core.AccessModeAuthenticated},
			}},
		},
		Serv: Serv{MCP: MCPConfig{AllowMutations: true}},
	}
	s := &graphjinService{conf: conf}
	md := &core.MetadataSnapshot{APIOperations: []core.MetadataAPIOperation{
		{ID: "api:spec:get", SourceName: "api", OperationID: "get", RootName: "get_item", Method: "GET", Active: true},
		{ID: "api:spec:create", SourceName: "api", OperationID: "create", RootName: "create_item", Method: "POST", Active: true, AllowedRoles: []string{"operator"}},
		{ID: "api:spec:skipped", SourceName: "api", OperationID: "skipped", Method: "POST", Active: false, SkipReason: "disabled"},
	}}

	operator := context.WithValue(context.Background(), core.UserRoleKey, "operator")
	operator = context.WithValue(operator, core.UserIDKey, "u-1")
	visible := s.filterCatalogAPIOperationsForContext(operator, md)
	if len(visible.APIOperations) != 2 {
		t.Fatalf("operator operations = %+v", visible.APIOperations)
	}

	member := context.WithValue(context.Background(), core.UserRoleKey, "member")
	member = context.WithValue(member, core.UserIDKey, "u-2")
	visible = s.filterCatalogAPIOperationsForContext(member, md)
	if len(visible.APIOperations) != 1 || visible.APIOperations[0].Method != "GET" {
		t.Fatalf("member operations = %+v", visible.APIOperations)
	}

	conf.Core.Sources[0].Access.Write = core.AccessModeAccount
	visible = s.filterCatalogAPIOperationsForContext(operator, md)
	if len(visible.APIOperations) != 1 || visible.APIOperations[0].Method != "GET" {
		t.Fatalf("account-scoped operation leaked without account identity: %+v", visible.APIOperations)
	}
	accountOperator := context.WithValue(operator, core.IdentityVarsKey, map[string]interface{}{"account_id": "acct-1"})
	visible = s.filterCatalogAPIOperationsForContext(accountOperator, md)
	if len(visible.APIOperations) != 2 {
		t.Fatalf("account-scoped operator operations = %+v", visible.APIOperations)
	}
	conf.Core.Sources[0].Access.Write = core.AccessModeAuthenticated

	conf.Agent.ReadOnly = true
	visible = s.filterCatalogAPIOperationsForContext(operator, md)
	if len(visible.APIOperations) != 1 || visible.APIOperations[0].Method != "GET" {
		t.Fatalf("read-only agent operations = %+v", visible.APIOperations)
	}

	admin := context.WithValue(context.Background(), core.UserRoleKey, "admin")
	admin = context.WithValue(admin, core.UserIDKey, "u-3")
	visible = s.filterCatalogAPIOperationsForContext(admin, md)
	if len(visible.APIOperations) != 2 { // GET plus skipped; active POST allows operator only.
		t.Fatalf("admin operations = %+v", visible.APIOperations)
	}
}

func TestLegacyOpenAPICatalogPreservesGETButHidesWrites(t *testing.T) {
	s := &graphjinService{conf: &Config{
		Core: core.Config{OpenAPI: map[string]openapi.SpecConfig{"legacy": {}}},
		Serv: Serv{MCP: MCPConfig{AllowMutations: true}},
	}}
	md := &core.MetadataSnapshot{APIOperations: []core.MetadataAPIOperation{
		{ID: "legacy:get", SourceName: core.LegacyOpenAPISourceName, OperationID: "get", Method: "GET", Active: true},
		{ID: "legacy:post", SourceName: core.LegacyOpenAPISourceName, OperationID: "post", Method: "POST", Active: true, AllowedRoles: []string{"admin"}},
	}}
	visible := s.filterCatalogAPIOperationsForContext(context.Background(), md)
	if len(visible.APIOperations) != 1 || visible.APIOperations[0].Method != "GET" {
		t.Fatalf("legacy operations = %+v", visible.APIOperations)
	}
}

func TestSecurityOpenAPIOperationPoliciesExplainEffectiveGates(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: sourcecap.ModeAgentic,
			Sources: []core.SourceConfig{{
				Name: "api", Kind: sourcecap.KindAPI,
				Capabilities: map[string]bool{
					sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: true, sourcecap.KeyAPIDelete: false,
				},
				Access: core.SourceAccessConfig{
					Read: core.AccessModeAuthenticated, Write: core.AccessModeAdmin, Delete: core.AccessModeBlocked,
				},
			}},
		},
		Serv: Serv{MCP: MCPConfig{AllowMutations: true}},
	}
	rows := securityOpenAPIOperationPolicies(conf, modeAgentic, []core.MetadataAPIOperation{
		{SourceName: "api", SpecKey: "store", OperationID: "getItem", RootName: "get_item", Method: "GET", Active: true},
		{SourceName: "api", SpecKey: "store", OperationID: "createItem", RootName: "create_item", Method: "POST", Active: true, AllowedRoles: []string{"admin"}, RetryEnabled: true},
		{SourceName: "api", SpecKey: "store", OperationID: "deleteItem", RootName: "delete_item", Method: "DELETE", Active: true, AllowedRoles: []string{"admin"}},
		{SourceName: "api", SpecKey: "store", OperationID: "unsafeWrite", Method: "PATCH", Active: false, SkipReason: "expose_mutation is false"},
	})
	if len(rows) != 4 {
		t.Fatalf("operation policies = %+v", rows)
	}
	byAction := make(map[string]securityPolicyEval, len(rows))
	for _, row := range rows {
		byAction[row.Title] = row
	}
	if row := byAction["GET getItem"]; !row.EffectiveAllowed || row.Capability != sourcecap.KeyAPIRead {
		t.Fatalf("read posture = %+v", row)
	}
	if row := byAction["POST createItem"]; !row.EffectiveAllowed || row.Details["blocked_by"] != "caller_identity" || row.Details["retry_on_auth_failure"] != true {
		t.Fatalf("write posture = %+v", row)
	}
	if row := byAction["DELETE deleteItem"]; row.EffectiveAllowed || row.Details["blocked_by"] != "capability" {
		t.Fatalf("delete posture = %+v", row)
	}
	if row := byAction["PATCH unsafeWrite"]; row.EffectiveAllowed || row.Details["blocked_by"] != "classification" || row.Details["skip_reason"] == "" {
		t.Fatalf("skipped posture = %+v", row)
	}
}
