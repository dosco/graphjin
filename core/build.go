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
	var err error
	qcomp := &queryComp{qr: qr}

	if !gj.prod || gj.conf.DisableAllowList {
		var userVars map[string]json.RawMessage

		if len(qr.vars) != 0 {
			if err := json.Unmarshal(qr.vars, &userVars); err != nil {
				return nil, fmt.Errorf("variables: %w", err)
			}
		}

		qcomp.st, err = gj.compileQueryForRole(qr, userVars, role)
		if err != nil {
			return nil, err
		}

	} else {
		// In production mode enforce the allow list and
		// compile and cache the result else compile each time
		// the allowlist queries are already loaded at init.
		// if qcomp, err = gj.getQuery(qr, role); err != nil {
		// 	return nil, err
		// }
		if qcomp, err = gj.compileQueryForRoleOnce(qcomp, role); err != nil {
			return nil, err
		}

		// Overwrite allow list vars with user vars
		// qcomp.qr.vars = qr.vars
		// qcomp.qr.ns = qr.ns
	}
	return qcomp, err
}

func (gj *graphjin) compileQueryForRoleOnce(qcomp *queryComp, role string) (*queryComp, error) {
	var err error

	qr := qcomp.qr
	val, loaded := gj.queries.LoadOrStore((qr.ns + qr.name + role), qcomp)
	if loaded {
		return val.(*queryComp), nil
	}

	qcomp.Do(func() {
		var vars1 map[string]json.RawMessage

		if len(qcomp.qr.vars) != 0 {
			err = json.Unmarshal(qcomp.qr.vars, &vars1)
		}

		if err == nil {
			qcomp.st, err = gj.compileQueryForRole(qcomp.qr, vars1, role)
		}
	})
	if err != nil {
		return nil, err
	}
	return qcomp, nil
}

func (gj *graphjin) compileQueryForRole(
	qr queryReq, vm map[string]json.RawMessage, role string) (stmt, error) {

	var st stmt
	var err error
	var ok bool

	if st.role, ok = gj.roles[role]; !ok {
		return st, fmt.Errorf(`roles '%s' not defined in c.gj.config`, role)
	}

	if st.qc, err = gj.qc.Compile(qr.query, vm, st.role.Name, qr.ns); err != nil {
		return st, err
	}

	var w bytes.Buffer

	if st.md, err = gj.pc.Compile(&w, st.qc); err != nil {
		return st, err
	}

	if st.qc.Validation.Source != "" {
		vc, ok := gj.validatorMap[st.qc.Validation.Type]
		if !ok {
			return st, fmt.Errorf("no validator found for '%s'", st.qc.Validation.Type)
		}
		ve, err := vc.CompileValidation(st.qc.Validation.Source)
		if err != nil {
			return st, err
		}
		st.qc.Validation.VE = ve
	}

	if st.qc.Script.Name != "" {
		if err := gj.loadScript(st.qc); err != nil {
			return st, err
		}
	}

	st.va = validator.New()
	st.sql = w.String()
	return st, nil
}
