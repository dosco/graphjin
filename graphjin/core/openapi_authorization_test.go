package core

import (
	"context"
	"testing"

	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestOpenAPIAuthorizationGateMatrix(t *testing.T) {
	baseSource := SourceConfig{
		Name: "external", Kind: sourcecap.KindAPI,
		Capabilities: map[string]bool{sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: true, sourcecap.KeyAPIDelete: true},
		Access:       SourceAccessConfig{Read: AccessModeAuthenticated, Write: AccessModeAuthenticated, Delete: AccessModeAdmin},
	}
	conf := Config{Sources: []SourceConfig{baseSource}, Identity: IdentityConfig{AdminRoles: []string{"admin"}}}
	ctx := context.WithValue(context.Background(), UserIDKey, "u-1")

	tests := []struct {
		name       string
		method     string
		role       string
		allowed    []string
		mutateConf func(*Config)
		want       bool
		gate       string
	}{
		{"read allowed", "GET", "member", nil, nil, true, "allowed"},
		{"anonymous read denied", "GET", "anon", nil, nil, false, "access"},
		{"read capability disabled", "GET", "member", nil, func(c *Config) { c.Sources[0].Capabilities[sourcecap.KeyAPIRead] = false }, false, "capability"},
		{"write allowed", "POST", "operator", []string{"operator"}, nil, true, "allowed"},
		{"write role denied", "PATCH", "member", []string{"operator"}, nil, false, "allowed_roles"},
		{"write access blocked", "PUT", "operator", []string{"operator"}, func(c *Config) { c.Sources[0].Access.Write = AccessModeBlocked }, false, "access"},
		{"read only veto", "POST", "operator", []string{"operator"}, func(c *Config) { c.Sources[0].ReadOnly = true }, false, "source.read_only"},
		{"delete admin allowed", "DELETE", "admin", []string{"admin"}, nil, true, "allowed"},
		{"delete access denied", "DELETE", "operator", []string{"operator"}, nil, false, "access"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := conf
			candidate.Sources = append([]SourceConfig(nil), conf.Sources...)
			candidate.Sources[0].Capabilities = map[string]bool{}
			for key, value := range conf.Sources[0].Capabilities {
				candidate.Sources[0].Capabilities[key] = value
			}
			if tc.mutateConf != nil {
				tc.mutateConf(&candidate)
			}
			op := &openapi.OpDescriptor{SourceName: "external", OperationID: "contract", Method: tc.method, AllowedRoles: tc.allowed}
			decision := candidate.authorizeOpenAPIOperation(ctx, op, tc.role)
			if decision.Allowed != tc.want || decision.Gate != tc.gate {
				t.Fatalf("decision = %+v, want allowed=%v gate=%s", decision, tc.want, tc.gate)
			}
		})
	}
}

func TestOpenAPIAuthorizationSafeDefaults(t *testing.T) {
	source := SourceConfig{Name: "external", Kind: sourcecap.KindAPI}
	conf := Config{Mode: sourcecap.ModeProd, Sources: []SourceConfig{source}}
	access := conf.EffectiveSourceAccess(source)
	if access.Read != AccessModeAuthenticated || access.Write != AccessModeBlocked || access.Delete != AccessModeBlocked {
		t.Fatalf("API access defaults = %+v", access)
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		op := &openapi.OpDescriptor{SourceName: "external", OperationID: "write", Method: method, AllowedRoles: []string{"operator"}}
		decision := conf.authorizeOpenAPIOperation(context.Background(), op, "operator")
		if decision.Allowed {
			t.Fatalf("%s unexpectedly allowed: %+v", method, decision)
		}
	}
}

func TestLegacyOpenAPICompatibilitySourceAllowsOnlyReads(t *testing.T) {
	conf := Config{}
	read := conf.authorizeOpenAPIOperation(context.Background(), &openapi.OpDescriptor{
		SourceName: LegacyOpenAPISourceName, OperationID: "legacyGet", Method: "GET",
	}, "anon")
	if !read.Allowed {
		t.Fatalf("legacy GET should preserve existing behavior: %+v", read)
	}
	write := conf.authorizeOpenAPIOperation(context.Background(), &openapi.OpDescriptor{
		SourceName: LegacyOpenAPISourceName, OperationID: "legacyPost", Method: "POST", AllowedRoles: []string{"admin"},
	}, "admin")
	if write.Allowed || write.Gate != "source.read_only" {
		t.Fatalf("legacy OpenAPI write should remain blocked: %+v", write)
	}
}

func TestOpenAPISourceProvenanceNormalizeCloneAndDuplicate(t *testing.T) {
	conf := Config{Sources: []SourceConfig{
		{Name: "crm", Kind: sourcecap.KindAPI, Specs: map[string]openapi.SpecConfig{"crm_spec": {Operations: map[string]openapi.OperationOverride{"update": {AllowedRoles: []string{"operator"}}}}}},
		{Name: "billing", Kind: sourcecap.KindAPI, Specs: map[string]openapi.SpecConfig{"billing_spec": {}}},
	}}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if conf.OpenAPI["crm_spec"].SourceName != "crm" || conf.OpenAPI["billing_spec"].SourceName != "billing" {
		t.Fatalf("provenance lost: %+v", conf.OpenAPI)
	}
	cloned := conf.clone()
	if cloned.OpenAPI["crm_spec"].SourceName != "crm" || cloned.Sources[0].Specs["crm_spec"].Operations["update"].AllowedRoles[0] != "operator" {
		t.Fatalf("clone lost provenance: %+v", cloned.OpenAPI)
	}
	clonedSpec := cloned.OpenAPI["crm_spec"]
	clonedOverride := clonedSpec.Operations["update"]
	clonedOverride.AllowedRoles[0] = "changed"
	clonedSpec.Operations["update"] = clonedOverride
	cloned.OpenAPI["crm_spec"] = clonedSpec
	if conf.OpenAPI["crm_spec"].Operations["update"].AllowedRoles[0] != "operator" {
		t.Fatal("clone shares mutable OpenAPI operation policy with original")
	}

	dup := Config{Sources: []SourceConfig{
		{Name: "one", Kind: sourcecap.KindAPI, Specs: map[string]openapi.SpecConfig{"shared": {}}},
		{Name: "two", Kind: sourcecap.KindAPI, Specs: map[string]openapi.SpecConfig{"shared": {}}},
	}}
	if err := dup.NormalizeSources(); err == nil {
		t.Fatal("expected duplicate OpenAPI spec key error")
	}
}

func TestProductionOpenAPIMutationRequiresRoleAllowlist(t *testing.T) {
	for _, roles := range [][]string{nil, {}, {" ", "\t"}} {
		conf := Config{Mode: sourcecap.ModeProd, Sources: []SourceConfig{{
			Name: "external", Kind: sourcecap.KindAPI,
			Specs: map[string]openapi.SpecConfig{"contract": {Operations: map[string]openapi.OperationOverride{
				"create": {ExposeMutation: true, AllowedRoles: roles},
			}}},
		}}}
		if err := conf.ValidateIsSourcesUsed(); err == nil {
			t.Fatalf("expected production mutation with allowed_roles=%q to fail validation", roles)
		}
	}
}
