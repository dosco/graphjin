package core

import (
	"context"
	"fmt"
	"strings"
)

const (
	// RoleModeFirst applies the first configured role that matches the caller.
	RoleModeFirst = "first"
	// RoleModeUnion applies every configured role that matches the caller and
	// merges their table rules.
	RoleModeUnion = "union"

	// UnionRoleSeparator joins the component roles of a union role key.
	UnionRoleSeparator = "+"

	// groupsVar is the trusted identity variable that holds the caller's groups.
	groupsVar = "groups"
)

func normalizeRoleMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return RoleModeFirst
	}
	return mode
}

func (c *Config) roleUnionEnabled() bool {
	return c != nil && normalizeRoleMode(c.Identity.RoleMode) == RoleModeUnion
}

func (c *Config) validateRoleMode() error {
	switch normalizeRoleMode(c.Identity.RoleMode) {
	case RoleModeFirst:
		return nil
	case RoleModeUnion:
	default:
		return fmt.Errorf("identity.role_mode: unsupported value %q (supported: first, union)", c.Identity.RoleMode)
	}
	for _, role := range c.Roles {
		if strings.Contains(role.Name, UnionRoleSeparator) {
			return fmt.Errorf("identity.role_mode union: role name %q must not contain %q", role.Name, UnionRoleSeparator)
		}
	}
	rolesQuery := strings.TrimSpace(c.RolesQuery)
	if rolesQuery == "" {
		rolesQuery = strings.TrimSpace(c.Identity.Query)
	}
	if rolesQuery != "" && !isGraphQLRoleQuery(rolesQuery) {
		return fmt.Errorf("identity.role_mode union requires role claims or a GraphQL roles_query; a SQL roles_query can match only one role")
	}
	return nil
}

// unionRoleComponents splits a union role key into its component roles. A
// plain role name returns itself.
func unionRoleComponents(role string) []string {
	if !strings.Contains(role, UnionRoleSeparator) {
		return []string{role}
	}
	return strings.Split(role, UnionRoleSeparator)
}

// unionRoleKey returns the role key for a set of matched role names. It keeps
// configured roles only, drops anon, user and reserved roles, and orders the
// result by config order so the same set always yields the same key.
func (gj *graphjinEngine) unionRoleKey(matched []string) string {
	if gj == nil || gj.conf == nil || len(matched) == 0 {
		return ""
	}
	set := make(map[string]struct{}, len(matched))
	for _, role := range matched {
		set[strings.ToLower(strings.TrimSpace(role))] = struct{}{}
	}
	var names []string
	for _, role := range gj.conf.Roles {
		name := strings.TrimSpace(role.Name)
		lower := strings.ToLower(name)
		if lower == "anon" || lower == "user" || isReservedRoleName(name) {
			continue
		}
		if _, ok := set[lower]; ok {
			names = append(names, name)
		}
	}
	return strings.Join(names, UnionRoleSeparator)
}

// unionConfiguredRoles resolves role claims in union mode. A trusted reserved
// role still wins alone, as in first mode.
func (gj *graphjinEngine) unionConfiguredRoles(ctx context.Context, candidates []string) (string, bool) {
	if gj == nil || gj.conf == nil || len(candidates) == 0 {
		return "", false
	}
	var accepted []string
	hasUser := false
	for _, candidate := range candidates {
		role, trusted := gj.requestRole(ctx, candidate)
		if role == "" {
			continue
		}
		if trusted && gj.isConfiguredRole(role) {
			return gj.configuredRoleName(role), true
		}
		if strings.EqualFold(role, "user") {
			hasUser = true
		}
		accepted = append(accepted, role)
	}
	if key := gj.unionRoleKey(accepted); key != "" {
		return key, false
	}
	if hasUser {
		return "user", false
	}
	return "", false
}

func (gj *graphjinEngine) isConfiguredRole(role string) bool {
	return gj.configuredRoleName(role) != ""
}

func (gj *graphjinEngine) configuredRoleName(role string) string {
	for _, r := range gj.conf.Roles {
		if strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(role)) {
			return r.Name
		}
	}
	return ""
}

// roleByName returns the role config for a request role. In union mode a
// union key resolves to a synthetic role when every component is defined.
func (gj *graphjinEngine) roleByName(name string) (*Role, bool) {
	if r, ok := gj.roles[name]; ok {
		return r, true
	}
	if gj.conf == nil || !gj.conf.roleUnionEnabled() || !strings.Contains(name, UnionRoleSeparator) {
		return nil, false
	}
	if cached, ok := gj.unionRoles.Load(name); ok {
		return cached.(*Role), true
	}
	for _, component := range unionRoleComponents(name) {
		if component == "" || isReservedRoleName(component) {
			return nil, false
		}
		if _, ok := gj.roles[component]; !ok {
			return nil, false
		}
	}
	role := &Role{Name: name, tm: make(map[string]*RoleTable)}
	actual, _ := gj.unionRoles.LoadOrStore(name, role)
	return actual.(*Role), true
}

// roleMatchesList reports whether a role, any component of a union role, or
// any of the caller's groups appears in an allow list.
func roleMatchesList(role string, groups []string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	names := append(unionRoleComponents(strings.TrimSpace(role)), groups...)
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		for _, name := range names {
			if strings.EqualFold(item, strings.TrimSpace(name)) {
				return true
			}
		}
	}
	return false
}

// contextGroups returns the trusted groups of the caller.
func contextGroups(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	vars, ok := ctx.Value(IdentityVarsKey).(map[string]interface{})
	if !ok {
		return nil
	}
	return stringList(vars[groupsVar])
}

func stringList(v interface{}) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []interface{}:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(list) == "" {
			return nil
		}
		return []string{list}
	default:
		return nil
	}
}
