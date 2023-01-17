package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// nolint:errcheck
func (gj *graphjin) prepareRoleStmt() error {
	if !gj.abacEnabled {
		return nil
	}

	if !strings.Contains(gj.conf.RolesQuery, "$user_id") {
		return fmt.Errorf("roles_query: $user_id variable missing")
	}

	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	gj.pc.RenderVar(w, &gj.roleStmtMD, gj.conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	io.WriteString(w, `(SELECT (CASE`)
	for roleName, role := range gj.roles {
		if role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, roleName)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE 'user' END) FROM (`)
	gj.pc.RenderVar(w, &gj.roleStmtMD, gj.conf.RolesQuery)
	io.WriteString(w, `) AS _sg_auth_roles_query LIMIT 1) `)

	switch gj.dbtype {
	case "mysql":
		io.WriteString(w, `ELSE 'anon' END) FROM (VALUES ROW(1)) AS _sg_auth_filler LIMIT 1; `)

	default:
		io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS _sg_auth_filler LIMIT 1; `)

	}
	gj.roleStmt = w.String()
	return nil
}
