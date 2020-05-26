package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/qcode"
)

type preparedItem struct {
	sd      *sql.Stmt
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
		if err != nil {
			sg.log.Printf("WRN %s: %v", v.Name, err)
		} else {
			success++
		}
	}

	sg.log.Printf("INF allow list: prepared %d / %d queries", success, len(list))

	return nil
}

func (sg *SuperGraph) prepareStmt(item allow.Item) error {
	query := item.Query
	qb := []byte(query)
	vars := item.Vars

	qt := qcode.GetQType(query)
	ct := context.Background()
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

		err = sg.prepare(ct, stmts1, stmtHash(item.Name, "user"))
		if err != nil {
			return err
		}

		if sg.anonExists {
			// logger.Debug().Msgf("Prepared statement 'query %s' (anon)", item.Name)

			stmts2, err := sg.buildRoleStmt(qb, vars, "anon")
			if err != nil {
				return err
			}

			err = sg.prepare(ct, stmts2, stmtHash(item.Name, "anon"))
			if err != nil {
				return err
			}
		}

	case qcode.QTMutation:
		for _, role := range sg.conf.Roles {
			// logger.Debug().Msgf("Prepared statement 'mutation %s' (%s)", item.Name, role.Name)

			stmts, err := sg.buildRoleStmt(qb, vars, role.Name)
			if err != nil {
				return err
			}

			err = sg.prepare(ct, stmts, stmtHash(item.Name, role.Name))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (sg *SuperGraph) prepare(ct context.Context, st []stmt, key string) error {
	sd, err := sg.db.PrepareContext(ct, st[0].sql)
	if err != nil {
		return fmt.Errorf("prepare failed: %v: %s", err, st[0].sql)
	}

	sg.prepared[key] = &preparedItem{
		sd:      sd,
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

	rq := strings.ReplaceAll(sg.conf.RolesQuery, "$user_id", "$1")
	w := &bytes.Buffer{}

	io.WriteString(w, `SELECT (CASE WHEN EXISTS (`)
	io.WriteString(w, rq)
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

	io.WriteString(w, ` ELSE $2 END) FROM (`)
	io.WriteString(w, sg.conf.RolesQuery)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	sg.getRole, err = tx.Prepare(w.String())
	if err != nil {
		return err
	}

	return nil
}

func (sg *SuperGraph) initAllowList() error {
	var ac allow.Config
	var err error

	if len(sg.conf.AllowListFile) == 0 {
		sg.conf.UseAllowList = false
		sg.log.Printf("WRN allow list disabled no file specified")
	}

	// When list is not eabled it is still created and
	// and new queries are saved to it.
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
	h := sha256.New()
	io.WriteString(h, strings.ToLower(name))
	io.WriteString(h, role)
	return hex.EncodeToString(h.Sum(nil))
}
