package qcode

import (
	"fmt"
	"regexp"
	"strings"
)

func NewBlacklist(list []string) *regexp.Regexp {
	var bl *regexp.Regexp

	if len(list) != 0 {
		re := fmt.Sprintf("(?i)%s", strings.Join(list, "|"))
		bl = regexp.MustCompile(re)
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
