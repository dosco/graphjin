package graph

import (
	"errors"
	"strings"
	"text/scanner"
)

type FPInfo struct {
	Operation string
	Name      string
}

func FastParse(gql string) (h FPInfo, err error) {
	if gql == "" {
		return h, errors.New("query missing or empty")
	}
	var s scanner.Scanner
	s.Init(strings.NewReader(gql))
	s.Whitespace ^= 1 << '\n' // don't skip new lines

	comment := false
	n := 0

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		t := s.TokenText()

		switch {
		case t == "#":
			comment = true
			continue
		case t == "\n":
			comment = false
			continue
		case comment:
			continue
		}

		if h.Operation == "" {
			if n == 0 && t == "{" {
				h.Operation = "query"
				return
			}

			if t == "query" || t == "mutation" || t == "subscription" {
				h.Operation = t
			}

		} else {
			if t != "{" && t != "(" && t != "@" {
				h.Name = t
			}
			return h, nil
		}
		n++
	}

	return h, errors.New("invalid query: query type and name not found")
}
