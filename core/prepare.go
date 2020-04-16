package core

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/valyala/fasttemplate"
)

type preparedItem struct {
	sd      *sql.Stmt
	args    [][]byte
	st      stmt
	roleArg bool
}

func (sg *SuperGraph) initPrepared() error {
	ct := context.Background()

	if sg.allowList.IsPersist() {
		return nil
	}
	sg.prepared = make(map[string]*preparedItem)

	tx, err := sg.db.BeginTx(ct, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint: errcheck

	if err = sg.prepareRoleStmt(tx); err != nil {
		return fmt.Errorf("prepareRoleStmt: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	success := 0

	list, err := sg.allowList.Load()
	if err != nil {
		return err
	}

	for _, v := range list {
		if len(v.Query) == 0 {
			continue
		}

		err := sg.prepareStmt(v)
		if err == nil {
			success++
			continue
		}

		// if len(v.Vars) == 0 {
		// 	logger.Warn().Err(err).Msg(v.Query)
		// } else {
		// 	logger.Warn().Err(err).Msgf("%s %s", v.Vars, v.Query)
		// }
	}

	// logger.Info().
	// 	Msgf("Registered %d of %d queries from allow.list as prepared statements",
	// 		success, len(list))

	return nil
}

func (sg *SuperGraph) prepareStmt(item allow.Item) error {
	query := item.Query
	qb := []byte(query)
	vars := item.Vars

	qt := qcode.GetQType(query)
	ct := context.Background()

	tx, err := sg.db.BeginTx(ct, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint: errcheck

	switch qt {
	case qcode.QTQuery:
		var stmts1 []stmt
		var err error

		if sg.abacEnabled {
			stmts1, err = sg.buildMultiStmt(qb, vars)
		} else {
			stmts1, err = sg.buildRoleStmt(qb, vars, "user")
		}

		if err != nil {
			return err
		}

		//logger.Debug().Msgf("Prepared statement 'query %s' (user)", item.Name)

		err = sg.prepare(ct, tx, stmts1, stmtHash(item.Name, "user"))
		if err != nil {
			return err
		}

		if sg.anonExists {
			// logger.Debug().Msgf("Prepared statement 'query %s' (anon)", item.Name)

			stmts2, err := sg.buildRoleStmt(qb, vars, "anon")
			if err == psql.ErrAllTablesSkipped {
				return nil
			}
			if err != nil {
				return err
			}

			err = sg.prepare(ct, tx, stmts2, stmtHash(item.Name, "anon"))
			if err != nil {
				return err
			}
		}

	case qcode.QTMutation:
		for _, role := range sg.conf.Roles {
			// logger.Debug().Msgf("Prepared statement 'mutation %s' (%s)", item.Name, role.Name)

			stmts, err := sg.buildRoleStmt(qb, vars, role.Name)

			if err != nil {
				// if len(item.Vars) == 0 {
				// 	logger.Warn().Err(err).Msg(item.Query)
				// } else {
				// 	logger.Warn().Err(err).Msgf("%s %s", item.Vars, item.Query)
				// }
				continue
			}

			err = sg.prepare(ct, tx, stmts, stmtHash(item.Name, role.Name))
			if err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (sg *SuperGraph) prepare(ct context.Context, tx *sql.Tx, st []stmt, key string) error {
	finalSQL, am := processTemplate(st[0].sql)

	sd, err := tx.Prepare(finalSQL)
	if err != nil {
		return err
	}

	sg.prepared[key] = &preparedItem{
		sd:      sd,
		args:    am,
		st:      st[0],
		roleArg: len(st) > 1,
	}
	return nil
}

// nolint: errcheck
func (sg *SuperGraph) prepareRoleStmt(tx *sql.Tx) error {
	var err error

	if !sg.abacEnabled {
		return nil
	}

	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	io.WriteString(w, sg.conf.RolesQuery)
	io.WriteString(w, `) THEN `)

	io.WriteString(w, `(SELECT (CASE`)
	for _, role := range sg.conf.Roles {
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
	io.WriteString(w, sg.conf.RolesQuery)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	roleSQL, _ := processTemplate(w.String())

	sg.getRole, err = tx.Prepare(roleSQL)
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

func (sg *SuperGraph) initAllowList() error {
	var ac allow.Config
	var err error

	if len(sg.conf.AllowListFile) == 0 {
		sg.conf.UseAllowList = false
		sg.log.Printf("WRN allow list disabled no file specified")
	}

	if !sg.conf.UseAllowList {
		ac = allow.Config{CreateIfNotExists: true, Persist: true}
	}

	sg.allowList, err = allow.New(sg.conf.AllowListFile, ac)
	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	return nil
}

// nolint: errcheck
func stmtHash(name string, role string) string {
	h := sha1.New()
	io.WriteString(h, strings.ToLower(name))
	io.WriteString(h, role)
	return hex.EncodeToString(h.Sum(nil))
}
