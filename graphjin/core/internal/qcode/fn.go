package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (co *Compiler) isFunction(sel *Select, f graph.Field) (
	fn Function, isFunc bool, err error) {

	fn.FieldName = f.Name
	fn.Alias = f.Alias

	switch {
	case f.Name == "search_rank":
		isFunc = true
		if _, ok := sel.GetInternalArg("search"); !ok {
			err = fmt.Errorf("no search defined: %s", f.Name)
		}

	case strings.HasPrefix(f.Name, "search_headline_"):
		isFunc = true
		fn.Name = "search_headline"
		fn.Args = []Arg{{Type: ArgTypeCol}}
		fn.Args[0].Col, err = sel.Ti.GetColumn(f.Name[(len(fn.Name) + 1):])
		if err != nil {
			return
		}
		if _, ok := sel.GetInternalArg("search"); !ok {
			err = fmt.Errorf("no search defined: %s", f.Name)
		}

	default:
		var fi funcInfo
		if fi, isFunc, err = co.isFunctionEx(sel, f); isFunc {
			fn.Name = fi.Name
			fn.Func = fi.Func
			fn.Agg = fi.Agg
			if fi.Col.Name != "" {
				fn.Args = []Arg{{Type: ArgTypeCol, Col: fi.Col}}
			}
			isFunc = true
		} else {
			return fn, false, err
		}
	}

	if co.c.DisableAgg && fn.Agg {
		err = fmt.Errorf("aggreation disabled: db function '%s' cannot be used", fn.Name)
	}

	return
}

type funcInfo struct {
	Name string
	Func sdata.DBFunction
	Col  sdata.DBColumn
	Agg  bool
}

func (co *Compiler) isFunctionEx(sel *Select, f graph.Field) (
	fi funcInfo, isFunc bool, err error) {
	fieldName := f.Name

	for k, v := range co.s.GetFunctions() {
		if k == fieldName && len(f.Args) != 0 {
			fi.Name = k
			fi.Agg = false
			fi.Func = v
			isFunc = true
			return
		}

		kLen := len(k)
		if strings.HasPrefix(fieldName, (k + "_")) {
			fi.Name = fieldName[:kLen]
			fi.Col, err = sel.Ti.GetColumn(fieldName[(kLen + 1):])
			if err != nil {
				return
			}
			fi.Agg = true
			fi.Func = v
			isFunc = true
			return
		}
	}

	return
}
