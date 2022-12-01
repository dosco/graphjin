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
	var userVars map[string]json.RawMessage
	var qc *queryComp
	var err error

	if len(qr.vars) != 0 {
		if err := json.Unmarshal(qr.vars, &userVars); err != nil {
			return nil, fmt.Errorf("variables: %w", err)
		}
	}

	if !gj.prod || gj.conf.DisableAllowList {
		if len(qr.query) == 0 {
			item, err := gj.allowList.GetByName(qr.name)
			if err != nil {
				return nil, err
			}
			qr.query = []byte(item.Query)
		}

		st, err := gj.compileQueryForRole(qr, userVars, role)
		if err != nil {
			return nil, err
		}
		qc = &queryComp{qr: qr, st: st}

	} else {
		// In production mode enforce the allow list and
		// compile and cache the result else compile each time
		if qc, err = gj.getQuery(qr, role); err != nil {
			return nil, err
		}

		if qc, err = gj.compileQueryForRoleOnce(qc, role); err != nil {
			return nil, err
		}

		// Overwrite allow list vars with user vars
		qc.qr.vars = qr.vars
		qc.qr.ns = qr.ns
	}

	return qc, err
}

func (gj *graphjin) compileQueryForRoleOnce(qc *queryComp, role string) (*queryComp, error) {
	var err error

	if qc.st.sql != "" {
		return qc, nil
	}

	qc.Do(func() {
		var vars1 map[string]json.RawMessage

		if len(qc.qr.vars) != 0 {
			err = json.Unmarshal(qc.qr.vars, &vars1)
		}

		if err == nil {
			qc.st, err = gj.compileQueryForRole(qc.qr, vars1, role)
		}
	})

	if err != nil {
		return nil, err
	}
	return qc, nil
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

	st.va = validator.New()
	st.sql = w.String()
	return st, nil
}
