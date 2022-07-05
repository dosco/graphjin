package graph

import (
	"errors"
	"strings"
	"text/scanner"
)

func FastParse(gql string) (Header, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(gql))
	s.Whitespace ^= 1 << '\n' // don't skip new lines

	var h Header

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

		if h.Type == 0 {
			if n == 0 && t == "{" {
				h.Type = OpQuery
				return h, nil
			}

			switch t {
			case "query":
				h.Type = OpQuery
			case "mutation":
				h.Type = OpMutate
			case "subscription":
				h.Type = OpSub
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
