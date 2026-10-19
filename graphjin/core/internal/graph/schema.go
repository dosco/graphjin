package graph

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type Schema struct {
	Type    string
	Version string
	Schema  string
	Types   []Type
}

type Type struct {
	Name       string
	Directives []Directive
	Fields     []TField
}

type TField struct {
	Name       string
	Type       string
	Required   bool
	List       bool
	Directives []Directive
}

func ParseSchema(schema []byte) (s Schema, err error) {
	var l lexer

	if len(schema) == 0 {
		err = errors.New("empty schema")
		return
	}

	hp := `# dbinfo:`

	r := bufio.NewReader(bytes.NewReader(schema))
	for {
		var line []byte
		if line, _, err = r.ReadLine(); err != nil {
			return
		}
		h := string(line)
		if h != "" && strings.HasPrefix(h, hp) {
			v := strings.SplitN(h[len(hp):], ",", 3)
			if len(v) >= 1 {
				s.Type = v[0]
			}
			if len(v) >= 2 {
				s.Version = v[1]
			}
			if len(v) >= 3 {
				s.Schema = v[2]
			}
			break
		} else if h != "" {
			break
		}
	}

	if l, err = lex(schema); err != nil {
		return
	}

	p := Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}

	for {
		var t Type

		if p.peek(itemEOF) {
			return
		}

		if t, err = p.parseType(); err != nil {
			return
		}
		s.Types = append(s.Types, t)
	}
}

func (p *Parser) parseType() (t Type, err error) {
	if !p.peekVal(typeToken) {
		err = p.tokErr(`type`)
		return
	}
	p.ignore()

	if !p.peek(itemName) {
		err = p.tokErr(`type name`)
		return
	}
	t.Name = p.val(p.next())

	for p.peek(itemDirective) {
		p.ignore()
		if t.Directives, err = p.parseDirective(t.Directives); err != nil {
			return
		}
	}

	if !p.peek(itemObjOpen) {
		err = p.tokErr(`{`)
		return
	}
	p.ignore()

	for {
		if p.peek(itemEOF) {
			err = p.eofErr(`type ` + t.Name)
			return
		}

		if p.peek(itemObjClose) {
			p.ignore()
			return
		}

		var f TField

		if !p.peek(itemName) {
			err = p.tokErr(`field name`)
			return
		}
		f.Name = p.val(p.next())

		if !p.peek(itemColon) {
			err = p.tokErr(`:`)
			return
		}
		p.ignore()

		if p.peek(itemListOpen) {
			p.ignore()
			f.List = true
		}

		if !p.peek(itemName) {
			err = p.tokErr(`field type`)
			return
		}
		f.Type = p.val(p.next())

		if f.List {
			if !p.peek(itemListClose) {
				err = p.tokErr(`]`)
				return
			}
			p.ignore()
		}

		if p.peek(itemRequired) {
			p.ignore()
			f.Required = true
		}

		for p.peek(itemDirective) {
			p.ignore()
			if f.Directives, err = p.parseDirective(f.Directives); err != nil {
				return
			}
		}
		t.Fields = append(t.Fields, f)
	}
}

func (p *Parser) tokErr(exp string) error {
	item := p.items[p.pos+1]
	return fmt.Errorf("unexpected token '%s', expecting '%s' (line: %d, pos: %d)",
		string(item.val), exp, item.line, item.pos)
}

func (p *Parser) eofErr(tok string) error {
	item := p.items[p.pos+1]
	return fmt.Errorf("invalid %[1]s: end reached before %[1]s was closed (line: %d, pos: %d)",
		tok, item.line, item.pos)
}
