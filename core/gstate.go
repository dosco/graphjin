package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/dosco/graphjin/v2/core/internal/psql"
	"github.com/dosco/graphjin/v2/core/internal/qcode"
	plugin "github.com/dosco/graphjin/v2/plugin"
)

type gstate struct {
	gj    *graphjin
	r     graphqlReq
	cs    *cstate
	vmap  map[string]json.RawMessage
	data  []byte
	dhash [sha256.Size]byte
	role  string
	verrs []qcode.ValidErr
}

type cstate struct {
	sync.Once
	st  stmt
	err error
}

type stmt struct {
	role string
	roc  *Role
	qc   *qcode.QCode
	md   psql.Metadata
	sql  string
}

func newGState(c context.Context, gj *graphjin, r graphqlReq) (s gstate, err error) {
	s.gj = gj
	s.r = r

	if v, ok := c.Value(UserRoleKey).(string); ok {
		s.role = v
	} else {
		switch c.Value(UserIDKey).(type) {
		case string, int:
			s.role = "user"
		default:
			s.role = "anon"
		}
	}

	// convert variable json to a go map also decrypted encrypted values
	if len(r.vars) != 0 {
		var vars json.RawMessage
		vars, err = decryptValues(r.vars, decPrefix, s.gj.encKey)
		if err != nil {
			return
		}

		s.vmap = make(map[string]json.RawMessage, 5)
		if err = json.Unmarshal(vars, &s.vmap); err != nil {
			return
		}
	}
	return
}

func (s *gstate) compile() (err error) {
	if !s.gj.prodSec {
		err = s.compileQueryForRole()
		return
	}

	// In production mode and compile and cache the result
	// In production mode the query is derived from the allow list
	err = s.compileQueryForRoleOnce()
	return
}

func (s *gstate) compileQueryForRoleOnce() (err error) {
	val, loaded := s.gj.queries.LoadOrStore(s.key(), &cstate{})
	s.cs = val.(*cstate)
	err = s.cs.err

	if loaded {
		return
	}

	s.cs.Do(func() {
		err = s.compileQueryForRole()
		s.cs.err = err
	})
	return
}

func (s *gstate) compileQueryForRole() (err error) {
	st := stmt{role: s.role}

	var ok bool
	if st.roc, ok = s.gj.roles[s.role]; !ok {
		err = fmt.Errorf(`roles '%s' not defined in c.gj.config`, s.role)
		return
	}

	var vars map[string]json.RawMessage
	if len(s.r.aschema) != 0 { // compile in prod (once)
		vars = s.r.aschema
	} else { // compiling in dev
		vars = s.vmap
	}

	if st.qc, err = s.gj.qc.Compile(
		s.r.query,
		vars,
		s.role,
		s.r.ns); err != nil {
		return
	}

	var w bytes.Buffer

	if st.md, err = s.gj.pc.Compile(&w, st.qc); err != nil {
		return
	}

	if st.qc.Validation.Source != "" {
		vc, ok := s.gj.validatorMap[st.qc.Validation.Type]
		if !ok {
			err = fmt.Errorf("no validator found for '%s'", st.qc.Validation.Type)
			return
		}

		var ve plugin.ValidationExecuter
		ve, err = vc.CompileValidation(st.qc.Validation.Source)
		if err != nil {
			return
		}
		st.qc.Validation.VE = ve
		st.qc.Validation.Exists = true
	}

	if st.qc.Script.Name != "" {
		if err = s.gj.loadScript(st.qc); err != nil {
			return
		}
	}

	st.sql = w.String()

	if s.cs == nil {
		s.cs = &cstate{st: st}
	} else {
		// s.cs.r = s.r
		s.cs.st = st
	}

	return
}

func (s *gstate) compileAndExecuteWrapper(c context.Context) (err error) {
	if err = s.compileAndExecute(c); err != nil {
		return
	}

	if s.gj.conf.Debug {
		s.debugLogStmt()
	}

	if len(s.data) == 0 {
		return
	}

	cs := s.cs

	if cs.st.qc.Remotes != 0 {
		if err = s.execRemoteJoin(c); err != nil {
			return
		}
	}

	qc := cs.st.qc

	if qc.Script.Exists && qc.Script.HasRespFn() {
		err = s.scriptCallResp(c)
	}
	return
}

func (s *gstate) compileAndExecute(c context.Context) (err error) {
	var conn *sql.Conn

	if s.tx() == nil {
		// get a new database connection
		c1, span1 := s.gj.spanStart(c, "Get Connection")
		defer span1.End()

		err = retryOperation(c1, func() (err1 error) {
			conn, err1 = s.gj.db.Conn(c1)
			return
		})
		if err != nil {
			span1.Error(err)
			return
		}
		defer conn.Close()
	}

	// set the local user id on the connection if needed
	if s.gj.conf.SetUserID {
		c1, span2 := s.gj.spanStart(c, "Set Local User ID")
		defer span2.End()

		err = retryOperation(c1, func() (err1 error) {
			return s.setLocalUserID(c1, conn)
		})
		if err != nil {
			span2.Error(err)
			return
		}
	}
	if s.role == "user" && s.gj.abacEnabled {
		if err = s.executeRoleQuery(c, conn); err != nil {
			return
		}
	}

	// compile query for the role
	if err = s.compile(); err != nil {
		return
	}

	// set default variables
	s.setDefaultVars()

	// execute query
	err = s.execute(c, conn)
	return
}

