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
	babel "github.com/jvatic/goja-babel"
)

type reqFunc func(map[string]interface{}) map[string]interface{}
type respFunc func(map[string]interface{}) map[string]interface{}

func (gj *GraphJin) initScripting() error {
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

func (c *gcontext) scriptCallReq(vars []byte) (_ []byte, err error) {
	if c.sc.ReqFunc == nil {
		return vars, nil
	}

	rj := make(map[string]interface{})
	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &rj); err != nil {
			return nil, err
		}
	}

	time.AfterFunc(500*time.Millisecond, func() {
		c.sc.vm.Interrupt("halt")
	})

	defer func() {
		if err1 := recover(); err1 != nil {
			err = fmt.Errorf("script: %w", err1)
		}
	}()

	val := c.sc.ReqFunc(rj)
	if val == nil {
		return vars, nil
	}

	return json.Marshal(val)
}

func (c *gcontext) scriptCallResp(data []byte) (_ []byte, err error) {
	if c.sc.RespFunc == nil {
		return data, nil
	}

	rj := make(map[string]interface{})
	if len(data) != 0 {
		if err := json.Unmarshal(data, &rj); err != nil {
			return nil, err
		}
	}

	time.AfterFunc(500*time.Millisecond, func() {
		c.sc.vm.Interrupt("halt")
	})

	defer func() {
		if err1 := recover(); err1 != nil {
			err = fmt.Errorf("script: %w", err1)
		}
	}()

	val := c.sc.RespFunc(rj)
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

	s.vm = goja.New()

	console := s.vm.NewObject()
	console.Set("log", logFunc) //nolint: errcheck
	if err := s.vm.Set("console", console); err != nil {
		return err
	}

	time.AfterFunc(500*time.Millisecond, func() {
		s.vm.Interrupt("halt")
	})

	if _, err = s.vm.RunProgram(ast); err != nil {
		return err
	}

	req := s.vm.Get("request")

	if req != nil {
		if _, ok := goja.AssertFunction(req); !ok {
			return fmt.Errorf("script: function 'request' not found")
		}

		if err := s.vm.ExportTo(req, &s.ReqFunc); err != nil {
			return err
		}
	}

	resp := s.vm.Get("response")

	if resp != nil {
		if _, ok := goja.AssertFunction(resp); !ok {
			return fmt.Errorf("script: function 'response' not found")
		}

		if err := s.vm.ExportTo(resp, &s.RespFunc); err != nil {
			return err
		}
	}
	return nil
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
