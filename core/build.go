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
	var ok bool

	var vm map[string]json.RawMessage

	if len(qr.vars) != 0 {
		if err := json.Unmarshal(qr.vars, &vm); err != nil {
			return nil, fmt.Errorf("variables: %w", err)
		}
	}

	if gj.allowList == nil || !gj.prod {
		st, err := gj.compileQueryRole(qr, vm, role)
		if err != nil {
			return nil, err
		}
		st.va = validator.New()
		return &queryComp{qr: qr, st: st}, nil
	}

	// In production mode enforce the allow list and
	// compile and cache the result else compile each time
	if qc, ok = gj.queries[(qr.ns + qr.name + role)]; !ok {
		return nil, errNotFound
	}
	ov := qc.qr.order[0]

	// If order variable is set
	if ov != "" {
		if qc, err = gj.orderQuery(ov, qc, vm, role); err != nil {
			return nil, err
		}
	}

	if qc.st.sql == "" {
		qc.Do(func() {
			qc.st, err = gj.compileQueryRole(qc.qr, vm, role)
			qc.st.va = validator.New()
		})
	}

	return qc, err
}

func (gj *graphjin) orderQuery(
	ov string,
	qc *queryComp,
	vm map[string]json.RawMessage,
	role string) (*queryComp, error) {

	var oval string

	v, ok := vm[ov]
	if !ok || v[0] != '"' || len(v) == 2 {
		return nil, fmt.Errorf("required variable not set: %s", ov)
	}
	oval = string(v[1:(len(v) - 1)])

	if qc, ok := gj.queries[(qc.qr.ns + qc.qr.name + role + oval)]; ok {
		return qc, nil
	} else {
		return nil, fmt.Errorf("invalid value for variable (%s): %s", ov, oval)
	}
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
