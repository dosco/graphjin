package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func unionEngine(mode string, roles ...string) *graphjinEngine {
	conf := &Config{Identity: IdentityConfig{RoleMode: mode}}
	gj := &graphjinEngine{conf: conf, roles: make(map[string]*Role)}
	for _, name := range roles {
		conf.Roles = append(conf.Roles, Role{Name: name})
	}
	for i := range conf.Roles {
		gj.roles[conf.Roles[i].Name] = &conf.Roles[i]
	}
	gj.roles["user"] = &Role{Name: "user"}
	gj.roles["anon"] = &Role{Name: "anon"}
	return gj
}

func TestValidateRoleMode(t *testing.T) {
	cases := []struct {
		name    string
		conf    Config
		wantErr string
	}{
		{name: "default first", conf: Config{}},
		{name: "explicit first with sql roles query", conf: Config{Identity: IdentityConfig{RoleMode: "first"}, RolesQuery: "SELECT * FROM users WHERE id = $user_id"}},
		{name: "union with claims", conf: Config{Identity: IdentityConfig{RoleMode: "union"}, Roles: []Role{{Name: "finance"}}}},
		{name: "union with graphql roles query", conf: Config{Identity: IdentityConfig{RoleMode: "Union"}, RolesQuery: "query { users(id: $user_id) { role } }"}},
		{name: "unknown mode", conf: Config{Identity: IdentityConfig{RoleMode: "all"}}, wantErr: "unsupported value"},
		{name: "separator in role name", conf: Config{Identity: IdentityConfig{RoleMode: "union"}, Roles: []Role{{Name: "a+b"}}}, wantErr: "must not contain"},
		{name: "union with sql roles query", conf: Config{Identity: IdentityConfig{RoleMode: "union"}, RolesQuery: "SELECT * FROM users WHERE id = $user_id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.conf.validateRoleMode()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRoleMode() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateRoleMode() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestUnionRoleKeyUsesConfigOrder(t *testing.T) {
	gj := unionEngine(RoleModeUnion, "finance", "sales", "support", "__internal")
	cases := map[string][]string{
		"finance+sales":         {"sales", "FINANCE"},
		"sales":                 {"sales", "unknown"},
		"":                      {"anon", "user", "__internal", "unknown"},
		"finance+sales+support": {"support", "sales", "finance", "sales"},
	}
	for want, matched := range cases {
		if got := gj.unionRoleKey(matched); got != want {
			t.Fatalf("unionRoleKey(%v) = %q, want %q", matched, got, want)
		}
	}
}

func TestInitialRequestRoleUnionMode(t *testing.T) {
	gj := unionEngine(RoleModeUnion, "finance", "sales")
	withRoles := func(roles ...string) context.Context {
		ctx := context.WithValue(context.Background(), UserIDKey, "u-1")
		return context.WithValue(ctx, IdentityRolesKey, roles)
	}

	if role, trusted := gj.initialRequestRole(withRoles("sales", "finance", "unknown")); role != "finance+sales" || trusted {
		t.Fatalf("union claims = %q, %v; want finance+sales, false", role, trusted)
	}
	if role, _ := gj.initialRequestRole(withRoles("sales")); role != "sales" {
		t.Fatalf("single claim = %q, want sales", role)
	}
	if role, _ := gj.initialRequestRole(withRoles("user", "unknown")); role != "user" {
		t.Fatalf("user claim = %q, want user", role)
	}
	if role, _ := gj.initialRequestRole(withRoles("unknown")); role != "user" {
		t.Fatalf("unknown claim = %q, want the authenticated default user", role)
	}

	gj.conf.Identity.RoleMode = RoleModeFirst
	if role, _ := gj.initialRequestRole(withRoles("sales", "finance")); role != "finance" {
		t.Fatalf("first mode = %q, want the first configured role finance", role)
	}
}

func TestInitialRequestRoleUnionTrustedReservedRoleWinsAlone(t *testing.T) {
	type trustedKey struct{}
	gj := unionEngine(RoleModeUnion, "finance", "__reserved_internal")
	gj.reservedRoleAuthorizer = func(ctx context.Context, role string) bool {
		return role == "__reserved_internal" && ctx.Value(trustedKey{}) == true
	}

	trusted := context.WithValue(context.Background(), trustedKey{}, true)
	trusted = context.WithValue(trusted, UserIDKey, "u-1")
	trusted = context.WithValue(trusted, IdentityRolesKey, []string{"finance", "__reserved_internal"})
	if role, ok := gj.initialRequestRole(trusted); role != "__reserved_internal" || !ok {
		t.Fatalf("trusted reserved role = %q, %v; want it alone and trusted", role, ok)
	}

	forged := context.WithValue(context.Background(), UserIDKey, "u-1")
	forged = context.WithValue(forged, IdentityRolesKey, []string{"finance", "__reserved_internal"})
	if role, ok := gj.initialRequestRole(forged); role != "finance" || ok {
		t.Fatalf("untrusted reserved role = %q, %v; want finance, false", role, ok)
	}
}

func TestRoleByNameUnionKey(t *testing.T) {
	gj := unionEngine(RoleModeUnion, "finance", "sales")

	r, ok := gj.roleByName("finance+sales")
	if !ok || r.Name != "finance+sales" {
		t.Fatalf("roleByName(finance+sales) = %+v, %v", r, ok)
	}
	if again, _ := gj.roleByName("finance+sales"); again != r {
		t.Fatal("union role config should be cached")
	}
	if _, ok := gj.roleByName("finance+unknown"); ok {
		t.Fatal("a union key with an undefined component must be rejected")
	}
	if _, ok := gj.roleByName("finance+__internal"); ok {
		t.Fatal("a union key with a reserved component must be rejected")
	}
	if r, ok := gj.roleByName("finance"); !ok || r.Name != "finance" {
		t.Fatal("plain roles must resolve as before")
	}

	gj.conf.Identity.RoleMode = RoleModeFirst
	gj.unionRoles.Delete("finance+sales")
	if _, ok := gj.roleByName("finance+sales"); ok {
		t.Fatal("first mode must not resolve union keys")
	}
}

func TestRoleMatchesList(t *testing.T) {
	cases := []struct {
		role    string
		groups  []string
		allowed []string
		want    bool
	}{
		{"operator", nil, []string{"operator"}, true},
		{"member", nil, []string{"operator"}, false},
		{"member+operator", nil, []string{"Operator"}, true},
		{"member", []string{"finance-leads"}, []string{"finance-leads"}, true},
		{"member", []string{"sales"}, []string{"finance-leads"}, false},
		{"operator", nil, nil, false},
	}
	for _, tc := range cases {
		if got := roleMatchesList(tc.role, tc.groups, tc.allowed); got != tc.want {
			t.Fatalf("roleMatchesList(%q, %v, %v) = %v, want %v", tc.role, tc.groups, tc.allowed, got, tc.want)
		}
	}
}

func TestOpenAPIAllowedRolesAcceptsGroupsAndUnionRoles(t *testing.T) {
	conf := Config{
		Sources: []SourceConfig{{
			Name: "external", Kind: sourcecap.KindAPI,
			Capabilities: map[string]bool{sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: true, sourcecap.KeyAPIDelete: true},
			Access:       SourceAccessConfig{Read: AccessModeAuthenticated, Write: AccessModeAuthenticated, Delete: AccessModeAdmin},
		}},
		Identity: IdentityConfig{AdminRoles: []string{"admin"}},
	}
	base := context.WithValue(context.Background(), UserIDKey, "u-1")
	withGroups := context.WithValue(base, IdentityVarsKey, map[string]interface{}{UserGroupsVar: []string{"finance-leads"}})
	write := &openapi.OpDescriptor{SourceName: "external", OperationID: "refund", Method: "POST", AllowedRoles: []string{"finance-leads"}}
	remove := &openapi.OpDescriptor{SourceName: "external", OperationID: "purge", Method: "DELETE", AllowedRoles: []string{"admin"}}

	if d := conf.authorizeOpenAPIOperation(withGroups, write, "member"); !d.Allowed {
		t.Fatalf("group in allowed_roles should permit the write: %+v", d)
	}
	if d := conf.authorizeOpenAPIOperation(base, write, "member"); d.Allowed || d.Gate != "allowed_roles" {
		t.Fatalf("caller without the group must be denied at allowed_roles: %+v", d)
	}
	if d := conf.authorizeOpenAPIOperation(base, remove, "member+admin"); !d.Allowed {
		t.Fatalf("a union role with an admin component should pass admin access: %+v", d)
	}
	groupAdmin := context.WithValue(base, IdentityVarsKey, map[string]interface{}{UserGroupsVar: []string{"admin"}})
	if d := conf.authorizeOpenAPIOperation(groupAdmin, remove, "member"); d.Allowed || d.Gate != "access" {
		t.Fatalf("a group named like an admin role must not grant admin access: %+v", d)
	}
}

func TestGroupsArgValue(t *testing.T) {
	v, err := groupsArgValue(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := v.(json.RawMessage); !ok || string(raw) != "[]" {
		t.Fatalf("missing groups = %#v, want an empty JSON array", v)
	}

	ctx := context.WithValue(context.Background(), IdentityVarsKey, map[string]interface{}{
		UserGroupsVar: []interface{}{"finance", "sales"},
	})
	v, err = groupsArgValue(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := v.(json.RawMessage); !ok || string(raw) != `["finance","sales"]` {
		t.Fatalf("groups = %#v, want a JSON array", v)
	}
}

func TestGroupsVariableIsTrustedIdentity(t *testing.T) {
	gj := &graphjinEngine{conf: &Config{Sources: []SourceConfig{{Name: "app", Kind: "database"}}}}
	if err := gj.conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	if !gj.sourceModeTrustedIdentityParam("user_groups") {
		t.Fatal("$user_groups must be a trusted identity variable in sources mode")
	}
	s := &gstate{gj: gj}
	if name, trusted := s.nanoTrustedVarName("user_groups"); name != "user_groups" || !trusted {
		t.Fatalf("nanodb must treat $user_groups as trusted, got %q %v", name, trusted)
	}
}

func TestConfigValidateRunsRoleModeValidation(t *testing.T) {
	conf := &Config{
		DBType:   "postgres",
		Identity: IdentityConfig{RoleMode: RoleModeUnion},
		Roles:    []Role{{Name: "finance+sales"}},
	}
	if err := conf.Validate(); err == nil || !strings.Contains(err.Error(), "must not contain") {
		t.Fatalf("Validate() = %v, want the role name error", err)
	}
}

func TestRenderRoleUnionStatementPerDialect(t *testing.T) {
	roles := map[string]*Role{
		"finance": {Name: "finance", Match: "id = 1"},
		"blocked": {Name: "blocked", Match: "disabled = true"},
	}
	names := []string{"finance", "blocked"}
	const cols = "(CASE WHEN id = 1 THEN 1 ELSE 0 END), (CASE WHEN disabled = true THEN 1 ELSE 0 END)"
	want := map[string]string{
		"postgres": "SELECT " + cols + " FROM (SELECT * FROM users WHERE id = $1) AS _sg_auth_roles_query LIMIT 1",
		"mysql":    "SELECT " + cols + " FROM (SELECT * FROM users WHERE id = ?) AS _sg_auth_roles_query LIMIT 1",
		"mariadb":  "SELECT " + cols + " FROM (SELECT * FROM users WHERE id = ?) AS _sg_auth_roles_query LIMIT 1",
		"sqlite":   "SELECT " + cols + " FROM (SELECT * FROM users WHERE id = ?) AS _sg_auth_roles_query LIMIT 1",
		"mssql":    "SELECT TOP 1 (CASE WHEN id = 1 THEN 1 ELSE 0 END), (CASE WHEN disabled = 1 THEN 1 ELSE 0 END) FROM (SELECT * FROM users WHERE id = @p1) AS _sg_auth_roles_query",
		"oracle":   "SELECT " + cols + ` FROM (SELECT * FROM users WHERE id = :1) "_SG_AUTH_ROLES_QUERY" FETCH FIRST 1 ROWS ONLY`,
	}
	for db, expected := range want {
		var md psql.Metadata
		pc := psql.NewCompiler(psql.Config{DBType: db})
		got := renderRoleUnionStatement(pc, &md, "SELECT * FROM users WHERE id = $user_id", names, roles)
		if got != expected {
			t.Fatalf("%s union role statement:\n got: %s\nwant: %s", db, got, expected)
		}
		if params := md.Params(); len(params) != 1 || params[0].Name != "user_id" {
			t.Fatalf("%s union role statement params = %+v, want user_id once", db, params)
		}
	}

	var md psql.Metadata
	got := renderRoleUnionStatement(psql.NewCompiler(psql.Config{DBType: "postgres"}), &md,
		"SELECT * FROM users WHERE id = $user_id", nil, roles)
	if got != "SELECT 1 FROM (SELECT * FROM users WHERE id = $1) AS _sg_auth_roles_query LIMIT 1" {
		t.Fatalf("statement without match rules = %s", got)
	}
}

func TestScanRoleUnionRowOutcomes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, dept TEXT)`,
		`INSERT INTO users (id, dept) VALUES (1, 'finance'), (2, 'none')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	gj := unionEngine(RoleModeUnion, "finance", "auditor")
	gj.roles["finance"].Match = "dept = 'finance'"
	gj.roles["auditor"].Match = "id = 1"
	gj.roleUnionRoles = gj.matchRoleNames()

	var md psql.Metadata
	query := renderRoleUnionStatement(psql.NewCompiler(psql.Config{DBType: "sqlite"}), &md,
		"SELECT * FROM users WHERE id = $user_id", gj.roleUnionRoles, gj.roles)

	cases := map[int]string{1: "finance+auditor", 2: "user", 3: "anon"}
	for userID, want := range cases {
		got, err := gj.scanRoleUnionRow(ctx, "sqlite", conn, nil, query, []interface{}{userID})
		if err != nil {
			t.Fatalf("user %d: %v", userID, err)
		}
		if got != want {
			t.Fatalf("user %d resolved to %q, want %q", userID, got, want)
		}
	}
}
