package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	"github.com/dosco/graphjin/core/internal/qcode"
	babel "github.com/jvatic/goja-babel"
)

type reqFunc func(map[string]interface{}, string, interface{}) map[string]interface{}
type respFunc func(map[string]interface{}, string, interface{}) map[string]interface{}

func (gj *graphjin) initScripting() error {
	if err := babel.Init(5); err != nil {
		return err
	}
	gj.scripts = sync.Map{}
	return nil
}

func (c *gcontext) loadScript(name string) error {
	var err error

	sv, _ := c.gj.scripts.LoadOrStore(name, &script{})
	c.sc = sv.(*script)

	c.sc.Do(func() {
		err = c.scriptInit(c.sc, name)
	})

	if err != nil {
		c.sc.Reset()
		return err
	}

	if !c.gj.conf.Production {
		c.sc.Reset()
	}

	return nil
}

func (c *gcontext) scriptCallReq(vars []byte, role string) (_ []byte, err error) {
	if c.sc.ReqFunc == nil {
		return vars, nil
	}

	rj := make(map[string]interface{})
	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &rj); err != nil {
			return nil, err
		}
	}

	timer := time.AfterFunc(500*time.Millisecond, func() {
		c.sc.vm.Interrupt("halt")
	})
	defer timer.Stop()

	defer func() {
		if err1 := recover(); err1 != nil {
			err = fmt.Errorf("script: %w", err1)
		}
	}()

	if err := c.sc.vm.Set("graphql", c.newGraphQLFunc(role)); err != nil {
		return nil, err
	}

	var userID interface{}
	if v := c.Value(UserIDKey); v != nil {
		userID = v
	}

	val := c.sc.ReqFunc(rj, role, userID)
	if val == nil {
		return vars, nil
	}

	return json.Marshal(val)
}

func (c *gcontext) scriptCallResp(data []byte, role string) (_ []byte, err error) {
	if c.sc.RespFunc == nil {
		return data, nil
	}

	rj := make(map[string]interface{})
	if len(data) != 0 {
		if err := json.Unmarshal(data, &rj); err != nil {
			return nil, err
		}
	}

	timer := time.AfterFunc(500*time.Millisecond, func() {
		c.sc.vm.Interrupt("halt")
	})
	defer timer.Stop()

	if err := c.sc.vm.Set("graphql", c.newGraphQLFunc(role)); err != nil {
		return nil, err
	}

	var userID interface{}
	if v := c.Value(UserIDKey); v != nil {
		userID = v
	}

	defer func() {
		if err1 := recover(); err1 != nil {
			err = fmt.Errorf("script: %w", err1)
		}
	}()

	val := c.sc.RespFunc(rj, role, userID)
	if val == nil {
		return data, nil
	}

	return json.Marshal(val)
}

func (c *gcontext) scriptInit(s *script, name string) error {
	var spath string

	if c.gj.conf.ScriptPath != "" {
		spath = c.gj.conf.ScriptPath
	} else {
		spath = path.Join(c.gj.conf.ConfigPath, "scripts")
	}

	file, err := os.Open(path.Join(spath, name))
	if err != nil {
		return err
	}

	es5, err := babel.Transform(file, babelOptions)
	if err != nil {
		return err
	}

	es5Code := new(strings.Builder)
	if _, err := io.Copy(es5Code, es5); err != nil {
		return err
	}

	ast, err := goja.Compile(name, es5Code.String(), true)
	if err != nil {
		return err
	}

	var vm *goja.Runtime

	if s.vm != nil {
		s.vm.ClearInterrupt()
		vm = s.vm
	} else {
		vm = goja.New()

		vm.SetParserOptions(parser.WithDisableSourceMaps)

		exports := vm.NewObject()
		vm.Set("exports", exports) //nolint: errcheck

		module := vm.NewObject()
		_ = module.Set("exports", exports)
		vm.Set("module", module) //nolint: errcheck

		env := make(map[string]string, len(os.Environ()))
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "SG_") || strings.HasPrefix(e, "GJ_") {
				continue
			}
			v := strings.SplitN(e, "=", 2)
			env[v[0]] = v[1]
		}
		vm.Set("__ENV", env)                //nolint: errcheck
		vm.Set("global", vm.GlobalObject()) //nolint: errcheck

		console := vm.NewObject()
		console.Set("log", logFunc) //nolint: errcheck
		vm.Set("console", console)  //nolint: errcheck

		http := vm.NewObject()
		http.Set("get", c.httpGetFunc)   //nolint: errcheck
		http.Set("post", c.httpPostFunc) //nolint: errcheck
		http.Set("request", c.httpFunc)  //nolint: errcheck
		vm.Set("http", http)             //nolint: errcheck
	}

	timer := time.AfterFunc(500*time.Millisecond, func() {
		vm.Interrupt("halt")
	})
	defer timer.Stop()

	if _, err = vm.RunProgram(ast); err != nil {
		return err
	}

	req := vm.Get("request")

	if req != nil {
		if _, ok := goja.AssertFunction(req); !ok {
			return fmt.Errorf("script: function 'request' not found")
		}

		if err := vm.ExportTo(req, &s.ReqFunc); err != nil {
			return err
		}
	}

	resp := vm.Get("response")

	if resp != nil {
		if _, ok := goja.AssertFunction(resp); !ok {
			return fmt.Errorf("script: function 'response' not found")
		}

		if err := vm.ExportTo(resp, &s.RespFunc); err != nil {
			return err
		}
	}

	s.vm = vm
	return nil
}

