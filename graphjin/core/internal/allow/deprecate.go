package allow

/*
import (
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/dosco/graphjin/v2/core/internal/graph"
	"github.com/dosco/graphjin/v2/plugin"
	"gopkg.in/yaml.v2"
)

const (
	expComment = iota + 1
	expVar
	expQuery
	expFrag
)

func itemFromYaml(fs plugin.FS, filePath string) (Item, error) {
	var item Item

	b, err := fs.ReadFile(filePath)
	if err != nil {
		return item, err
	}

	if err := yaml.Unmarshal(b, &item); err != nil {
		return item, err
	}

	h, err := graph.FastParse(item.Query)
	if err != nil {
		return item, err
	}
	item.Operation = h.Operation

	qi, err := parseQuery(item.Query)
	if err != nil {
		return item, err
	}

	for _, f := range qi.frags {
		b, err := fs.ReadFile(filepath.Join(fragmentPath, f.Name))
		if err != nil {
			return item, err
		}
		item.frags = append(item.frags, Frag{Name: f.Name, Value: string(b)})
	}

	return item, nil
}

func parseQuery(b string) (Item, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(b))
	s.Mode ^= scanner.SkipComments

	var op, sp scanner.Position
	var item Item
	var err error

	st := expComment
	period := 0
	frags := make(map[string]struct{})

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		txt := s.TokenText()

		switch {
		case strings.HasPrefix(txt, "/*"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = s.Pos()

		case strings.HasPrefix(txt, "variables"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = s.Pos()
			st = expVar

		case isGraphQL(txt):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = op
			st = expQuery

		case strings.HasPrefix(txt, "fragment"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = op
			st = expFrag

		case txt == "@":
			exp := []string{"json", "(", "schema", ":"}
			if ok := expTokens(&s, exp); !ok {
				continue
			}
			s.Scan()
			txt = s.TokenText()
			if txt == ":" {
				s.Scan()
				txt = s.TokenText()
			}
			if txt == "" {
				continue
			}
			vars, err := strconv.Unquote(txt)
			if err != nil {
				return item, err
			}
			item.Vars = strings.TrimSpace(vars)
		default:
			if period == 3 && txt != "." {
				frags[txt] = struct{}{}
			}
			if period != 3 && txt == "." {
				period++
			} else {
				period = 0
			}
		}

		if err != nil {
			return item, err
		}

		op = s.Pos()
	}

	if st == expQuery || st == expFrag {
		v := b[sp.Offset:s.Pos().Offset]
		item, err = setValue(st, v, item)
	}

	if err != nil {
		return item, err
	}

	for k := range frags {
		item.frags = append(item.frags, Frag{Name: k})
	}
	return item, nil
}

func expTokens(s *scanner.Scanner, exp []string) (ok bool) {
	for _, v := range exp {
		if tok := s.Scan(); tok == scanner.EOF {
			return
		}
		txt := s.TokenText()
		if txt != v {
			return
		}
	}
	return true
}

func setValue(st int, v string, item Item) (Item, error) {
	val := func() string {
		return strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
	}
	switch st {
	case expVar:
		item.Vars = val()

	case expQuery:
		item.Query = val()

	case expFrag:
		f := Frag{Value: val()}
		f.Name = fragmentName(f.Value)
		item.frags = append(item.frags, f)
	}

	return item, nil
}
*/
