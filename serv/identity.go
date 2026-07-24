package serv

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

func (s *graphjinService) observeAuthHandler(ah auth.HandlerFunc) auth.HandlerFunc {
	if ah == nil {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ctx, err := ah(w, r)
		if err != nil && s != nil && s.conf != nil && s.conf.Core.IsSourcesUsed() && strings.EqualFold(s.conf.Auth.Type, "jwt") {
			code := authFailureClass(err)
			s.recordRuntimeEvent(r.Context(), runtimeEvent{
				Phase:      "auth",
				Status:     runtimeStatusFailed,
				Severity:   "warn",
				Summary:    "JWT authentication did not produce a verified identity.",
				NextAction: "Send a valid bearer token with the configured issuer, audience, and identity claims.",
				ErrorCode:  code,
				Details:    map[string]any{"auth_type": "jwt", "reason": code},
			})
		}
		return ctx, err
	}
}

func authFailureClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no jwt token") || strings.Contains(msg, "authorization header") {
		return "jwt_missing"
	}
	return "jwt_invalid"
}

func (s *graphjinService) applyIdentityContext(ctx context.Context) context.Context {
	if ctx == nil || s == nil || s.conf == nil || !s.conf.Core.IsSourcesUsed() {
		return ctx
	}
	id := s.conf.Core.EffectiveIdentityConfig()
	claims := auth.UserClaims(ctx)
	vars := identityVarsFromContext(ctx)

	if userID, ok := claimValue(claims, id.UserIDClaim); ok {
		if ctx.Value(core.UserIDKey) == nil {
			ctx = context.WithValue(ctx, core.UserIDKey, userID)
		}
		vars["user_id"] = userID
		vars[id.UserIDClaim] = userID
	}
	if namespace, ok := claimValue(claims, id.NamespaceClaim); ok {
		vars["account_id"] = namespace
		vars[id.NamespaceClaim] = namespace
	} else if sourceModeNeedsNamespace(&s.conf.Core) && auth.IsAuth(ctx) {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "auth",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Required namespace claim is missing for account-scoped access.",
			NextAction: "Add the configured namespace claim to the JWT or change sources[].access for this source.",
			ErrorCode:  "identity_namespace_missing",
			Details:    map[string]any{"claim": id.NamespaceClaim},
		})
	}
	if userID := ctx.Value(core.UserIDKey); userID != nil {
		vars["user_ref"] = safeArtifactIdentity(fmt.Sprint(userID), false)
	}
	if accountID, ok := vars["account_id"]; ok && accountID != nil {
		vars["account_ref"] = safeArtifactIdentity(fmt.Sprint(accountID), false)
	}

	roles := extractClaimRoles(claims, id.RoleClaims)
	if len(roles) != 0 {
		ctx = context.WithValue(ctx, core.IdentityRolesKey, roles)
		if !s.identityRolesContainConfiguredRole(roles) {
			s.recordRuntimeEvent(ctx, runtimeEvent{
				Phase:      "auth",
				Status:     runtimeStatusReady,
				Severity:   "info",
				Summary:    "No JWT role matched a configured GraphJin role; falling back to role resolution.",
				NextAction: "Add a matching role to config or adjust identity.role_claims if this fallback is unexpected.",
				ErrorCode:  "identity_role_fallback",
				Details:    map[string]any{"candidate_count": len(roles)},
			})
		}
	}
	if len(vars) != 0 {
		ctx = context.WithValue(ctx, core.IdentityVarsKey, vars)
	}
	return ctx
}

func (s *graphjinService) identityRolesContainConfiguredRole(roles []string) bool {
	if s == nil || s.conf == nil {
		return false
	}
	seen := make(map[string]struct{})
	for _, role := range s.conf.Core.Roles {
		if strings.TrimSpace(role.Name) != "" {
			seen[strings.ToLower(strings.TrimSpace(role.Name))] = struct{}{}
		}
	}
	seen["user"] = struct{}{}
	seen["anon"] = struct{}{}
	for _, role := range s.conf.Core.EffectiveIdentityConfig().AdminRoles {
		seen[strings.ToLower(strings.TrimSpace(role))] = struct{}{}
	}
	for _, role := range roles {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(role))]; ok {
			return true
		}
	}
	return false
}

