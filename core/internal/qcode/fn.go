package qcode

import (
	"fmt"
	"strings"
)

var stdFuncs = []string{
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
	"lower_",
	"length_",
	"array_agg_",
	"json_agg_",
	"unnest_",
}

func (co *Compiler) isFunction(sel *Select, fname, alias string) (Function, bool, error) {
	var cn string
	var agg bool
	var err error

	fn := Function{FieldName: fname, Alias: alias}

	switch {
	case fname == "search_rank":
		fn.Name = "search_rank"

		if _, ok := sel.Args["search"]; !ok {
			return fn, false, fmt.Errorf("no search defined: %s", fname)
		}

	case strings.HasPrefix(fname, "search_headline_"):
		fn.Name = "search_headline"
		cn = fname[16:]

		if _, ok := sel.Args["search"]; !ok {
			return fn, false, fmt.Errorf("no search defined: %s", fname)
		}

	case fname == "__typename":
		sel.Typename = true
		fn.skip = true

	case strings.HasSuffix(fname, "_cursor"):
		fn.skip = true

	default:
		n := co.funcPrefixLen(fname)
		if n != 0 {
			cn = fname[n:]
			fn.Name = fname[:(n - 1)]
			agg = true
		}
	}

	if cn != "" {
		fn.Col, err = sel.Ti.GetColumn(cn)
	}

	return fn, agg, err
}

func (co *Compiler) funcPrefixLen(col string) int {
	if !co.c.DisableAgg {
		for _, v := range stdFuncs {
			if strings.HasPrefix(col, v) {
				return len(v)
			}
		}
	}
	fnLen := len(col)

	for k := range co.s.GetFunctions() {
		kLen := len(k)
		if kLen < fnLen && k[0] == col[0] && strings.HasPrefix(col, k) && col[kLen] == '_' {
			return kLen + 1
		}
	}

	return 0
}
