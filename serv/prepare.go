package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dosco/super-graph/qcode"
	"github.com/jackc/pgconn"
	"github.com/valyala/fasttemplate"
)

type preparedItem struct {
	stmt    *pgconn.StatementDescription
	args    [][]byte
	skipped uint32
	qc      *qcode.QCode
}

var (
	_preparedList map[string]*preparedItem
)

func initPreparedList() {
	_preparedList = make(map[string]*preparedItem)

	if err := prepareRoleStmt(); err != nil {
		logger.Fatal().Err(err).Msg("failed to prepare get role statement")
	}

	for _, v := range _allowList.list {
		err := prepareStmt(v.gql, v.vars)
		if err != nil {
			logger.Warn().Str("gql", v.gql).Err(err).Send()
		}
	}
}

func prepareStmt(gql string, varBytes json.RawMessage) error {
	if len(gql) == 0 {
		return nil
	}

	c := &coreContext{Context: context.Background()}
	c.req.Query = gql
	c.req.Vars = varBytes

	stmts, err := c.buildStmt()
	if err != nil {
		return err
	}

	for _, s := range stmts {
		if len(s.sql) == 0 {
			continue
		}

		finalSQL, am := processTemplate(s.sql)

		ctx := context.Background()

		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		pstmt, err := tx.Prepare(ctx, "", finalSQL)
		if err != nil {
			return err
		}

		var key string

		if s.role == nil {
			key = gqlHash(gql, varBytes, "")
		} else {
			key = gqlHash(gql, varBytes, s.role.Name)
		}

		_preparedList[key] = &preparedItem{
			stmt:    pstmt,
			args:    am,
			skipped: s.skipped,
			qc:      s.qc,
		}

		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}

func prepareRoleStmt() error {
	if len(conf.RolesQuery) == 0 {
		return nil
	}

	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE`)
	for _, role := range conf.Roles {
		if len(role.Match) == 0 {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, role.Name)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE {{role}} END) FROM (`)
	io.WriteString(w, conf.RolesQuery)
	io.WriteString(w, `) AS "_sg_auth_roles_query"`)

	roleSQL, _ := processTemplate(w.String())

	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Prepare(ctx, "_sg_get_role", roleSQL)
	if err != nil {
		return err
	}

	return nil
}

func processTemplate(tmpl string) (string, [][]byte) {
	t := fasttemplate.New(tmpl, `{{`, `}}`)
	am := make([][]byte, 0, 5)
	i := 0

	vmap := make(map[string]int)

	return t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		if n, ok := vmap[tag]; ok {
			return w.Write([]byte(fmt.Sprintf("$%d", n)))
		}
		am = append(am, []byte(tag))
		i++
		vmap[tag] = i
		return w.Write([]byte(fmt.Sprintf("$%d", i)))
	}), am
}