func (c *gcontext) newGraphQLFunc(role string) func(string, map[string]interface{}, map[string]string) map[string]interface{} {

	return func(
		query string,
		vars map[string]interface{},
		opt map[string]string) map[string]interface{} {
		var err error

		op, name := qcode.GetQType(query)

		qreq := queryReq{
			op:    op,
			name:  name,
			query: []byte(query),
		}

		ct := gcontext{
			Context: c.Context,
			gj:      c.gj,
			op:      c.op,
			rc:      c.rc,
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

		qres, err := ct.execQuery(qreq, r1)
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

func logFunc(args ...interface{}) {
	for _, arg := range args {
		if _, ok := arg.(map[string]interface{}); ok {
			j, err := json.MarshalIndent(arg, "", "  ")
			if err != nil {
				continue
			}
			os.Stdout.Write(j) //nolint: errcheck
		} else {
			io.WriteString(os.Stdout, fmt.Sprintf("%v", arg)) //nolint: errcheck
		}

		io.WriteString(os.Stdout, "\n") //nolint: errcheck
	}
}

var babelOptions = map[string]interface{}{
	"plugins": []string{
		"proposal-async-generator-functions",
		"proposal-class-properties",
		"proposal-dynamic-import",
		"proposal-json-strings",
		"proposal-nullish-coalescing-operator",
		"proposal-numeric-separator",
		"proposal-object-rest-spread",
		"proposal-optional-catch-binding",
		"proposal-optional-chaining",
		"proposal-private-methods",
		"proposal-unicode-property-regex",
		"syntax-async-generators",
		"syntax-class-properties",
		// "syntax-dynamic-import",
		// "syntax-json-strings",
		// "syntax-nullish-coalescing-operator",
		// "syntax-numeric-separator",
		"syntax-object-rest-spread",
		"syntax-optional-catch-binding",
		// "syntax-optional-chaining",
		"syntax-top-level-await",
		"transform-arrow-functions",
		"transform-async-to-generator",
		"transform-block-scoped-functions",
		"transform-block-scoping",
		"transform-classes",
		"transform-computed-properties",
		"transform-destructuring",
		"transform-dotall-regex",
		"transform-duplicate-keys",
		"transform-exponentiation-operator",
		"transform-for-of",
		"transform-function-name",
		"transform-literals",
		"transform-member-expression-literals",
		// "transform-modules-amd",
		"transform-modules-commonjs",
		// "transform-modules-systemjs",
		// "transform-modules-umd",
		"transform-named-capturing-groups-regex",
		"transform-new-target",
		"transform-object-super",
		"transform-parameters",
		"transform-property-literals",
		"transform-regenerator",
		"transform-reserved-words",
		"transform-shorthand-properties",
		"transform-spread",
		"transform-sticky-regex",
		"transform-template-literals",
		"transform-typeof-symbol",
		"transform-unicode-escapes",
		"transform-unicode-regex",
	},

	"retainLines": true,
}
