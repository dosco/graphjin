package serv

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/dosco/super-graph/qcode"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/valyala/fasttemplate"
)

type preparedItem struct {
	sd   *pgconn.StatementDescription
	args [][]byte
	st   *stmt
}

var (
	_preparedList map[string]*preparedItem
)

func initPreparedList() {
	c := context.Background()
	_preparedList = make(map[string]*preparedItem)

	tx, err := db.Begin(c)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}
	defer tx.Rollback(c)

	err = prepareRoleStmt(c, tx)
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to prepare get role statement")
	}

	if err := tx.Commit(c); err != nil {
		errlog.Fatal().Err(err).Send()
	}

	success := 0

	for _, v := range _allowList.list {
		if len(v.gql) == 0 {
			continue
		}

		err := prepareStmt(c, v.gql, v.vars)
		if err == nil {
			success++
			continue
		}

		if len(v.vars) == 0 {
			logger.Warn().Err(err).Msg(v.gql)
		} else {
			logger.Warn().Err(err).Msgf("%s %s", v.vars, v.gql)
		}
	}

	logger.Info().
		Msgf("Registered %d of %d queries from allow.list as prepared statements",
			success, len(_allowList.list))
}

func prepareStmt(c context.Context, gql string, vars []byte) error {
	qt := qcode.GetQType(gql)
	q := []byte(gql)

	tx, err := db.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)

	switch qt {
	case qcode.QTQuery:
		stmts1, err := buildMultiStmt(q, vars)
		if err != nil {
			return err
		}

		err = prepare(c, tx, &stmts1[0], gqlHash(gql, vars, "user"))
		if err != nil {
			return err
		}

		stmts2, err := buildRoleStmt(q, vars, "anon")
		if err != nil {
			return err
		}

		err = prepare(c, tx, &stmts2[0], gqlHash(gql, vars, "anon"))
		if err != nil {
			return err
		}

	case qcode.QTMutation:
		for _, role := range conf.Roles {
			stmts, err := buildRoleStmt(q, vars, role.Name)
			if err != nil {
				return err
			}

			err = prepare(c, tx, &stmts[0], gqlHash(gql, vars, role.Name))
			if err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(c); err != nil {
		return err
	}

	return nil
}

func prepare(c context.Context, tx pgx.Tx, st *stmt, key string) error {
	finalSQL, am := processTemplate(st.sql)

	sd, err := tx.Prepare(c, "", finalSQL)
	if err != nil {
		return err
	}

	_preparedList[key] = &preparedItem{
		sd:   sd,
		args: am,
		st:   st,
	}
	return nil
}

func prepareRoleStmt(c context.Context, tx pgx.Tx) error {
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

	_, err := tx.Prepare(c, "_sg_get_role", roleSQL)
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
