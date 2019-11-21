package serv

import (
	"bytes"
	"encoding/json"
	"errors"
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

func (c *coreContext) buildStmt() ([]stmt, error) {
	var vars map[string]json.RawMessage

	if len(c.req.Vars) != 0 {
		if err := json.Unmarshal(c.req.Vars, &vars); err != nil {
			return nil, err
		}
	}

	gql := []byte(c.req.Query)

	if len(conf.Roles) == 0 {
		return nil, errors.New(`no roles found ('user' and 'anon' required)`)
	}

	qc, err := qcompile.Compile(gql, conf.Roles[0].Name)
	if err != nil {
		return nil, err
	}

	stmts := make([]stmt, 0, len(conf.Roles))
	mutation := (qc.Type != qcode.QTQuery)
	w := &bytes.Buffer{}

	for i := 1; i < len(conf.Roles); i++ {
		role := &conf.Roles[i]

		// For mutations only render sql for a single role from the request
		if mutation && len(c.req.role) != 0 && role.Name != c.req.role {
			continue
		}

		qc, err = qcompile.Compile(gql, role.Name)
		if err != nil {
			return nil, err
		}

		if conf.Production && role.Name == "anon" {
			for _, id := range qc.Roots {
				root := qc.Selects[id]
				if _, ok := role.tablesMap[root.Table]; !ok {
					continue
				}
			}
		}

		stmts = append(stmts, stmt{role: role, qc: qc})

		if mutation {
			skipped, err := pcompile.Compile(qc, w, psql.Variables(vars))
			if err != nil {
				return nil, err
			}

			s := &stmts[len(stmts)-1]
			s.skipped = skipped
			s.sql = w.String()
			w.Reset()
		}
	}

	if mutation {
		return stmts, nil
	}

	io.WriteString(w, `SELECT "_sg_auth_info"."role", (CASE "_sg_auth_info"."role" `)

	for _, s := range stmts {
		io.WriteString(w, `WHEN '`)
		io.WriteString(w, s.role.Name)
		io.WriteString(w, `' THEN (`)

		s.skipped, err = pcompile.Compile(s.qc, w, psql.Variables(vars))
		if err != nil {
			return nil, err
		}

		io.WriteString(w, `) `)
	}
	io.WriteString(w, `END) FROM (`)

	if len(conf.RolesQuery) == 0 {
		v := c.Value(userRoleKey)

		io.WriteString(w, `VALUES ("`)
		if v != nil {
			io.WriteString(w, v.(string))
		} else {
			io.WriteString(w, c.req.role)
		}
		io.WriteString(w, `")) AS "_sg_auth_info"(role) LIMIT 1;`)

	} else {

		io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
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

		if len(c.req.role) == 0 {
			io.WriteString(w, ` ELSE 'anon' END) FROM (`)
		} else {
			io.WriteString(w, ` ELSE '`)
			io.WriteString(w, c.req.role)
			io.WriteString(w, `' END) FROM (`)
		}

		io.WriteString(w, conf.RolesQuery)
		io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) ELSE '`)
		if len(c.req.role) == 0 {
			io.WriteString(w, `anon`)
		} else {
			io.WriteString(w, c.req.role)
		}
		io.WriteString(w, `' END) FROM (VALUES (1)) AS "_sg_auth_filler") AS "_sg_auth_info"(role) LIMIT 1; `)
	}

	stmts[0].sql = w.String()
	stmts[0].role = nil

	return stmts, nil
}

func (c *coreContext) buildStmtByRole(role string) (stmt, error) {
	var st stmt
	var err error

	if len(role) == 0 {
		return st, errors.New(`no role defined`)
	}

	var vars map[string]json.RawMessage

	if len(c.req.Vars) != 0 {
		if err := json.Unmarshal(c.req.Vars, &vars); err != nil {
			return st, err
		}
	}

	gql := []byte(c.req.Query)

	st.qc, err = qcompile.Compile(gql, role)
	if err != nil {
		return st, err
	}

	w := &bytes.Buffer{}

	st.skipped, err = pcompile.Compile(st.qc, w, psql.Variables(vars))
	if err != nil {
		return st, err
	}

	st.sql = w.String()

	return st, nil

}
