package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// LegacyOpenAPISourceName is the server-owned provenance label used only for
// pre-sources OpenAPI configuration. It preserves existing GET behavior while
// keeping write/delete access blocked; source-mode operations always use their
// actual sources[].name.
const LegacyOpenAPISourceName = "legacy_openapi"

// OpenAPIAuthorizationDecision is the precomputed explanation for an API
// operation allow/deny result. Execution uses this decision directly and
// security/discovery adapters can surface the same gate without duplicating
// policy logic.
type OpenAPIAuthorizationDecision struct {
	Allowed    bool
	SourceName string
	Capability string
	AccessMode string
	Gate       string
	Reason     string
}

func (c *Config) effectiveSourceCapability(source SourceConfig, key string) bool {
	if value, explicit := source.Capability(key); explicit {
		return value
	}
	def, ok := sourcecap.Lookup(source.CanonicalKind(), key)
	return ok && def.Default(c.modeForSourceDefaults())
}

func (c *Config) authorizeOpenAPIOperation(ctx context.Context, op *openapi.OpDescriptor, role string) OpenAPIAuthorizationDecision {
	d := OpenAPIAuthorizationDecision{Gate: "operation", Reason: "operation metadata is missing"}
	if c == nil || op == nil {
		return d
	}
	d.SourceName = op.SourceName
	source, ok := c.OpenAPISourceByName(op.SourceName)
	if !ok || source.CanonicalKind() != sourcecap.KindAPI {
		d.Gate = "source"
		d.Reason = fmt.Sprintf("owning api source %q is not configured", op.SourceName)
		return d
	}

	method := strings.ToUpper(strings.TrimSpace(op.Method))
	access := c.EffectiveSourceAccess(source)
	switch method {
	case "GET":
		d.Capability = sourcecap.KeyAPIRead
		d.AccessMode = access.Read
	case "POST", "PUT", "PATCH":
		d.Capability = sourcecap.KeyAPIWrite
		d.AccessMode = access.Write
	case "DELETE":
		d.Capability = sourcecap.KeyAPIDelete
		d.AccessMode = access.Delete
	default:
		d.Gate = "method"
		d.Reason = fmt.Sprintf("HTTP method %s is not supported", method)
		return d
	}

	if method != "GET" && source.ReadOnly {
		d.Gate = "source.read_only"
		d.Reason = fmt.Sprintf("api source %q is read-only", source.Name)
		return d
	}
	if !c.effectiveSourceCapability(source, d.Capability) {
		d.Gate = "capability"
		d.Reason = fmt.Sprintf("source capability %s is disabled", d.Capability)
		return d
	}
	if !apiSourceAccessAllowed(ctx, c, d.AccessMode, role) {
		d.Gate = "access"
		d.Reason = fmt.Sprintf("source access.%s blocks role %q", sourcecapAction(d.Capability), role)
		return d
	}
	if method != "GET" && !roleInList(role, op.AllowedRoles) {
		d.Gate = "allowed_roles"
		d.Reason = fmt.Sprintf("role %q is not allowlisted for operation %q", role, op.OperationID)
		return d
	}

	d.Allowed = true
	d.Gate = "allowed"
	d.Reason = "source capability, access policy, and operation role allowlist permit execution"
	return d
}

// AuthorizeOpenAPIOperation exposes the same runtime decision to embedding
// layers that need caller-scoped discovery. Execution still repeats the check.
func (c *Config) AuthorizeOpenAPIOperation(ctx context.Context, op *openapi.OpDescriptor, role string) OpenAPIAuthorizationDecision {
	return c.authorizeOpenAPIOperation(ctx, op, role)
}

// OpenAPISourceByName resolves a configured API source or the server-owned
// compatibility source used by legacy top-level OpenAPI GET configuration.
func (c *Config) OpenAPISourceByName(name string) (SourceConfig, bool) {
	if source, ok := c.SourceByName(name); ok {
		return source, source.CanonicalKind() == sourcecap.KindAPI
	}
	if c != nil && !c.IsSourcesUsed() && name == LegacyOpenAPISourceName {
		return SourceConfig{
			Name:         LegacyOpenAPISourceName,
			Kind:         sourcecap.KindAPI,
			Capabilities: map[string]bool{sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: false, sourcecap.KeyAPIDelete: false},
			Access:       SourceAccessConfig{Read: AccessModePublic, Write: AccessModeBlocked, Delete: AccessModeBlocked},
			ReadOnly:     true,
		}, true
	}
	return SourceConfig{}, false
}

func sourcecapAction(capability string) string {
	switch capability {
	case sourcecap.KeyAPIWrite:
		return "write"
	case sourcecap.KeyAPIDelete:
		return "delete"
	default:
		return "read"
	}
}

func roleInList(role string, allowed []string) bool {
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(role)) {
			return true
		}
	}
	return false
}

func apiSourceAccessAllowed(ctx context.Context, c *Config, mode, role string) bool {
	mode = normalizeAccessMode(mode)
	authenticated := strings.TrimSpace(role) != "" && !strings.EqualFold(role, "anon")
	switch mode {
	case AccessModePublic:
		return true
	case AccessModeAuthenticated:
		return authenticated
	case AccessModeAdmin:
		return roleInList(role, c.EffectiveIdentityConfig().AdminRoles)
	case AccessModeAccount:
		if !authenticated || ctx == nil {
			return false
		}
		vars, _ := ctx.Value(IdentityVarsKey).(map[string]interface{})
		return identityValuePresent(vars[c.EffectiveIdentityConfig().NamespaceClaim])
	case AccessModeOwner:
		return authenticated && ctx != nil && identityValuePresent(ctx.Value(UserIDKey))
	default:
		return false
	}
}
