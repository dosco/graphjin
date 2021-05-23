package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/internal/util"
)

func init() {
	for _, v := range stdFuncSK {
		stdFuncCK = append(stdFuncCK, util.ToCamel(v[:len(v)-1]))
	}
}

var stdFuncSK = []string{
	"avg_",
	"count_",
	"max_",
	"min_",
	"sum_",
	"stddev_",
	"stddev_pop_",
	"stddev_samp_",
	"variance_",
	"var_pop_",
	"var_samp_",
	"length_",
	"lower_",
	"length_",
}

var stdFuncCK = []string{}

func (co *Compiler) isFunction(sel *Select, fname, alias string) (Function, bool, error) {
	var cn string
	var agg bool
	var err error

	fn := Function{FieldName: fname, Alias: alias}

	switch {
	case fname == "search_rank" || fname == "searchRank":
		fn.Name = "search_rank"

		if _, ok := sel.Args["search"]; !ok {
			return fn, false, fmt.Errorf("no search defined: %s", fname)
		}

	case strings.HasPrefix(fname, "search_headline_") || strings.HasPrefix(fname, "searchHeadline"):
		fn.Name = "search_headline"
		cn = fname[16:]

		if _, ok := sel.Args["search"]; !ok {
			return fn, false, fmt.Errorf("no search defined: %s", fname)
		}

	case fname == "__typename":
		sel.Typename = true
		fn.skip = true

	case strings.HasSuffix(fname, "_cursor") || strings.HasSuffix(fname, "Cursor"):
		fn.skip = true

	default:
		n, trimSuffix := co.funcPrefixLen(fname)
		if n != 0 {
			if trimSuffix {
				fn.Name = fname[:(n - 1)]
				cn = fname[n:]
			} else {
				fn.Name = fname[:n]
				cn = fname[n+1:]
			}
			agg = true
		}
	}

	if cn != "" {
		fn.Col, err = sel.Ti.GetColumn(cn)
	}

	return fn, agg, err
}

func (co *Compiler) funcPrefixLen(col string) (int, bool) {
	if co.c.EnableCamelcase {
		return co._funcPrefixLen(col, stdFuncCK, false)
	} else {
		return co._funcPrefixLen(col, stdFuncSK, true)
	}
}

func (co *Compiler) _funcPrefixLen(col string, stdFuncs []string, hasSuffix bool) (int, bool) {
	if co.c.DisableFuncs {
		return 0, false
	}

	if !co.c.DisableAgg {
		for _, v := range stdFuncs {
			if strings.HasPrefix(col, v) {
				return len(v), hasSuffix
			}
		}
	}
	fnLen := len(col)

	for k := range co.s.GetFunctions() {
		kLen := len(k)
		isFunc := kLen < fnLen && k[0] == col[0] && strings.HasPrefix(col, k)
		if isFunc && hasSuffix && col[kLen] == '_' {
			return kLen + 1, hasSuffix
		}
		if isFunc && !hasSuffix {
			return kLen, hasSuffix
		}
	}

	return 0, false
}
