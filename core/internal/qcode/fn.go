package qcode

import (
	"fmt"
	"strings"
)

func (co *Compiler) isFunction(sel *Select, fname string) (Function, bool, error) {
	var cn string
	var agg bool
	var err error

	fn := Function{FieldName: fname}

	switch {
	case fname == "search_rank":
		fn.Name = "search_rank"
		fn.Col = sel.Ti.TSVCol

		if fn.Col.Name == "" {
			return fn, false, fmt.Errorf("no tsvector column found: %s", fname)
		}
		if _, ok := sel.ArgMap["search"]; !ok {
			return fn, false, fmt.Errorf("not a search query: %s", fname)
		}

	case strings.HasPrefix(fname, "search_headline_"):
		fn.Name = "search_headline"
		fn.Col = sel.Ti.TSVCol
		cn = fname[16:]

		if fn.Col.Name == "" {
			return fn, false, fmt.Errorf("no tsvector column found: %s", fname)
		}
		if _, ok := sel.ArgMap["search"]; !ok {
			return fn, false, fmt.Errorf("not a search query: %s", fname)
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
	switch {
	case strings.HasPrefix(col, "avg_"):
		return 4
	case strings.HasPrefix(col, "count_"):
		return 6
	case strings.HasPrefix(col, "max_"):
		return 4
	case strings.HasPrefix(col, "min_"):
		return 4
	case strings.HasPrefix(col, "sum_"):
		return 4
	case strings.HasPrefix(col, "stddev_"):
		return 7
	case strings.HasPrefix(col, "stddev_pop_"):
		return 11
	case strings.HasPrefix(col, "stddev_samp_"):
		return 12
	case strings.HasPrefix(col, "variance_"):
		return 9
	case strings.HasPrefix(col, "var_pop_"):
		return 8
	case strings.HasPrefix(col, "var_samp_"):
		return 9
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
