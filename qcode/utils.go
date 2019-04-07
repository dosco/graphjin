package qcode

import (
	"strings"
)

func NewBlacklist(list []string) Blacklist {
	bl := make(map[string]struct{}, len(list))

	for i := range list {
		bl[strings.ToLower(list[i])] = struct{}{}
	}
	return bl
}

func NewFilterMap(filters map[string]string) FilterMap {
	fm := make(FilterMap)

	for k, v := range filters {
		fil, err := CompileFilter(v)
		if err != nil {
			panic(err)
		}
		key := strings.ToLower(k)
		fm[key] = fil
	}
	return fm
}
