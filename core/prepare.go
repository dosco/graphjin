package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/qcode"
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
func (sg *SuperGraph) prepareRoleStmt() error {
	if !sg.abacEnabled {
		return nil
	}

	if !strings.Contains(sg.conf.RolesQuery, "$user_id") {
		return fmt.Errorf("roles_query: $user_id variable missing")
	}

	rq := strings.ReplaceAll(sg.conf.RolesQuery, "$user_id", "$1")
	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	io.WriteString(w, rq)
	io.WriteString(w, `) THEN `)

	io.WriteString(w, `(SELECT (CASE`)
	for _, role := range sg.conf.Roles {
		if role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, role.Name)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE $2 END) FROM (`)
	io.WriteString(w, rq)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	sg.roleStmt = w.String()
	return nil
}

func (sg *SuperGraph) initAllowList() error {
	var ac allow.Config
	var err error

	if sg.conf.AllowListFile == "" {
		sg.conf.AllowListFile = "allow.list"
	}

	// When list is not enabled it is still created and
	// and new queries are saved to it.
	if !sg.conf.UseAllowList {
		ac = allow.Config{CreateIfNotExists: true, Persist: true, Log: sg.log}
	}

	sg.allowList, err = allow.New(sg.conf.AllowListFile, ac)
	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	// List is presistant in dev mode so don't go ahead and set
	// the queries struct
	if sg.allowList.IsPersist() {
		return nil
	}

	sg.queries = make(map[string]*cquery)

	list, err := sg.allowList.Load()
	if err != nil {
		return err
	}

	for _, v := range list {
		if v.Query == "" {
			continue
		}

		q := rquery{
			op:    qcode.GetQType(v.Query),
			name:  v.Name,
			query: []byte(v.Query),
			vars:  []byte(v.Vars),
		}

		switch q.op {
		case qcode.QTQuery, qcode.QTSubscription:
			sg.queries[(v.Name + "user")] = &cquery{q: q}
			sg.queries[(v.Name + "anon")] = &cquery{q: q}

		case qcode.QTMutation:
			for _, role := range sg.conf.Roles {
				sg.queries[(v.Name + role.Name)] = &cquery{q: q}
			}
		}
	}

	return nil
}
