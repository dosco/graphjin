package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"github.com/dosco/graphjin/v2/core/internal/graph"
	"github.com/dosco/graphjin/v2/core/internal/qcode"
)

func (gj *graphjin) loadScript(qc *qcode.QCode) error {
	name := qc.Script.Name

	fn := func() error {
		ext := path.Ext(name)
		se, ok := gj.scriptMap[ext]
		if !ok {
			err := fmt.Errorf("no script execution engine defined for '%s', extension", ext)
			return err
		}

		src, err := gj.readScriptSource(name)
		if err != nil {
			return err
		}

		qc.Script.SC, err = se.CompileScript(name, src)
		if err != nil {
			return err
		}
		qc.Script.Exists = true

		return nil
	}

	if err := fn(); err != nil {
		return err
	}

	return nil
}

func (gj *graphjin) readScriptSource(name string) (string, error) {
	src, err := gj.fs.ReadFile(filepath.Join("scripts", name))
	if err != nil {
		return "", err
	}
	return string(src), nil
}

func (s *gstate) scriptCallReq(ctx context.Context,
	qc *qcode.QCode,
	vars map[string]interface{},
	role string) ([]byte, error) {

	defer func() {
		// nolint: errcheck
		recover()
	}()

	var userID interface{}
	if v := ctx.Value(UserIDKey); v != nil {
		userID = v
	}

	ctx1, span := s.gj.spanStart(ctx, "Execute Request Script")
	gfn := s.newGraphQLFunc(ctx1)

	val := qc.Script.SC.RequestFn(ctx1, vars, role, userID, gfn)
	if val == nil {
		err := errors.New("error excuting script")
		span.Error(err)
		return nil, nil
	}
	span.End()

	return json.Marshal(val)
}

func (s *gstate) scriptCallResp(c context.Context) (err error) {
	defer func() {
		// nolint: errcheck
		recover()
	}()

	rj := make(map[string]interface{})
	if len(s.data) != 0 {
		if err = json.Unmarshal(s.data, &rj); err != nil {
			return
		}
	}

	var userID interface{}
	if v := c.Value(UserIDKey); v != nil {
		userID = v
	}

	defer func() {
		// nolint: errcheck
		recover()
	}()

	c1, span := s.gj.spanStart(c, "Execute Response Script")
	gfn := s.newGraphQLFunc(c1)

	val := s.cs.st.qc.Script.SC.ReponseFn(c1, rj, s.role, userID, gfn)
	if val == nil {
		err = errors.New("error excuting script")
		span.Error(err)
		return
	}
	span.End()

	s.data, err = json.Marshal(val)
	return
}

func (s *gstate) newGraphQLFunc(c context.Context) func(string, map[string]interface{}, map[string]string) map[string]interface{} {
	return func(
		query string,
		vars map[string]interface{},
		opt map[string]string) map[string]interface{} {
		var err error

		h, err := graph.FastParse(query)
		if err != nil {
			panic(err)
		}

		r := s.gj.newGraphqlReq(s.r.rc,
			h.Operation,
			h.Name,
			[]byte(query),
			nil)

		if len(vars) != 0 {
			if r.vars, err = json.Marshal(vars); err != nil {
				panic(fmt.Errorf("variables: %s", err))
			}
		}

		s := newGState(s.gj, r, s.role)

		if v, ok := opt["role"]; ok && v != "" {
			s.role = v
		}

		err = s.compileAndExecuteWrapper(c)

		if err != nil {
			panic(err)
		}

		jres := make(map[string]interface{})
		if err = json.Unmarshal(s.data, &jres); err != nil {
			panic(fmt.Errorf("json: %s", err))
		}

		return jres
	}
}
