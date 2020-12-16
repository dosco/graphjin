package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
)

type stmt struct {
	role *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func (gj *GraphJin) compileQuery(cq *cquery, role string) error {
	var err error

	// In production mode enforce the allow list and
	// compile and cache the result else compile each time
	if gj.allowList != nil && gj.conf.EnforceAllowList {
		if cq1, ok := gj.queries[(cq.q.name + role)]; ok {
			cq.q = cq1.q
		} else {
			return errNotFound
		}

		if cq.st.sql == "" {
			cq.Do(func() {
				err = gj.compileQueryFn(cq, role)
			})
		}

	} else {
		err = gj.compileQueryFn(cq, role)
	}

	return err
}

func (gj *GraphJin) compileQueryFn(cq *cquery, role string) error {
	var err error

	switch cq.q.op {
	case qcode.QTQuery, qcode.QTSubscription:
		if gj.abacEnabled && role == "user" {
			err = gj.buildMultiStmt(cq)
		} else {
			err = gj.buildRoleStmt(cq, role)
		}

	case qcode.QTMutation:
		err = gj.buildRoleStmt(cq, role)

	default:
		err = errors.New("unknown query")
	}

	cq.roleArg = (len(cq.stmts) > 0)
	return err
}

func (gj *GraphJin) buildRoleStmt(cq *cquery, role string) error {
	query := cq.q.query
	vars := cq.q.vars

	ro, ok := gj.roles[role]
	if !ok {
		return fmt.Errorf(`roles '%s' not defined in c.gj.config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return err
		}
	}

	qc, err := gj.qc.Compile(query, vm, ro.Name)
	if err != nil {
		return err
	}

	var w bytes.Buffer

	cq.st.md, err = gj.pc.Compile(&w, qc)
	if err != nil {
		return err
	}

	cq.st.role = ro
	cq.st.qc = qc
	cq.st.sql = w.String()

	return nil
}

func (gj *GraphJin) buildMultiStmt(cq *cquery) error {
	var vm map[string]json.RawMessage
	var md psql.Metadata
	var err error

	query := cq.q.query
	vars := cq.q.vars

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return err
		}
	}

	cq.stmts = make([]stmt, 0, len(gj.conf.Roles))
	w := &bytes.Buffer{}

	for i := 0; i < len(gj.conf.Roles); i++ {
		role := &gj.conf.Roles[i]

		// skip anon as it's not included in the combined multi-statement
		if role.Name == "anon" {
			continue
		}

		qc, err := gj.qc.Compile(query, vm, role.Name)
		if err != nil {
			return err
		}

		cq.stmts = append(cq.stmts, stmt{role: role, qc: qc})
		s := &cq.stmts[len(cq.stmts)-1]

		md = gj.pc.CompileQuery(w, qc, md)
		s.sql = w.String()
		s.md = md

		w.Reset()
	}

	fsql, err := gj.renderUserQuery(&md, cq.stmts)
	cq.st = cq.stmts[0]
	cq.st.md = md
	cq.st.sql = fsql

	return err
}

//nolint: errcheck
func (gj *GraphJin) renderUserQuery(md *psql.Metadata, stmts []stmt) (string, error) {
	if gj.conf.RolesQuery == "" {
		return "", errors.New("roles_query: empty of not defined")
	}

	if !strings.Contains(gj.conf.RolesQuery, "$user_id") {
		return "", fmt.Errorf("roles_query: $user_id variable missing")
	}

	w := &bytes.Buffer{}

	w.WriteString(`SELECT "_sg_auth_info"."role", (CASE "_sg_auth_info"."role" `)

	for _, s := range stmts {
		if s.role.Match == "" &&
			s.role.Name != "user" && s.role.Name != "anon" {
			continue
		}
		w.WriteString(`WHEN '`)
		w.WriteString(s.role.Name)
		w.WriteString(`' THEN (`)
		w.WriteString(s.sql)
		w.WriteString(`) `)
	}

	w.WriteString(`END) as "__root" FROM (SELECT (CASE WHEN EXISTS (`)
	md.RenderVar(w, gj.conf.RolesQuery)
	w.WriteString(`) THEN `)

	w.WriteString(`(SELECT (CASE`)
	for _, s := range stmts {
		if s.role.Match == "" {
			continue
		}
		w.WriteString(` WHEN `)
		w.WriteString(s.role.Match)
		w.WriteString(` THEN '`)
		w.WriteString(s.role.Name)
		w.WriteString(`'`)
	}

	w.WriteString(` ELSE 'user' END) FROM (`)
	md.RenderVar(w, gj.conf.RolesQuery)
	w.WriteString(`) AS "_sg_auth_roles_query" LIMIT 1) `)
	w.WriteString(`ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1 `)

	return w.String(), nil
}
