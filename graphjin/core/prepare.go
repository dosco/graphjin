package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/qcode"
)

type cquery struct {
	sync.Once
	q       rquery
	stmts   []stmt
	st      stmt
	roleArg bool
}

type rquery struct {
	op    qcode.QType
	name  string
	query []byte
	vars  []byte
}

// nolint: errcheck
func (gj *GraphJin) prepareRoleStmt() error {
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
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	gj.roleStmt = w.String()

	return nil
}

func (gj *GraphJin) initAllowList() error {
	var err error

	if gj.conf.DisableAllowList {
		return nil
	}

	gj.allowList, err = allow.New(gj.conf.AllowListFile, allow.Config{
		Log: gj.log,
	})

	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	gj.queries = make(map[string]*cquery)

	list, err := gj.allowList.Load()
	if err != nil {
		return err
	}

	for _, v := range list {
		if v.Query == "" {
			continue
		}

		qt, _ := qcode.GetQType(v.Query)

		q := rquery{
			op:    qt,
			name:  v.Name,
			query: []byte(v.Query),
			vars:  []byte(v.Vars),
		}

		for roleName := range gj.roles {
			gj.queries[(v.Name + roleName)] = &cquery{q: q}
		}
	}

	return nil
}
