package qcode

import (
	"encoding/json"
	"errors"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

type Constraint struct {
	VarName string
	fns     []constFn
}

type constFn struct {
	name string
	fn   ValidFn
}

type Vars map[string]json.RawMessage

type (
	ValidFn    func(Vars, Constraint) bool
	NewValidFn func(args []string) (fn ValidFn, err error)
)

type Validator struct {
	Description string
	Type        string
	List        bool
	Types       []graph.ParserType
	NewFn       NewValidFn
}

var ErrUnknownValidator = errors.New("unknown validator")

func (co *Compiler) newConstraint(varName string, dargs []graph.Arg) (con Constraint, err error) {
	con = Constraint{VarName: varName}

	for _, a := range dargs {
		if a.Name == "variable" {
			continue
		}

		v, ok := co.c.Validators[a.Name]
		if !ok {
			err = ErrUnknownValidator
			return
		}

		if err = validateArg(a, v.Types...); err != nil {
			return
		}

		var args []string

		switch a.Val.Type {
		case graph.NodeStr:
			args = []string{quoteStr(a.Val.Val)}

		case graph.NodeNum, graph.NodeBool:
			args = []string{a.Val.Val}

		case graph.NodeObj:
			for _, v := range a.Val.Children {
				if v.Type == graph.NodeStr {
					// wrap so we don't have to unwrap at checktime
					args = append(args, v.Name, quoteStr(v.Val))
				} else {
					args = append(args, v.Name, v.Val)
				}
			}

		case graph.NodeList:
			for _, v := range a.Val.Children {
				if v.Type == graph.NodeStr {
					// wrap so we don't have to unwrap at checktime
					args = append(args, quoteStr(v.Val))
				} else {
					args = append(args, v.Val)
				}
			}
		}

		fn := constFn{name: a.Name}
		if fn.fn, err = v.NewFn(args); err != nil {
			return
		}
		con.fns = append(con.fns, fn)
	}
	return
}

func quoteStr(v string) string {
	return `"` + v + `"`
}

type ValidErr struct {
	FieldName  string `json:"field_name"`
	Constraint string `json:"constraint"`
}

func (qc *QCode) ProcessConstraints(vmap map[string]json.RawMessage) (errs []ValidErr) {
	for _, c := range qc.Consts {
		if err := validate(vmap, c); err != nil {
			errs = append(errs, err...)
		}
	}
	return
}

func validate(vmap map[string]json.RawMessage, c Constraint) (errs []ValidErr) {
	for _, fn := range c.fns {
		if ok := fn.fn(vmap, c); !ok {
			err := ValidErr{
				FieldName:  c.VarName,
				Constraint: fn.name,
			}
			errs = append(errs, err)
		}
	}
	return
}
