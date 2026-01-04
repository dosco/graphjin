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

	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	gj.psqlCompiler.RenderVar(w, &gj.roleStatementMetadata, gj.conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	// MSSQL uses TOP instead of LIMIT
	if gj.dbtype == "mssql" {
		io.WriteString(w, `(SELECT TOP 1 (CASE`)
	} else {
		io.WriteString(w, `(SELECT (CASE`)
	}
	for roleName, role := range gj.roles {
		if role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		match := role.Match
		// MSSQL uses 1/0 for boolean literals instead of true/false
		if gj.dbtype == "mssql" {
			match = strings.ReplaceAll(match, " true", " 1")
			match = strings.ReplaceAll(match, " false", " 0")
			match = strings.ReplaceAll(match, "=true", "=1")
			match = strings.ReplaceAll(match, "=false", "=0")
		}
		io.WriteString(w, match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, roleName)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE 'user' END) FROM (`)
	gj.psqlCompiler.RenderVar(w, &gj.roleStatementMetadata, gj.conf.RolesQuery)
	// MSSQL uses TOP instead of LIMIT
	if gj.dbtype == "mssql" {
		io.WriteString(w, `) AS _sg_auth_roles_query) `)
	} else {
		io.WriteString(w, `) AS _sg_auth_roles_query LIMIT 1) `)
	}

	switch gj.dbtype {
	case "mysql":
		io.WriteString(w, `ELSE 'anon' END) FROM (VALUES ROW(1)) AS _sg_auth_filler LIMIT 1; `)

	case "mssql":
		io.WriteString(w, `ELSE 'anon' END) FROM (SELECT 1 AS _sg_auth_filler) AS _sg_auth_filler; `)

	default:
		io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS _sg_auth_filler LIMIT 1; `)

	}
	gj.roleStatement = w.String()
	return nil
}
