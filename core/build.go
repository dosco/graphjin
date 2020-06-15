package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
)

type stmt struct {
	role *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func (sg *SuperGraph) buildStmt(qt qcode.QType, query, vars []byte, role string) ([]stmt, error) {
	switch qt {
	case qcode.QTMutation:
		return sg.buildRoleStmt(query, vars, role)

	case qcode.QTQuery:
		if role == "anon" {
			return sg.buildRoleStmt(query, vars, "anon")
		}

		if sg.abacEnabled {
			return sg.buildMultiStmt(query, vars)
		}

		return sg.buildRoleStmt(query, vars, "user")

	default:
		return nil, fmt.Errorf("unknown query type '%d'", qt)
	}
}

func (sg *SuperGraph) buildRoleStmt(query, vars []byte, role string) ([]stmt, error) {
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

	stmts := []stmt{stmt{role: ro, qc: qc}}
	w := &bytes.Buffer{}

	stmts[0].md, err = sg.pc.Compile(w, qc, psql.Variables(vm))
	if err != nil {
		return nil, err
	}

	stmts[0].sql = w.String()

	return stmts, nil
}

func (sg *SuperGraph) buildMultiStmt(query, vars []byte) ([]stmt, error) {
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
	md := psql.Metadata{}

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

	io.WriteString(w, `SELECT "_sg_auth_info"."role", (CASE "_sg_auth_info"."role" `)

	for _, s := range stmts {
		if s.role.Match == "" &&
			s.role.Name != "user" && s.role.Name != "anon" {
			continue
		}
		io.WriteString(w, `WHEN '`)
		io.WriteString(w, s.role.Name)
		io.WriteString(w, `' THEN (`)
		io.WriteString(w, s.sql)
		io.WriteString(w, `) `)
	}

	io.WriteString(w, `END) FROM (SELECT (CASE WHEN EXISTS (`)
	md.RenderVar(w, sg.conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	io.WriteString(w, `(SELECT (CASE`)
	for _, s := range stmts {
		if s.role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, s.role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, s.role.Name)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE 'user' END) FROM (`)
	md.RenderVar(w, sg.conf.RolesQuery)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1; `)

	return w.String(), nil
}

// func (sg *SuperGraph) hasTablesWithConfig(qc *qcode.QCode, role *Role) bool {
// 	for _, id := range qc.Roots {
// 		t, err := sg.schema.GetTable(qc.Selects[id].Name)
// 		if err != nil {
// 			return false
// 		}

// 		if r := role.GetTable(t.Name); r == nil {
// 			return false
// 		}
// 	}
// 	return true
// }
