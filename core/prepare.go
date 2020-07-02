package core

import (
	"bytes"
	"database/sql"
	"fmt"
	"hash/maphash"
	"io"
	"strings"
	"sync"

	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/qcode"
)

type query struct {
	sync.Once
	sd      *sql.Stmt
	ai      allow.Item
	qt      qcode.QType
	err     error
	st      stmt
	roleArg bool
}

func (sg *SuperGraph) prepare(q *query, role string) {
	var stmts []stmt
	var err error

	qb := []byte(q.ai.Query)
	vars := []byte(q.ai.Vars)

	switch q.qt {
	case qcode.QTQuery:
		if sg.abacEnabled {
			stmts, err = sg.buildMultiStmt(qb, vars, false)
		} else {
			stmts, err = sg.buildRoleStmt(qb, vars, role, false)
		}

	case qcode.QTMutation:
		stmts, err = sg.buildRoleStmt(qb, vars, role, false)

	}

	if err != nil {
		sg.log.Printf("WRN %s %s: %v", q.qt, q.ai.Name, err)
		return
	}

	if len(stmts) == 0 {
		sg.log.Printf("ERR %s %s: invalid query", q.qt, q.ai.Name)
		return
	}

	q.st = stmts[0]
	q.roleArg = len(stmts) > 1

	q.sd, err = sg.db.Prepare(q.st.sql)
	if err != nil {
		q.err = fmt.Errorf("prepare failed: %v: %s", err, q.st.sql)
	}
}

func (sg *SuperGraph) initPrepared() error {
	if sg.allowList.IsPersist() {
		return nil
	}

	if err := sg.prepareRoleStmt(); err != nil {
		return fmt.Errorf("role query: %w", err)
	}

	sg.queries = make(map[uint64]*query)

	list, err := sg.allowList.Load()
	if err != nil {
		return err
	}

	h := maphash.Hash{}
	h.SetSeed(sg.hashSeed)

	for _, v := range list {
		if v.Query == "" {
			continue
		}

		qt := qcode.GetQType(v.Query)

		switch qt {
		case qcode.QTQuery:
			sg.queries[queryID(&h, v.Name, "user")] = &query{ai: v, qt: qt}
			sg.queries[queryID(&h, v.Name, "anon")] = &query{ai: v, qt: qt}

		case qcode.QTMutation:
			for _, role := range sg.conf.Roles {
				sg.queries[queryID(&h, v.Name, role.Name)] = &query{ai: v, qt: qt}
			}
		}
	}

	return nil
}

// nolint: errcheck
func (sg *SuperGraph) prepareRoleStmt() error {
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
		if role.Match == "" {
			continue
		}
		io.WriteString(w, ` WHEN `)
		io.WriteString(w, role.Match)
		io.WriteString(w, ` THEN '`)
		io.WriteString(w, role.Name)
		io.WriteString(w, `'`)
	}

	io.WriteString(w, ` ELSE $2 END) FROM (`)
	io.WriteString(w, rq)
	io.WriteString(w, `) AS "_sg_auth_roles_query" LIMIT 1) `)
	io.WriteString(w, `ELSE 'anon' END) FROM (VALUES (1)) AS "_sg_auth_filler" LIMIT 1; `)

	sg.getRole, err = sg.db.Prepare(w.String())
	if err != nil {
		return err
	}

	return nil
}

func (sg *SuperGraph) initAllowList() error {
	var ac allow.Config
	var err error

	if sg.conf.AllowListFile == "" {
		sg.conf.AllowListFile = "allow.list"
	}

	// When list is not enabled it is still created and
	// and new queries are saved to it.
	if !sg.conf.UseAllowList {
		ac = allow.Config{CreateIfNotExists: true, Persist: true, Log: sg.log}
	}

	sg.allowList, err = allow.New(sg.conf.AllowListFile, ac)
	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	return nil
}

// nolint: errcheck
func queryID(h *maphash.Hash, name, role string) uint64 {
	h.WriteString(name)
	h.WriteString(role)
	v := h.Sum64()
	h.Reset()

	return v
}
