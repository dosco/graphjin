package config

import (
	"os"
	"regexp"
	"strings"
	"unicode"
)

var (
	varRe1 = regexp.MustCompile(`(?mi)\$([a-zA-Z0-9_.]+)`)
	varRe2 = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)
)

func sanitize(s string) string {
	s0 := varRe1.ReplaceAllString(s, `{{$1}}`)

	s1 := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s0)

	return varRe2.ReplaceAllStringFunc(s1, func(m string) string {
		return strings.ToLower(m)
	})
}

func GetConfigName() string {
	if len(os.Getenv("GO_ENV")) == 0 {
		return "dev"
	}

	ge := strings.ToLower(os.Getenv("GO_ENV"))

	switch {
	case strings.HasPrefix(ge, "pro"):
		return "prod"

	case strings.HasPrefix(ge, "sta"):
		return "stage"

	case strings.HasPrefix(ge, "tes"):
		return "test"

	case strings.HasPrefix(ge, "dev"):
		return "dev"
	}

	return ge
}
