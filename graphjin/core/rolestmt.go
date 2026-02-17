package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// nolint:errcheck
func (gj *graphjinEngine) prepareRoleStmt() error {
	if !gj.abacEnabled {
		return nil
	}

	if !strings.Contains(gj.conf.RolesQuery, "$user_id") {
		return fmt.Errorf("roles_query: $user_id variable missing")
	}

	pdb := gj.primaryDB()
	if pdb == nil || pdb.psqlCompiler == nil {
		return fmt.Errorf("roles_query: primary database not initialized")
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
