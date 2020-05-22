package qcode

import (
	"regexp"
	"sort"
	"strings"
)

type Config struct {
	Blocklist    []string
	DefaultBlock bool
}

type QueryConfig struct {
	Limit            int
	Filters          []string
	Columns          []string
	DisableFunctions bool
	Block            bool
}

type InsertConfig struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type UpdateConfig struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type DeleteConfig struct {
	Filters []string
	Columns []string
	Block   bool
}

type TRConfig struct {
	ReadOnly bool
	Query    QueryConfig
	Insert   InsertConfig
	Update   UpdateConfig
	Delete   DeleteConfig
}

type trval struct {
	readOnly bool
	query    struct {
		limit   string
		fil     *Exp
		filNU   bool
		cols    map[string]struct{}
		disable struct{ funcs bool }
		block   bool
	}

	insert struct {
		fil    *Exp
		filNU  bool
		cols   map[string]struct{}
		psmap  map[string]string
		pslist []string
		block  bool
	}

	update struct {
		fil    *Exp
		filNU  bool
		cols   map[string]struct{}
		psmap  map[string]string
		pslist []string
		block  bool
	}

	delete struct {
		fil   *Exp
		filNU bool
		cols  map[string]struct{}
		block bool
	}
}

func (trv *trval) allowedColumns(qt QType) map[string]struct{} {
	switch qt {
	case QTQuery:
		return trv.query.cols
	case QTInsert:
		return trv.insert.cols
	case QTUpdate:
		return trv.update.cols
	case QTDelete:
		return trv.delete.cols
	case QTUpsert:
		return trv.insert.cols
	}

	return nil
}

func (trv *trval) filter(qt QType) (*Exp, bool) {
	switch qt {
	case QTQuery:
		return trv.query.fil, trv.query.filNU
	case QTInsert:
		return trv.insert.fil, trv.insert.filNU
	case QTUpdate:
		return trv.update.fil, trv.update.filNU
	case QTDelete:
		return trv.delete.fil, trv.delete.filNU
	case QTUpsert:
		return trv.insert.fil, trv.insert.filNU
	}

	return nil, false
}

func listToMap(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for i := range list {
		m[strings.ToLower(list[i])] = struct{}{}
	}
	return m
}

func mapToList(m map[string]string) []string {
	list := []string{}
	for k := range m {
		list = append(list, strings.ToLower(k))
	}
	sort.Strings(list)
	return list
}

var varRe = regexp.MustCompile(`\$([a-zA-Z0-9_]+)`)

func parsePresets(m map[string]string) map[string]string {
	for k, v := range m {
		m[k] = varRe.ReplaceAllString(v, `{{$1}}`)
	}
	return m
}