func identityVarsFromContext(ctx context.Context) map[string]interface{} {
	out := make(map[string]interface{})
	if ctx == nil {
		return out
	}
	if existing, ok := ctx.Value(core.IdentityVarsKey).(map[string]interface{}); ok {
		for k, v := range existing {
			out[k] = v
		}
	}
	return out
}

func claimValue(claims map[string]interface{}, name string) (interface{}, bool) {
	name = strings.TrimSpace(name)
	if claims == nil || name == "" {
		return nil, false
	}
	v, ok := claims[name]
	if !ok || v == nil {
		return nil, false
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, false
		}
		return t, true
	case fmt.Stringer:
		s := t.String()
		if strings.TrimSpace(s) == "" {
			return nil, false
		}
		return s, true
	default:
		return v, true
	}
}

func extractClaimRoles(claims map[string]interface{}, names []string) []string {
	if claims == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(role string) {
		role = strings.TrimSpace(role)
		if role == "" || core.IsReservedRoleName(role) {
			return
		}
		key := strings.ToLower(role)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	for _, name := range names {
		v, ok := claimValue(claims, name)
		if !ok {
			continue
		}
		switch roles := v.(type) {
		case string:
			for _, role := range strings.Split(roles, ",") {
				add(role)
			}
		case []string:
			for _, role := range roles {
				add(role)
			}
		case []interface{}:
			for _, role := range roles {
				if s, ok := role.(string); ok {
					add(s)
				}
			}
		}
	}
	return out
}

func sourceModeNeedsNamespace(conf *core.Config) bool {
	if conf == nil || !conf.IsSourcesUsed() {
		return false
	}
	for _, source := range conf.Sources {
		access := conf.EffectiveSourceAccess(source)
		if accessNeedsNamespace(access.Read) || accessNeedsNamespace(access.Write) || accessNeedsNamespace(access.Delete) {
			return true
		}
	}
	for _, mode := range conf.EffectiveSystemRootAccess() {
		if accessNeedsNamespace(mode) {
			return true
		}
	}
	return false
}

func accessNeedsNamespace(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), core.AccessModeAccount)
}

func (s *graphjinService) recordGraphQLAccessFailure(ctx context.Context, err error) {
	if err == nil || s == nil || s.conf == nil || !s.conf.Core.IsSourcesUsed() {
		return
	}
	reason := graphQLAccessFailureReason(err)
	if reason == "" {
		return
	}
	details := map[string]any{
		"reason": reason,
		"role":   runtimeRoleClass(ctx),
	}
	nextAction := "Check gj_security for the effective source/root policy and retry with the required identity or role."
	if key, value, ok := graphQLAccessFailureTarget(err); ok {
		details[key] = value
		if key == "root" {
			nextAction = sourceModeRootAccessNextAction
		}
	}
	s.recordRuntimeEvent(ctx, runtimeEvent{
		Phase:      "access",
		Status:     runtimeStatusFailed,
		Severity:   "warn",
		Summary:    "GraphQL request denied by source-mode access policy.",
		NextAction: nextAction,
		ErrorCode:  reason,
		Details:    details,
	})
}

const sourceModeRootAccessNextAction = `Check gj_security for the effective source/root policy and retry with the required role; if unsure, run query_catalog(search: "graphjin root access gj_security gj_config admin").`

func (s *graphjinService) recordGraphQLAccessFailures(ctx context.Context, query string, res *core.Result, err error) {
	if err != nil {
		s.recordGraphQLAccessFailure(ctx, err)
	}
	s.recordDeniedSourceModeRootAccess(ctx, query)
	if res == nil {
		return
	}
	for _, e := range res.Errors {
		if strings.TrimSpace(e.Message) == "" {
			continue
		}
		s.recordGraphQLAccessFailure(ctx, fmt.Errorf("%s", e.Message))
	}
}

