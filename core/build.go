package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

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
	case qcode.QTQuery:
		if sg.abacEnabled {
			cq.stmts, cq.st, err = sg.buildMultiStmt(cq.q.query, cq.q.vars)
		} else {
			cq.st, err = sg.buildRoleStmt(cq.q.query, cq.q.vars, role)
		}

	case qcode.QTSubscription:
		if sg.abacEnabled {
			cq.stmts, cq.st, err = sg.buildMultiStmt(cq.q.query, cq.q.vars)
		} else {
			cq.st, err = sg.buildRoleStmt(cq.q.query, cq.q.vars, role)
		}

	case qcode.QTMutation:
		cq.st, err = sg.buildRoleStmt(cq.q.query, cq.q.vars, role)

	default:
		err = errors.New("unknown query")
	}

	cq.roleArg = (len(cq.stmts) > 0)
	return err
}

func (sg *SuperGraph) buildRoleStmt(query, vars []byte, role string) (stmt, error) {
	var st stmt

	ro, ok := sg.roles[role]
	if !ok {
		return st, fmt.Errorf(`roles '%s' not defined in c.sg.config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return st, err
		}
	}

	qc, err := sg.qc.Compile(query, vm, ro.Name)
	if err != nil {
		return st, err
	}

	w := &bytes.Buffer{}
	st.md, err = sg.pc.Compile(w, qc)
	if err != nil {
		return st, err
	}

	st.role = ro
	st.qc = qc
	st.sql = w.String()

	return st, nil
}

func (sg *SuperGraph) buildMultiStmt(query, vars []byte) ([]stmt, stmt, error) {
	var vm map[string]json.RawMessage
	var err error
	var st stmt
	var md psql.Metadata

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return nil, st, err
		}
	}

	if sg.conf.RolesQuery == "" {
		return nil, st, errors.New("roles_query not defined")
	}

	stmts := make([]stmt, 0, len(sg.conf.Roles))
	w := &bytes.Buffer{}

	for i := 0; i < len(sg.conf.Roles); i++ {
		role := &sg.conf.Roles[i]

		// skip anon as it's not included in the combined multi-statement
		if role.Name == "anon" {
			continue
		}

		qc, err := sg.qc.Compile(query, vm, role.Name)
		if err != nil {
			return nil, st, err
		}

		stmts = append(stmts, stmt{role: role, qc: qc})
		s := &stmts[len(stmts)-1]

		md = sg.pc.CompileQuery(w, qc, md)

		s.sql = w.String()
		s.md = md

		w.Reset()
	}
	st = stmts[0]

	st.sql, err = sg.renderUserQuery(md, stmts)
	if err != nil {
		return nil, st, err
	}

	return stmts, st, nil
}

//nolint: errcheck
func (sg *SuperGraph) renderUserQuery(md psql.Metadata, stmts []stmt) (string, error) {
	if sg.conf.RolesQuery == "" {
		return "", errors.New("roles_query not defined")
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

	w.WriteString(`END) FROM (SELECT (CASE WHEN EXISTS (`)
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
	w.WriteString(`ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1; `)

	return w.String(), nil
}
