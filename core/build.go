package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
)

type stmt struct {
	role *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func (sg *SuperGraph) compileQuery(cq *cquery, role string) error {
	var err error

	// In production mode enforce the allow list and
	// compile and cache the result else compile each time
	if sg.conf.UseAllowList {
		if cq1, ok := sg.queries[(cq.q.name + role)]; ok {
			cq.q = cq1.q
		} else {
			return errNotFound
		}

		if cq.st.sql == "" {
			cq.Do(func() {
				err = sg.compileQueryFn(cq, role)
			})
		}

	} else {
		err = sg.compileQueryFn(cq, role)
	}

	return err
}

func (sg *SuperGraph) compileQueryFn(cq *cquery, role string) error {
	var err error

	switch cq.q.op {
	case qcode.QTQuery, qcode.QTSubscription:
		if sg.abacEnabled {
			err = sg.buildMultiStmt(cq)
		} else {
			err = sg.buildRoleStmt(cq, role)
		}

	case qcode.QTMutation:
		err = sg.buildRoleStmt(cq, role)

	default:
		err = errors.New("unknown query")
	}

	cq.roleArg = (len(cq.stmts) > 0)
	return err
}

func (sg *SuperGraph) buildRoleStmt(cq *cquery, role string) error {
	query := cq.q.query
	vars := cq.q.vars

	ro, ok := sg.roles[role]
	if !ok {
		return fmt.Errorf(`roles '%s' not defined in c.sg.config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return err
		}
	}

	qc, err := sg.qc.Compile(query, vm, ro.Name)
	if err != nil {
		return err
	}

	w := &bytes.Buffer{}
	cq.st.md, err = sg.pc.Compile(w, qc)
	if err != nil {
		return err
	}

	cq.st.role = ro
	cq.st.qc = qc
	cq.st.sql = w.String()

	return nil
}

func (sg *SuperGraph) buildMultiStmt(cq *cquery) error {
	var vm map[string]json.RawMessage
	var err error
	var md psql.Metadata

	query := cq.q.query
	vars := cq.q.vars

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return err
		}
	}

	if cq.q.op == qcode.QTSubscription {
		md.Poll = true
	}

	cq.stmts = make([]stmt, 0, len(sg.conf.Roles))
	w := &bytes.Buffer{}

	for i := 0; i < len(sg.conf.Roles); i++ {
		role := &sg.conf.Roles[i]

		// skip anon as it's not included in the combined multi-statement
		if role.Name == "anon" {
			continue
		}

		qc, err := sg.qc.Compile(query, vm, role.Name)
		if err != nil {
			return err
		}

		cq.stmts = append(cq.stmts, stmt{role: role, qc: qc})
		s := &cq.stmts[len(cq.stmts)-1]

		md = sg.pc.CompileQuery(w, qc, md)

		s.sql = w.String()
		s.md = md

		w.Reset()
	}

	fsql, err := sg.renderUserQuery(&md, cq.stmts)

	cq.st = cq.stmts[0]
	cq.st.md = md
	cq.st.sql = fsql

	return err
}

//nolint: errcheck
func (sg *SuperGraph) renderUserQuery(md *psql.Metadata, stmts []stmt) (string, error) {
	if sg.conf.RolesQuery == "" {
		return "", errors.New("roles_query: empty of not defined")
	}

	if !strings.Contains(sg.conf.RolesQuery, "$user_id") {
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
	md.RenderVar(w, sg.conf.RolesQuery)
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
	md.RenderVar(w, sg.conf.RolesQuery)
	w.WriteString(`) AS "_sg_auth_roles_query" LIMIT 1) `)
	w.WriteString(`ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1 `)

	return w.String(), nil
}
