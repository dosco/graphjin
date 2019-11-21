package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dosco/super-graph/qcode"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
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
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		logger.Fatal().Err(err).Send()
	}
	defer tx.Rollback(ctx)

	_preparedList = make(map[string]*preparedItem)

	if err := prepareRoleStmt(ctx, tx); err != nil {
		logger.Fatal().Err(err).Msg("failed to prepare get role statement")
	}

	for _, v := range _allowList.list {
		err := prepareStmt(ctx, tx, v.gql, v.vars)
		if err != nil {
			logger.Warn().Str("gql", v.gql).Err(err).Send()
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Fatal().Err(err).Send()
	}

	logger.Info().Msgf("Registered %d queries from allow.list as prepared statements", len(_allowList.list))
}

func prepareStmt(ctx context.Context, tx pgx.Tx, gql string, varBytes json.RawMessage) error {
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

	if len(stmts) != 0 && stmts[0].qc.Type == qcode.QTQuery {
		c.req.Vars = nil
	}

	for _, s := range stmts {
		if len(s.sql) == 0 {
			continue
		}

		finalSQL, am := processTemplate(s.sql)

		pstmt, err := tx.Prepare(c.Context, "", finalSQL)
		if err != nil {
			return err
		}

		var key string

		if s.role == nil {
			key = gqlHash(gql, c.req.Vars, "")
		} else {
			key = gqlHash(gql, c.req.Vars, s.role.Name)
		}

		_preparedList[key] = &preparedItem{
			stmt:    pstmt,
			args:    am,
			skipped: s.skipped,
			qc:      s.qc,
		}

	}

	return nil
}

func prepareRoleStmt(ctx context.Context, tx pgx.Tx) error {
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

	_, err := tx.Prepare(ctx, "_sg_get_role", roleSQL)
	if err != nil {
		return err
	}

	return nil
}

func processTemplate(tmpl string) (string, [][]byte) {
	st := struct {
		vmap map[string]int
		am   [][]byte
		i    int
	}{
		vmap: make(map[string]int),
		am:   make([][]byte, 0, 5),
		i:    0,
	}

	execFunc := func(w io.Writer, tag string) (int, error) {
		if n, ok := st.vmap[tag]; ok {
			return w.Write([]byte(fmt.Sprintf("$%d", n)))
		}
		st.am = append(st.am, []byte(tag))
		st.i++
		st.vmap[tag] = st.i
		return w.Write([]byte(fmt.Sprintf("$%d", st.i)))
	}

	t1 := fasttemplate.New(tmpl, `'{{`, `}}'`)
	ts1 := t1.ExecuteFuncString(execFunc)

	t2 := fasttemplate.New(ts1, `{{`, `}}`)
	ts2 := t2.ExecuteFuncString(execFunc)

	return ts2, st.am
}
