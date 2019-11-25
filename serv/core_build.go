package serv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
)

type stmt struct {
	role    *configRole
	qc      *qcode.QCode
	skipped uint32
	sql     string
}

func buildStmt(qt qcode.QType, gql, vars []byte, role string) ([]stmt, error) {
	switch qt {
	case qcode.QTMutation:
		return buildRoleStmt(gql, vars, role)

	case qcode.QTQuery:
		switch {
		case role == "anon":
			return buildRoleStmt(gql, vars, role)

		default:
			return buildMultiStmt(gql, vars)
		}

	default:
		return nil, fmt.Errorf("unknown query type '%d'", qt)
	}
}

func buildRoleStmt(gql, vars []byte, role string) ([]stmt, error) {
	ro, ok := conf.roles[role]
	if !ok {
		return nil, fmt.Errorf(`roles '%s' not defined in config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return nil, err
		}
	}

	qc, err := qcompile.Compile(gql, ro.Name)
	if err != nil {
		return nil, err
	}

	// For the 'anon' role in production only compile
	// queries for tables defined in the config file.
	if conf.Production &&
		ro.Name == "anon" &&
		hasTablesWithConfig(qc, ro) == false {
		return nil, errors.New("query contains tables with no 'anon' role config")
	}

	stmts := []stmt{stmt{role: ro, qc: qc}}
	w := &bytes.Buffer{}

	skipped, err := pcompile.Compile(qc, w, psql.Variables(vm))
	if err != nil {
		return nil, err
	}

	stmts[0].skipped = skipped
	stmts[0].sql = w.String()

	return stmts, nil
}

func buildMultiStmt(gql, vars []byte) ([]stmt, error) {
	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return nil, err
		}
	}

	if len(conf.RolesQuery) == 0 {
		return buildRoleStmt(gql, vars, "user")
	}

	stmts := make([]stmt, 0, len(conf.Roles))
	w := &bytes.Buffer{}

	for i := 0; i < len(conf.Roles); i++ {
		role := &conf.Roles[i]

		qc, err := qcompile.Compile(gql, role.Name)
		if err != nil {
			return nil, err
		}

		stmts = append(stmts, stmt{role: role, qc: qc})

		skipped, err := pcompile.Compile(qc, w, psql.Variables(vm))
		if err != nil {
			return nil, err
		}

		s := &stmts[len(stmts)-1]
		s.skipped = skipped
		s.sql = w.String()
		w.Reset()
	}

	sql, err := renderUserQuery(stmts, vm)
	if err != nil {
		return nil, err
	}

	stmts[0].sql = sql
	return stmts, nil
}

func renderUserQuery(
	stmts []stmt, vars map[string]json.RawMessage) (string, error) {

	var err error
	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT "_sg_auth_info"."role", (CASE "_sg_auth_info"."role" `)

	for _, s := range stmts {
		if len(s.role.Match) == 0 &&
			s.role.Name != "user" && s.role.Name != "anon" {
			continue
		}
		io.WriteString(w, `WHEN '`)
		io.WriteString(w, s.role.Name)
		io.WriteString(w, `' THEN (`)

		s.skipped, err = pcompile.Compile(s.qc, w, psql.Variables(vars))
		if err != nil {
			return "", err
		}
		io.WriteString(w, `) `)
	}

	io.WriteString(w, `END) FROM (SELECT (CASE WHEN EXISTS (`)
	io.WriteString(w, conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	io.WriteString(w, `(SELECT (CASE`)
	for _, s := range stmts {
		if len(s.role.Match) == 0 {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, s.role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, s.role.Name)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE 'user' END) FROM (`)
	io.WriteString(w, conf.RolesQuery)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1; `)

	return w.String(), nil
}

func hasTablesWithConfig(qc *qcode.QCode, role *configRole) bool {
	for _, id := range qc.Roots {
		t, err := schema.GetTable(qc.Selects[id].Table)
		if err != nil {
			return false
		}
		if _, ok := role.tablesMap[t.Name]; !ok {
			return false
		}
	}
	return true
}
