package qcode

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/dosco/graphjin/v2/core/internal/graph"
	"github.com/dosco/graphjin/v2/core/internal/sdata"
)

func (co *Compiler) compileOpDirectives(qc *QCode, dirs []graph.Directive) error {
	var err error

	for _, d := range dirs {
		switch d.Name {
		case "cacheControl":
			err = co.compileDirectiveCacheControl(qc, d)

		case "script":
			err = co.compileDirectiveScript(qc, d)

		case "constraint", "validate":
			err = co.compileDirectiveConstraint(qc, d)

		case "validation":
			err = co.compileDirectiveValidation(qc, d)

		default:
			err = fmt.Errorf("unknown operation directive: %s", d.Name)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

// directives need to run before the relationship resolution code
func (co *Compiler) compileSelectorDirectives(qc *QCode,
	sel *Select, dirs []graph.Directive, role string) (err error) {
	for _, d := range dirs {
		switch d.Name {
		case "add":
			err = co.compileDirectiveAddRemove(false, sel, &sel.Field, d, role)

		case "remove":
			err = co.compileDirectiveAddRemove(true, sel, &sel.Field, d, role)

		case "include":
			err = co.compileDirectiveSkipInclude(false, sel, &sel.Field, d, role)

		case "skip":
			err = co.compileDirectiveSkipInclude(true, sel, &sel.Field, d, role)

		case "schema":
			err = co.compileDirectiveSchema(sel, d)

		case "notRelated", "not_related":
			err = co.compileDirectiveNotRelated(sel, d)

		case "through":
			err = co.compileDirectiveThrough(sel, d)

		case "object":
			sel.Singular = true
			sel.Paging.Limit = 1

		default:
			err = fmt.Errorf("no such selector directive: %s", d.Name)
		}

		if err != nil {
			return fmt.Errorf("directive @%s: %w", d.Name, err)
		}
	}
	return
}

func (co *Compiler) compileFieldDirectives(sel *Select,
	f *Field, dirs []graph.Directive, role string) (err error) {
	for _, d := range dirs {
		switch d.Name {
		case "add":
			err = co.compileDirectiveAddRemove(false, sel, f, d, role)

		case "remove":
			err = co.compileDirectiveAddRemove(true, sel, f, d, role)

		case "include":
			err = co.compileDirectiveSkipInclude(false, sel, f, d, role)

		case "skip":
			err = co.compileDirectiveSkipInclude(true, sel, f, d, role)

		default:
			err = fmt.Errorf("unknown field directive: %s", d.Name)
		}

		if err != nil {
			return fmt.Errorf("directive @%s: %w", d.Name, err)
		}
	}
	return
}

func (co *Compiler) compileDirectiveSchema(sel *Select, d graph.Directive) (err error) {
	arg, err := getArg(d.Args, "name", true, graph.NodeStr)
	if err == nil {
		sel.Schema = arg.Val.Val
	}
	return
}

func (co *Compiler) compileDirectiveAddRemove(
	remove bool,
	sel *Select,
	f *Field,
	d graph.Directive,
	role string) (err error) {

	arg, err := getArg(d.Args, "ifRole", true, graph.NodeStr, graph.NodeLabel)
	if err != nil {
		return
	}

	switch {
	case remove && arg.Val.Val == role:
		f.SkipRender = SkipTypeDrop
	case !remove && arg.Val.Val != role:
		f.SkipRender = SkipTypeDrop
	}
	return
}

func (co *Compiler) compileDirectiveSkipInclude(
	skip bool,
	sel *Select,
	f *Field,
	d graph.Directive,
	role string) (err error) {

	if len(d.Args) == 0 {
		err = fmt.Errorf("arguments 'ifVar' or 'ifRole' expected")
		return
	}

	for _, arg := range d.Args {
		switch arg.Name {
		case "ifVar", "if_var":
			if err = validateArg(arg, graph.NodeVar); err != nil {
				return
			}
			var ex *Exp
			if skip {
				ex = newExpOp(OpNotEqualsTrue)
			} else {
				ex = newExpOp(OpEqualsTrue)
			}
			ex.Right.ValType = ValVar
			ex.Right.Val = arg.Val.Val
			addAndFilter(&f.FieldFilter, ex)

			if f.Type == FieldTypeTable {
				addAndFilter(&sel.Where, ex)
			}

		case "ifRole", "if_role":
			if err = validateArg(arg, graph.NodeStr, graph.NodeLabel); err != nil {
				return
			}
			switch {
			case skip && arg.Val.Val == role:
				f.SkipRender = SkipTypeNulled
			case !skip && arg.Val.Val != role:
				f.SkipRender = SkipTypeNulled
			}

		default:
			return unknownArg(arg)
		}
	}
	return
}

func (co *Compiler) compileDirectiveCacheControl(qc *QCode, d graph.Directive) (err error) {
	var hdr []string

	if len(d.Args) == 0 {
		err = fmt.Errorf("arguments 'maxAge' or 'maxAge' expected")
		return
	}

	for _, arg := range d.Args {
		switch arg.Name {
		case "maxAge":
			if err = validateArg(arg, graph.NodeNum); err != nil {
				return
			}
			hdr = append(hdr, "max-age="+arg.Val.Val)
		case "scope":
			if err = validateArg(arg, graph.NodeStr); err != nil {
				return
			}
			hdr = append(hdr, arg.Val.Val)

		default:
			return unknownArg(arg)

		}
	}
	if len(hdr) != 0 {
		qc.Cache.Header = strings.Join(hdr, " ")
	}
	return nil
}

func (co *Compiler) compileDirectiveScript(qc *QCode, d graph.Directive) (err error) {
	if len(d.Args) == 0 {
		qc.Script.Name = (qc.Name + ".js")
		return
	}

	arg, err := getArg(d.Args, "name", false, graph.NodeStr)
	if err != nil {
		return
	}
	qc.Script.Name = arg.Val.Val

	if path.Ext(qc.Script.Name) == "" {
		qc.Script.Name += ".js"
	}
	return
}

type validator struct {
	name   string
	types  []graph.ParserType
	single bool
}

var validators = map[string]validator{
	"variable":                 {name: "variable", types: []graph.ParserType{graph.NodeStr}},
	"error":                    {name: "error", types: []graph.ParserType{graph.NodeStr}},
	"unique":                   {name: "unique", types: []graph.ParserType{graph.NodeBool}, single: true},
	"format":                   {name: "format", types: []graph.ParserType{graph.NodeStr}, single: true},
	"required":                 {name: "required", types: []graph.ParserType{graph.NodeBool}, single: true},
	"requiredIf":               {name: "required_if", types: []graph.ParserType{graph.NodeObj}},
	"requiredUnless":           {name: "required_unless", types: []graph.ParserType{graph.NodeObj}},
	"requiredWith":             {name: "required_with", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithAll":          {name: "required_with_all", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithout":          {name: "required_without", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithoutAll":       {name: "required_without_all", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"length":                   {name: "len", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"max":                      {name: "max", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"min":                      {name: "min", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"equals":                   {name: "eq", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"notEquals":                {name: "neq", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"oneOf":                    {name: "oneof", types: []graph.ParserType{graph.NodeList, graph.NodeNum, graph.NodeList, graph.NodeStr}},
	"greaterThan":              {name: "gt", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"greaterThanOrEquals":      {name: "gte", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"lessThan":                 {name: "lt", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"lessThanOrEquals":         {name: "lte", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"equalsField":              {name: "eqfield", types: []graph.ParserType{graph.NodeStr}},
	"notEqualsField":           {name: "nefield", types: []graph.ParserType{graph.NodeStr}},
	"greaterThanField":         {name: "gtfield", types: []graph.ParserType{graph.NodeStr}},
	"greaterThanOrEqualsField": {name: "gtefield", types: []graph.ParserType{graph.NodeStr}},
	"lessThanField":            {name: "ltfield", types: []graph.ParserType{graph.NodeStr}},
	"lessThanOrEqualsField":    {name: "ltefield", types: []graph.ParserType{graph.NodeStr}},
}

func (co *Compiler) compileDirectiveConstraint(qc *QCode, d graph.Directive) (err error) {
	var varName string
	var errMsg string
	var vals []string

	for _, a := range d.Args {
		switch a.Name {
		case "variable":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			varName = a.Val.Val
			continue

		case "error":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			errMsg = a.Val.Val
			continue

		case "format":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			vals = append(vals, a.Val.Val)
			continue
		}

		v, ok := validators[a.Name]
		if !ok {
			return unknownArg(a)
		}

		if err := validateConstraint(a, v); err != nil {
			return err
		}

		if v.single {
			vals = append(vals, v.name)
			continue
		}

		var value string
		switch a.Val.Type {
		case graph.NodeStr, graph.NodeNum, graph.NodeBool:
			if ifNotArgVal(a, "") {
				value = a.Val.Val
			}

		case graph.NodeObj:
			var items []string
			for _, v := range a.Val.Children {
				items = append(items, v.Name, v.Val)
			}
			value = strings.Join(items, " ")

		case graph.NodeList:
			var items []string
			for _, v := range a.Val.Children {
				items = append(items, v.Val)
			}
			value = strings.Join(items, " ")
		}

		vals = append(vals, (v.name + "=" + value))
	}

	if varName == "" {
		return errors.New("invalid @constraint no variable name specified")
	}

	if qc.Consts == nil {
		qc.Consts = make(map[string]interface{})
	}

	opt := strings.Join(vals, ",")
	if errMsg != "" {
		opt += "~" + errMsg
	}

	qc.Consts[varName] = opt
	return nil
}

func validateConstraint(a graph.Arg, v validator) error {
	list := false
	for _, t := range v.types {
		switch {
		case t == graph.NodeList:
			list = true
		case list && ifArgList(a, t):
			return nil
		case ifArg(a, t):
			return nil
		}
	}

	list = false
	err := "value must be of type: "

	for i, t := range v.types {
		if i != 0 {
			err += ", "
		}
		if !list && t == graph.NodeList {
			err += "a list of "
			list = true
		}
		err += t.String()
	}
	return errors.New(err)
}

func (co *Compiler) compileDirectiveNotRelated(sel *Select, d graph.Directive) error {
	sel.Rel.Type = sdata.RelSkip
	return nil
}

func (co *Compiler) compileDirectiveThrough(sel *Select, d graph.Directive) (err error) {
	if len(d.Args) == 0 {
		return fmt.Errorf("required argument 'table' or 'column'")
	}

	for _, a := range d.Args {
		switch a.Name {
		case "table":
			if err = validateArg(a, graph.NodeStr, graph.NodeLabel); err != nil {
				return
			}
			sel.through = a.Val.Val
			return

		case "column":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			sel.through = a.Val.Val
			return

		default:
			return unknownArg(a)
		}
	}
	return
}
func (co *Compiler) compileDirectiveValidation(qc *QCode, d graph.Directive) (err error) {
	if len(d.Args) == 0 {
		return fmt.Errorf("required arguments 'src' and 'type'")
	}

	for _, a := range d.Args {
		switch a.Name {
		case "src", "source":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			qc.Validation.Source = a.Val.Val

		case "type", "lang":
			if err = validateArg(a, graph.NodeStr); err != nil {
				return
			}
			qc.Validation.Type = a.Val.Val

		default:
			return unknownArg(a)
		}
	}
	return
}
