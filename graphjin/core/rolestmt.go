package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/psql"
)

// nolint:errcheck
func (gj *graphjinEngine) prepareRoleStmt() error {
	if !gj.abacEnabled {
		return nil
	}

	if !strings.Contains(gj.conf.RolesQuery, "$user_id") {
		return fmt.Errorf("roles_query: $user_id variable missing")
	}

	if isGraphQLRoleQuery(gj.conf.RolesQuery) {
		gj.roleQueryMode = roleQueryGraphQL
		return gj.prepareGraphQLRoleStmt()
	}

	gj.roleQueryMode = roleQuerySQL

	pdb := gj.primaryDB()
	if pdb == nil || pdb.psqlCompiler == nil {
		return fmt.Errorf("roles_query: primary database not initialized")
	}

	if gj.conf.roleUnionEnabled() {
		gj.roleUnionRoles = gj.matchRoleNames()
		gj.roleStatement = renderRoleUnionStatement(pdb.psqlCompiler, &gj.roleStatementMetadata,
			gj.conf.RolesQuery, gj.roleUnionRoles, gj.roles)
		return nil
	}

	w := &bytes.Buffer{}
	dialect := pdb.psqlCompiler.GetDialect()

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	pdb.psqlCompiler.RenderVar(w, &gj.roleStatementMetadata, gj.conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	// Use dialect-specific SELECT prefix (e.g., MSSQL uses TOP instead of LIMIT)
	io.WriteString(w, dialect.RoleSelectPrefix())

	for roleName, role := range gj.roles {
		if role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		// Transform boolean literals using dialect (e.g., MSSQL uses 1/0 instead of true/false)
		match := dialect.TransformBooleanLiterals(role.Match)
		io.WriteString(w, match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, roleName)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE 'user' END) FROM (`)
	pdb.psqlCompiler.RenderVar(w, &gj.roleStatementMetadata, gj.conf.RolesQuery)
	// Use dialect-specific LIMIT suffix
	io.WriteString(w, dialect.RoleLimitSuffix())

	// Use dialect-specific dummy table syntax
	io.WriteString(w, dialect.RoleDummyTable())

	gj.roleStatement = w.String()
	return nil
}

// matchRoleNames returns the roles that have a match rule, in config order, so
// union role keys and statement columns stay stable across restarts.
func (gj *graphjinEngine) matchRoleNames() []string {
	var names []string
	for _, role := range gj.conf.Roles {
		if r, ok := gj.roles[role.Name]; ok && r.Match != "" {
			names = append(names, role.Name)
		}
	}
	return names
}

// renderRoleUnionStatement builds the union mode role statement. It reads the
// first row of the roles query and returns one column per role, 1 when the
// role's match rule is true and 0 otherwise. No row means the user is unknown.
// nolint:errcheck
func renderRoleUnionStatement(pc *psql.Compiler, md *psql.Metadata, rolesQuery string, names []string, roles map[string]*Role) string {
	w := &bytes.Buffer{}
	dialect := pc.GetDialect()

	io.WriteString(w, dialect.RoleUnionSelectPrefix())
	if len(names) == 0 {
		io.WriteString(w, `1`)
	}
	for i, name := range names {
		if i != 0 {
			io.WriteString(w, `, `)
		}
		io.WriteString(w, `(CASE WHEN `)
		io.WriteString(w, dialect.TransformBooleanLiterals(roles[name].Match))
		io.WriteString(w, ` THEN 1 ELSE 0 END)`)
	}
	io.WriteString(w, ` FROM (`)
	pc.RenderVar(w, md, rolesQuery)
	io.WriteString(w, dialect.RoleUnionFromSuffix())
	return w.String()
}