func (s *graphjinService) recordDeniedSourceModeRootAccess(ctx context.Context, query string) {
	if s == nil || s.conf == nil || !s.conf.Core.IsSourcesUsed() || strings.TrimSpace(query) == "" {
		return
	}
	for _, root := range sourceModeRootsInQuery(query) {
		role := runtimeRoleClass(ctx)
		if s.sourceModeRootAccessAllowed(root, role) {
			continue
		}
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "GraphQL request denied by source-mode root access policy.",
			NextAction: sourceModeRootAccessNextAction,
			ErrorCode:  "access_unauthorized",
			Details: map[string]any{
				"reason": "access_unauthorized",
				"role":   role,
				"root":   root,
			},
		})
	}
}

func (s *graphjinService) sourceModeRootAccessAllowed(root, role string) bool {
	if s == nil || s.conf == nil {
		return true
	}
	return systemRootAllowed(s.conf, root, systemActionRead, role)
}

func sourceModeRootsInQuery(query string) []string {
	lower := strings.ToLower(query)
	roots := []string{"gj_security", "gj_runtime", "gj_config", "gj_artifacts", "gj_watch", "gj_watch_event", "gj_watch_flow_preview", "gj_workflow", "gj_workflow_execution", "gj_catalog"}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.Contains(lower, root) {
			out = append(out, root)
		}
	}
	return out
}

func graphQLAccessFailureReason(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "identity variable") && strings.Contains(msg, "required"):
		return "identity_variable_missing"
	case strings.Contains(msg, "requires authenticated"):
		return "authenticated_required"
	case strings.Contains(msg, "unauthorized"):
		return "access_unauthorized"
	case strings.Contains(msg, "blocked"):
		return "access_blocked"
	default:
		return ""
	}
}

func graphQLAccessFailureTarget(err error) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	msg := strings.ToLower(err.Error())
	if value, ok := betweenAccessFailureText(msg, "identity variable '", "' is required"); ok {
		return "identity_variable", value, true
	}
	if value, ok := betweenAccessFailureText(msg, "table: 'true' (", ") blocked"); ok {
		return "table_name", value, true
	}
	if value, ok := accessFailureNameAfter(msg, "query blocked: "); ok {
		return accessFailureRootOrTable(value)
	}
	if value, ok := accessFailureNameAfter(msg, "mutation blocked: "); ok {
		return accessFailureRootOrTable(value)
	}
	if value, ok := accessFailureNameAfter(msg, "unauthorized: table "); ok {
		return "table_name", value, true
	}
	if value, ok := accessFailureNameAfter(msg, "blocked: "); ok {
		return accessFailureRootOrTable(value)
	}
	return "", "", false
}

func accessFailureRootOrTable(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	if strings.HasPrefix(value, "gj_") {
		return "root", value, true
	}
	return "table_name", value, true
}

func accessFailureNameAfter(msg, marker string) (string, bool) {
	idx := strings.Index(msg, marker)
	if idx == -1 {
		return "", false
	}
	value := strings.TrimSpace(msg[idx+len(marker):])
	if value == "" {
		return "", false
	}
	for i, r := range value {
		if r == ' ' || r == '(' || r == ':' || r == ',' || r == ')' {
			if i == 0 {
				return "", false
			}
			return value[:i], true
		}
	}
	return value, true
}

func betweenAccessFailureText(msg, prefix, suffix string) (string, bool) {
	start := strings.Index(msg, prefix)
	if start == -1 {
		return "", false
	}
	start += len(prefix)
	rest := msg[start:]
	end := strings.Index(rest, suffix)
	if end == -1 {
		return "", false
	}
	value := strings.TrimSpace(rest[:end])
	return value, value != ""
}

func runtimeRoleClass(ctx context.Context) string {
	if ctx == nil {
		return "unknown"
	}
	if role, ok := ctx.Value(core.UserRoleKey).(string); ok && strings.TrimSpace(role) != "" {
		return strings.TrimSpace(role)
	}
	switch roles := ctx.Value(core.IdentityRolesKey).(type) {
	case []string:
		if len(roles) != 0 && strings.TrimSpace(roles[0]) != "" {
			return strings.TrimSpace(roles[0])
		}
	case string:
		if strings.TrimSpace(roles) != "" {
			return strings.TrimSpace(roles)
		}
	}
	if ctx.Value(core.UserIDKey) != nil {
		return "user"
	}
	return "anon"
}
