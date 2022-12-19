package js

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	plugin "github.com/dosco/graphjin/v2/plugin"
	babel "github.com/jvatic/goja-babel"
)

type reqHandler func(vars map[string]interface{},
	role string,
	userID interface{}) map[string]interface{}

type JSEngine struct {
	ready bool
}

func New() *JSEngine { return &JSEngine{} }

type JScript struct {
	vm     *goja.Runtime
	reqFn  reqHandler
	respFn reqHandler
}

func (s *JScript) HasRequestFn() bool {
	return s.reqFn != nil
}

func (s *JScript) HasResponseFn() bool {
	return s.respFn != nil
}

func (s *JScript) RequestFn(
	c context.Context,
	vars map[string]interface{},
	role string,
	userID interface{},
	gfn plugin.GraphQLFn) map[string]interface{} {

	if gfn != nil {
		if err := s.vm.Set("graphql", gfn); err != nil {
			panic(err)
		}
	}
	timer := time.AfterFunc(500*time.Millisecond, func() {
		s.vm.Interrupt("halt")
	})
	defer timer.Stop()
	return s.reqFn(vars, role, userID)
}

func (s *JScript) ReponseFn(
	c context.Context,
	vars map[string]interface{},
	role string,
	userID interface{},
	gfn plugin.GraphQLFn) map[string]interface{} {

	if gfn != nil {
		if err := s.vm.Set("graphql", gfn); err != nil {
			panic(err)
		}
	}
	timer := time.AfterFunc(500*time.Millisecond, func() {
		s.vm.Interrupt("halt")
	})
	defer timer.Stop()
	return s.respFn(vars, role, userID)
}

func (js *JSEngine) CompileScript(name, source string) (
	plugin.ScriptExecuter, error) {
	var s JScript

	if !js.ready {
		if err := babel.Init(5); err != nil {
			return nil, err
		}
		js.ready = true
	}

	es5, err := babel.Transform(strings.NewReader(source), babelOptions)
	if err != nil {
		return nil, err
	}

	var es5Code strings.Builder
	if _, err := io.Copy(&es5Code, es5); err != nil {
		return nil, err
	}

	ast, err := goja.Compile(name, es5Code.String(), true)
	if err != nil {
		return nil, err
	}

	s.vm = goja.New()
	// s.vm.ClearInterrupt()

	s.vm.SetParserOptions(parser.WithDisableSourceMaps)

	exports := s.vm.NewObject()
	if err := s.vm.Set("exports", exports); err != nil {
		return nil, err
	}

	module := s.vm.NewObject()
	if err := module.Set("exports", exports); err != nil {
		return nil, err
	}
	if err := s.vm.Set("module", module); err != nil {
		return nil, err
	}

	// env := make(map[string]string, len(os.Environ()))
	// for _, e := range os.Environ() {
	// 	if strings.HasPrefix(e, "SG_") || strings.HasPrefix(e, "GJ_") {
	// 		continue
	// 	}
	// 	v := strings.SplitN(e, "=", 2)
	// 	env[v[0]] = v[1]
	// }
	// if err := vm.Set("__ENV", env); err != nil {
	// 	return err
	// }
	if err := s.vm.Set("global", s.vm.GlobalObject()); err != nil {
		return nil, err
	}

	console := s.vm.NewObject()
	if err := console.Set("log", logFunc); err != nil {
		return nil, err
	}
	if err := s.vm.Set("console", console); err != nil {
		return nil, err
	}

	http := s.vm.NewObject()
	if err := http.Set("get", s.httpGetFunc); err != nil {
		return nil, err
	}
	if err := http.Set("post", s.httpPostFunc); err != nil {
		return nil, err
	}
	if err := http.Set("request", s.httpFunc); err != nil {
		return nil, err
	}
	if err := s.vm.Set("http", http); err != nil {
		return nil, err
	}

	timer := time.AfterFunc(500*time.Millisecond, func() {
		s.vm.Interrupt("halt")
	})
	defer timer.Stop()

	if _, err = s.vm.RunProgram(ast); err != nil {
		return nil, err
	}

	req := s.vm.Get("request")
	if req != nil {
		if _, ok := goja.AssertFunction(req); !ok {
			return nil, fmt.Errorf("function 'request' not found")
		}

		if err := s.vm.ExportTo(req, &s.reqFn); err != nil {
			return nil, err
		}
	}

	resp := s.vm.Get("response")
	if resp != nil {
		if _, ok := goja.AssertFunction(resp); !ok {
			return nil, fmt.Errorf("script: function 'response' not found")
		}

		if err := s.vm.ExportTo(resp, &s.respFn); err != nil {
			return nil, err
		}
	}

	return &s, nil
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
