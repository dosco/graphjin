package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/qcode"
)

type queryReq struct {
	op    qcode.QType
	ns    string
	name  string
	query []byte
	vars  []byte
	order [2]string
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
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	gj.roleStmt = w.String()

	return nil
}

func (gj *graphjin) initAllowList() error {
	var err error

	gj.allowList, err = allow.New(allow.Config{Log: gj.log}, gj.fs)

	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	if gj.conf.DisableAllowList {
		return nil
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
		qt, _ := qcode.GetQType(q)
		qk := gj.generateQueryKeys(item)
		qr := queryReq{
			op:    qt,
			name:  item.Name,
			query: []byte(q),
			vars:  []byte(item.Vars),
		}
		ov := item.Metadata.Order.Var

		for _, v := range qk {
			qc := &queryComp{qr: qr}
			if ov != "" {
				qc.qr.order = [2]string{ov, strconv.Quote(v.val)}
			}
			gj.queries[v.key] = qc
		}
	}

	return nil
}

type queryKey struct {
	key string
	val string
}

func (gj *graphjin) generateQueryKeys(item allow.Item) []queryKey {
	var qk []queryKey

	for roleName := range gj.roles {
		k1 := (item.Namespace + item.Name + roleName)
		qk = append(qk, queryKey{key: k1})

		for _, v := range item.Metadata.Order.Values {
			k2 := k1 + v
			qk = append(qk, queryKey{key: k2, val: v})
		}
	}
	return qk
}

func (gj *graphjin) getQuery(qr queryReq, role string, vm map[string]json.RawMessage) (*queryComp, error) {
	qk := (qr.ns + qr.name + role)
	qc, ok := gj.queries[qk]
	if !ok {
		return nil, errNotFound
	}

	ov := qc.qr.order[0]
	if ov == "" {
		return qc, nil
	}

	v, ok := vm[ov]
	if !ok || v[0] != '"' || len(v) == 2 {
		return nil, fmt.Errorf("required variable not set: %s", ov)
	}

	oval := string(v[1:(len(v) - 1)])
	qc, ok = gj.queries[(qk + oval)]
	if !ok {
		return nil, fmt.Errorf("invalid value for variable (%s): %s", ov, oval)
	}
	return qc, nil
}