func (s *gstate) setDefaultVars() {
	if vlen := len(s.cs.st.qc.Vars); vlen != 0 && s.vmap == nil {
		s.vmap = make(map[string]json.RawMessage, vlen)
	}

	for _, v := range s.cs.st.qc.Vars {
		s.vmap[v.Name] = v.Val
	}
}

func (s *gstate) execute(c context.Context, conn *sql.Conn) (err error) {
	if err = s.validateAndUpdateVars(c); err != nil {
		return
	}

	var args args
	if args, err = s.argList(c); err != nil {
		return
	}

	cs := s.cs

	c1, span := s.gj.spanStart(c, "Execute Query")
	defer span.End()

	err = retryOperation(c1, func() (err1 error) {
		var row *sql.Row
		if tx := s.tx(); tx != nil {
			row = tx.QueryRowContext(c1, cs.st.sql, args.values...)
		} else {
			row = conn.QueryRowContext(c1, cs.st.sql, args.values...)
		}
		return row.Scan(&s.data)
	})

	if err != nil && err != sql.ErrNoRows {
		span.Error(err)
	}

	if span.IsRecording() {
		span.SetAttributesString(
			stringAttr{"query.namespace", s.r.ns},
			stringAttr{"query.operation", cs.st.qc.Type.String()},
			stringAttr{"query.name", cs.st.qc.Name},
			stringAttr{"query.role", cs.st.role})
	}

	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return
	}

	s.dhash = sha256.Sum256(s.data)

	s.data, err = encryptValues(s.data,
		s.gj.pf, decPrefix, s.dhash[:], s.gj.encKey)

	return
}

func (s *gstate) executeRoleQuery(c context.Context, conn *sql.Conn) (err error) {
	s.role, err = s.gj.executeRoleQuery(c, conn, s.vmap, s.r.rc)
	return
}

func (s *gstate) argList(c context.Context) (args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, s.vmap, s.r.rc, false)
	return
}

func (s *gstate) argListForSub(c context.Context,
	vmap map[string]json.RawMessage,
) (args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, vmap, s.r.rc, true)
	return
}

func (s *gstate) setLocalUserID(c context.Context, conn *sql.Conn) (err error) {
	if v := c.Value(UserIDKey); v == nil {
		return nil
	} else {
		var q string
		switch v1 := v.(type) {
		case string:
			q = `SET SESSION "user.id" = '` + v1 + `'`
		case int:
			q = `SET SESSION "user.id" = ` + strconv.Itoa(v1)
		}
		if tx := s.tx(); tx != nil {
			_, err = tx.ExecContext(c, q)
		} else {
			_, err = conn.ExecContext(c, q)
		}
	}
	return
}

var errValidationFailed = errors.New("validation failed")

func (s *gstate) validateAndUpdateVars(c context.Context) (err error) {
	cs := s.cs
	qc := cs.st.qc

	if qc == nil {
		return nil
	}

	if len(qc.Consts) != 0 {
		s.verrs = qc.ProcessConstraints(s.vmap)
		if len(s.verrs) != 0 {
			err = errValidationFailed
			return
		}
	}

	if qc.Validation.Exists {
		err = qc.Validation.VE.Validate(s.r.vars)
		if err != nil {
			return
		}
	}

	if qc.Script.Exists && qc.Script.HasReqFn() {
		var v []byte
		var vars map[string]interface{}
		if vars, err = fromVMap(s.vmap); err != nil {
			return
		}
		if v, err = s.scriptCallReq(c, qc, vars, s.role); err != nil {
			return
		}
		if s.vmap, err = toVMap(v); err != nil {
			return
		}
	}
	return
}

func fromVMap(vmap map[string]json.RawMessage) (
	vars map[string]interface{}, err error,
) {
	vars = make(map[string]interface{}, len(vmap))
	for k, v := range vmap {
		var v1 interface{}
		if err = json.Unmarshal(v, &v1); err != nil {
			return
		}
		vars[k] = v1
	}
	return
}

func toVMap(vars json.RawMessage) (
	vmap map[string]json.RawMessage, err error,
) {
	vmap = make(map[string]json.RawMessage)
	if err = json.Unmarshal(vars, &vmap); err != nil {
		return
	}
	return
}

func (s *gstate) sql() (sql string) {
	if s.cs != nil && s.cs.st.qc != nil {
		sql = s.cs.st.sql
	}
	return
}

func (s *gstate) cacheHeader() (ch string) {
	if s.cs != nil && s.cs.st.qc != nil {
		ch = s.cs.st.qc.Cache.Header
	}
	return
}

func (s *gstate) qcode() (qc *qcode.QCode) {
	if s.cs != nil {
		qc = s.cs.st.qc
	}
	return
}

func (s *gstate) tx() (tx *sql.Tx) {
	if s.r.rc != nil {
		tx = s.r.rc.Tx
	}
	return
}

func (s *gstate) key() (key string) {
	key = s.r.ns + s.r.name + s.role
	return
}
