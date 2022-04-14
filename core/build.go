package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/go-playground/validator/v10"
)

type queryComp struct {
	sync.Once
	qr queryReq
	st stmt
}

type stmt struct {
	role *Role
	qc   *qcode.QCode
	md   psql.Metadata
	va   *validator.Validate
	sql  string
}

func (gj *graphjin) compileQuery(qr queryReq, role string) (*queryComp, error) {
	var qc *queryComp
	var err error

	var varsFromUser map[string]json.RawMessage

	if len(qr.vars) != 0 {
		if err := json.Unmarshal(qr.vars, &varsFromUser); err != nil {
			return nil, fmt.Errorf("variables: %w", err)
		}
	}

	if gj.allowList == nil || !gj.prod {
		st, err := gj.compileQueryRole(qr, varsFromUser, role)
		if err != nil {
			return nil, err
		}
		st.va = validator.New()
		return &queryComp{qr: qr, st: st}, nil
	}

	// In production mode enforce the allow list and
	// compile and cache the result else compile each time
	if qc, err = gj.getQuery(qr, role, varsFromUser); qc == nil {
		return nil, err
	}

	var varsFromAllowList map[string]json.RawMessage

	if len(qc.qr.vars) != 0 {
		if err := json.Unmarshal(qc.qr.vars, &varsFromAllowList); err != nil {
			return nil, fmt.Errorf("variables: %w", err)
		}
	}

	if qc.st.sql == "" {
		qc.Do(func() {
			qc.st, err = gj.compileQueryRole(qc.qr, varsFromAllowList, role)
			qc.st.va = validator.New()
		})
	}

	// Overwrite allow list vars with user vars
	qc.qr.vars = qr.vars
	qc.qr.ns = qr.ns
	return qc, err
}

func (gj *graphjin) compileQueryRole(
	qr queryReq, vm map[string]json.RawMessage, role string) (stmt, error) {

	var st stmt
	var err error
	var ok bool

	if st.role, ok = gj.roles[role]; !ok {
		return st, fmt.Errorf(`roles '%s' not defined in c.gj.config`, role)
	}

	if qr.order[0] != "" {
		vm[qr.order[0]] = json.RawMessage(qr.order[1])
	}

	if st.qc, err = gj.qc.Compile(qr.query, vm, st.role.Name, qr.ns); err != nil {
		return st, err
	}

	var w bytes.Buffer

	if st.md, err = gj.pc.Compile(&w, st.qc); err != nil {
		return st, err
	}

	st.sql = w.String()
	return st, nil
}
