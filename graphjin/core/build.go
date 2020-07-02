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

func (sg *SuperGraph) buildStmt(qt qcode.QType, query, vars []byte, role string, poll bool) ([]stmt, error) {
	if qt == qcode.QTQuery && sg.abacEnabled {
		return sg.buildMultiStmt(query, vars, poll)
	}

	return sg.buildRoleStmt(query, vars, role, poll)
}

func (sg *SuperGraph) buildRoleStmt(query, vars []byte, role string, poll bool) ([]stmt, error) {
	ro, ok := sg.roles[role]
	if !ok {
		return nil, fmt.Errorf(`roles '%s' not defined in c.sg.config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return nil, err
		}
	}

	qc, err := sg.qc.Compile(query, ro.Name)
	if err != nil {
		return nil, err
	}

	stmts := []stmt{{role: ro, qc: qc}}
	w := &bytes.Buffer{}
	md := psql.Metadata{Poll: poll}

	stmts[0].md, err = sg.pc.CompileWithMetadata(w, qc, psql.Variables(vm), md)
	if err != nil {
		return nil, err
	}
	stmts[0].sql = w.String()

	return stmts, nil
}

func (sg *SuperGraph) buildMultiStmt(query, vars []byte, poll bool) ([]stmt, error) {
	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return nil, err
		}
	}

	if sg.conf.RolesQuery == "" {
		return nil, errors.New("roles_query not defined")
	}

	stmts := make([]stmt, 0, len(sg.conf.Roles))
	w := &bytes.Buffer{}
	md := psql.Metadata{Poll: poll}

	for i := 0; i < len(sg.conf.Roles); i++ {
		role := &sg.conf.Roles[i]

		// skip anon as it's not included in the combined multi-statement
		if role.Name == "anon" {
			continue
		}

		qc, err := sg.qc.Compile(query, role.Name)
		if err != nil {
			return nil, err
		}

		stmts = append(stmts, stmt{role: role, qc: qc})
		s := &stmts[len(stmts)-1]

		md, err = sg.pc.CompileWithMetadata(w, qc, psql.Variables(vm), md)
		if err != nil {
			return nil, err
		}

		s.sql = w.String()
		s.md = md

		w.Reset()
	}

	sql, err := sg.renderUserQuery(md, stmts)
	if err != nil {
		return nil, err
	}

	stmts[0].sql = sql
	return stmts, nil
}

//nolint: errcheck
func (sg *SuperGraph) renderUserQuery(md psql.Metadata, stmts []stmt) (string, error) {
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
