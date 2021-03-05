package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
)

type stmt struct {
	role *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func (gj *GraphJin) compileQuery(cq *cquery, role string) error {
	var err error

	// In production mode enforce the allow list and
	// compile and cache the result else compile each time
	if gj.allowList != nil && gj.prod {
		if cq1, ok := gj.queries[(cq.q.name + role)]; ok {
			cq.q = cq1.q
		} else {
			return errNotFound
		}

		if cq.st.sql == "" {
			cq.Do(func() {
				err = gj.compileQueryFn(cq, role)
			})
		}

	} else {
		err = gj.compileQueryFn(cq, role)
	}

	return err
}

func (gj *GraphJin) compileQueryFn(cq *cquery, role string) error {
	var err error

	switch cq.q.op {
	case qcode.QTQuery, qcode.QTSubscription, qcode.QTMutation:
		err = gj.buildRoleStmt(cq, role)

	default:
		err = errors.New("unknown query")
	}

	cq.roleArg = (len(cq.stmts) > 0)
	return err
}

func (gj *GraphJin) buildRoleStmt(cq *cquery, role string) error {
	query := cq.q.query
	vars := cq.q.vars

	ro, ok := gj.roles[role]
	if !ok {
		return fmt.Errorf(`roles '%s' not defined in c.gj.config`, role)
	}

	var vm map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &vm); err != nil {
			return fmt.Errorf("variables: %w", err)
		}
	}

	qc, err := gj.qc.Compile(query, vm, ro.Name)
	if err != nil {
		return err
	}

	var w bytes.Buffer

	cq.st.md, err = gj.pc.Compile(&w, qc)
	if err != nil {
		return err
	}

	cq.st.role = ro
	cq.st.qc = qc
	cq.st.sql = w.String()

	return nil
}
