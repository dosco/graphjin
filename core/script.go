package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/qcode"
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

		return nil
	}

	if err := fn(); err != nil {
		return err
	}

	return nil
}

func (gj *graphjin) readScriptSource(name string) (string, error) {
	var spath string
	if gj.conf.ScriptPath != "" {
		spath = gj.conf.ScriptPath
	} else {
		spath = path.Join(gj.conf.ConfigPath, "scripts")
	}

	src, err := os.ReadFile(path.Join(spath, name))
	if err != nil {
		return "", err
	}
	return string(src), nil
}

func (c *gcontext) scriptCallReq(ctx context.Context, qc *qcode.QCode,
	vars map[string]interface{}, role string) (
	[]byte, error) {
	defer func() {
		// nolint: errcheck
		recover()
	}()

	var userID interface{}
	if v := ctx.Value(UserIDKey); v != nil {
		userID = v
	}

	ctx1, span := c.gj.spanStart(ctx, "Execute Request Script")
	gfn := c.newGraphQLFunc(ctx1, role)

	val := qc.Script.SC.RequestFn(ctx1, vars, role, userID, gfn)
	if val == nil {
		err := errors.New("error excuting script")
		spanError(span, err)
		return nil, nil
	}
	span.End()

	return json.Marshal(val)
}

func (c *gcontext) scriptCallResp(ctx context.Context, qc *qcode.QCode,
	data []byte, role string) (_ []byte, err error) {
	defer func() {
		// nolint: errcheck
		recover()
	}()

	rj := make(map[string]interface{})
	if len(data) != 0 {
		if err := json.Unmarshal(data, &rj); err != nil {
			return nil, err
		}
	}

	var userID interface{}
	if v := ctx.Value(UserIDKey); v != nil {
		userID = v
	}

	defer func() {
		// nolint: errcheck
		recover()
	}()

	ctx1, span := c.gj.spanStart(ctx, "Execute Response Script")
	gfn := c.newGraphQLFunc(ctx1, role)

	val := qc.Script.SC.ReponseFn(ctx1, rj, role, userID, gfn)
	if val == nil {
		err := errors.New("error excuting script")
		spanError(span, err)
		return data, nil
	}
	span.End()

	return json.Marshal(val)
}

func (c *gcontext) newGraphQLFunc(ctx context.Context, role string) func(string, map[string]interface{}, map[string]string) map[string]interface{} {
	return func(
		query string,
		vars map[string]interface{},
		opt map[string]string) map[string]interface{} {
		var err error

		h, err := graph.FastParse(query)
		if err != nil {
			panic(err)
		}
		op := qcode.GetQTypeByName(h.Operation)
		name := h.Name

		qreq := queryReq{
			op:    op,
			name:  name,
			query: []byte(query),
		}

		ct := gcontext{
			gj:   c.gj,
			rc:   c.rc,
			op:   op,
			name: name,
		}

		if len(vars) != 0 {
			if qreq.vars, err = json.Marshal(vars); err != nil {
				panic(fmt.Errorf("variables: %s", err))
			}
		}

		var r1 string

		if v, ok := opt["role"]; ok && len(v) != 0 {
			r1 = v
		} else {
			r1 = role
		}

		qres, err := ct.execQuery(ctx, qreq, r1)
		if err != nil {
			panic(err)
		}

		jres := make(map[string]interface{})
		if err = json.Unmarshal(qres.data, &jres); err != nil {
			panic(fmt.Errorf("json: %s", err))
		}

		return jres
	}
}
