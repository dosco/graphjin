package core

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/qcode"
)

type queryReq struct {
	op    qcode.QType
	ns    string
	name  string
	query []byte
	vars  []byte
}

// nolint: errcheck
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

func (gj *graphjin) initAllowList() error {
	var err error

	if gj.conf.DisableAllowList {
		gj.allowList, err = allow.NewReadOnly(gj.fs)
	} else {
		gj.allowList, err = allow.New(allow.Config{Log: gj.log}, gj.fs)
	}

	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	gj.queries = make(map[string]*queryComp)

	list, err := gj.allowList.Load()
	if err != nil {
		return err
	}

	for _, item := range list {
		if item.Query == "" {
			continue
		}

		q := strings.TrimSpace(item.Query)
		h, err := graph.FastParse(q)
		if err != nil {
			return err
		}

		qr := queryReq{
			op:    qcode.GetQType(h.Type),
			name:  h.Name,
			query: []byte(q),
			vars:  []byte(item.Vars),
		}

		qk := gj.generateQueryKeys(item)

		for _, v := range qk {
			qc := &queryComp{qr: qr}
			gj.queries[v.key] = qc
		}
	}

	return nil
}

type queryKey struct {
	key string
}

func (gj *graphjin) generateQueryKeys(item allow.Item) []queryKey {
	var qk []queryKey

	for roleName := range gj.roles {
		k1 := (item.Namespace + item.Name + roleName)
		qk = append(qk, queryKey{key: k1})
	}
	return qk
}

func (gj *graphjin) getQuery(qr queryReq, role string) (*queryComp, error) {
	qk := (qr.ns + qr.name + role)
	qc, ok := gj.queries[qk]
	if !ok {
		return nil, ErrNotFound
	}
	return qc, nil
}
