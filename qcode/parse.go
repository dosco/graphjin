package qcode

import (
	"errors"
	"fmt"

	"github.com/dosco/super-graph/util"
)

var (
	errEOT = errors.New("end of tokens")
)

type parserType int16

const (
	maxFields = 100

	parserError parserType = iota
	parserEOF
	opQuery
	opMutate
	opSub
	nodeStr
	nodeInt
	nodeFloat
	nodeBool
	nodeObj
	nodeList
	nodeVar
)

func (t parserType) String() string {
	var v string

	switch t {
	case parserEOF:
		v = "EOF"
	case parserError:
		v = "error"
	case opQuery:
		v = "query"
	case opMutate:
		v = "mutation"
	case opSub:
		v = "subscription"
	case nodeStr:
		v = "node-string"
	case nodeInt:
		v = "node-int"
	case nodeFloat:
		v = "node-float"
	case nodeBool:
		v = "node-bool"
	case nodeVar:
		v = "node-var"
	case nodeObj:
		v = "node-obj"
	case nodeList:
		v = "node-list"
	}
	return fmt.Sprintf("<%s>", v)
}

type Operation struct {
	Type   parserType
	Name   string
	Args   []*Arg
	Fields []Field
}

type Field struct {
	ID       uint16
	Name     string
	Alias    string
	Args     []*Arg
	ParentID uint16
	Children []uint16
}

type Arg struct {
	Name string
	Val  *Node
}

type Node struct {
	Type     parserType
	Name     string
	Val      string
	Parent   *Node
	Children []*Node
}

type Parser struct {
	pos   int
	items []item
	depth int
	err   error
}

func Parse(gql string) (*Operation, error) {
	if len(gql) == 0 {
		return nil, errors.New("blank query")
	}

	l, err := lex(gql)
	if err != nil {
		return nil, err
	}
	p := &Parser{
		pos:   -1,
		items: l.items,
	}
	return p.parseOp()
}

func ParseQuery(gql string) (*Operation, error) {
	return parseByType(gql, opQuery)
}

func ParseArgValue(argVal string) (*Node, error) {
	l, err := lex(argVal)
	if err != nil {
		return nil, err
	}
	p := &Parser{
		pos:   -1,
		items: l.items,
	}

	return p.parseValue()
}

func parseByType(gql string, ty parserType) (*Operation, error) {
	l, err := lex(gql)
	if err != nil {
		return nil, err
	}
	p := &Parser{
		pos:   -1,
		items: l.items,
	}
	return p.parseOpByType(ty)
}

func (p *Parser) next() item {
	n := p.pos + 1
	if n >= len(p.items) {
		p.err = errEOT
		return item{typ: itemEOF}
	}
	p.pos = n
	return p.items[p.pos]
}

func (p *Parser) ignore() {
	n := p.pos + 1
	if n >= len(p.items) {
		p.err = errEOT
		return
	}
	p.pos = n
}

func (p *Parser) current() item {
	return p.items[p.pos]
}

func (p *Parser) eof() bool {
	n := p.pos + 1
	return p.items[n].typ == itemEOF
}

func (p *Parser) peek(types ...itemType) bool {
	n := p.pos + 1
	if p.items[n].typ == itemEOF {
		return false
	}
	if n >= len(p.items) {
		return false
	}
	for i := 0; i < len(types); i++ {
		if p.items[n].typ == types[i] {
			return true
		}
	}
	return false
}

func (p *Parser) parseOpByType(ty parserType) (*Operation, error) {
	op := &Operation{Type: ty}
	var err error

	if p.peek(itemName) {
		op.Name = p.next().val
	}

	if p.peek(itemArgsOpen) {
		p.ignore()
		op.Args, err = p.parseArgs()
		if err != nil {
			return nil, err
		}
	}

	if p.peek(itemObjOpen) {
		p.ignore()
		op.Fields, err = p.parseFields()
		if err != nil {
			return nil, err
		}
	}

	if p.peek(itemObjClose) {
		p.ignore()
	}

	return op, nil
}

func (p *Parser) parseOp() (*Operation, error) {
	if p.peek(itemQuery, itemMutation, itemSub) == false {
		err := fmt.Errorf("expecting a query, mutation or subscription (not '%s')", p.next().val)
		return nil, err
	}

	item := p.next()

	switch item.typ {
	case itemQuery:
		return p.parseOpByType(opQuery)
	case itemMutation:
		return p.parseOpByType(opMutate)
	case itemSub:
		return p.parseOpByType(opSub)
	}

	return nil, errors.New("unknown operation type")
}

func (p *Parser) parseFields() ([]Field, error) {
	var id uint16

	fields := make([]Field, 0, 5)
	st := util.NewStack()

	for {
		if id >= maxFields {
			return nil, fmt.Errorf("field limit reached (%d)", maxFields)
		}

		if p.peek(itemObjClose) {
			p.ignore()
			st.Pop()

			if st.Len() == 0 {
				break
			}
			continue
		}

		if p.peek(itemName) == false {
			return nil, errors.New("expecting an alias or field name")
		}

		f := Field{ID: id}

		if err := p.parseField(&f); err != nil {
			return nil, err
		}

		if f.ID != 0 {
			intf := st.Peek()
			pid, ok := intf.(uint16)

			if !ok {
				return nil, fmt.Errorf("14: unexpected value %v (%t)", intf, intf)
			}

			f.ParentID = pid
			fields[pid].Children = append(fields[pid].Children, f.ID)
		}

		fields = append(fields, f)
		id++

		if p.peek(itemObjOpen) {
			p.ignore()
			st.Push(f.ID)
		}
	}

	return fields, nil
}

func (p *Parser) parseField(f *Field) error {
	var err error
	f.Name = p.next().val

	if p.peek(itemColon) {
		p.ignore()

		if p.peek(itemName) {
			f.Alias = f.Name
			f.Name = p.next().val
		} else {
			return errors.New("expecting an aliased field name")
		}
	}

	if p.peek(itemArgsOpen) {
		p.ignore()
		if f.Args, err = p.parseArgs(); err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) parseArgs() ([]*Arg, error) {
	var args []*Arg
	var err error

	for {
		if p.peek(itemArgsClose) {
			p.ignore()
			break
		}
		if p.peek(itemName) == false {
			return nil, errors.New("expecting an argument name")
		}
		arg := &Arg{Name: p.next().val}

		if p.peek(itemColon) == false {
			return nil, errors.New("missing ':' after argument name")
		}
		p.ignore()

		arg.Val, err = p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func (p *Parser) parseList() (*Node, error) {
	parent := &Node{}
	var nodes []*Node
	var ty parserType

	for {
		if p.peek(itemListClose) {
			p.ignore()
			break
		}
		node, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if ty == 0 {
			ty = node.Type
		} else {
			if ty != node.Type {
				return nil, errors.New("All values in a list must be of the same type")
			}
		}
		node.Parent = parent
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("List cannot be empty")
	}

	parent.Type = nodeList
	parent.Children = nodes

	return parent, nil
}

func (p *Parser) parseObj() (*Node, error) {
	parent := &Node{}
	var nodes []*Node

	for {
		if p.peek(itemObjClose) {
			p.ignore()
			break
		}

		if p.peek(itemName) == false {
			return nil, errors.New("expecting an argument name")
		}
		nodeName := p.next().val

		if p.peek(itemColon) == false {
			return nil, errors.New("missing ':' after Field argument name")
		}
		p.ignore()

		node, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.Name = nodeName
		node.Parent = parent
		nodes = append(nodes, node)
	}

	parent.Type = nodeObj
	parent.Children = nodes

	return parent, nil
}

func (p *Parser) parseValue() (*Node, error) {
	if p.peek(itemListOpen) {
		p.ignore()
		return p.parseList()
	}

	if p.peek(itemObjOpen) {
		p.ignore()
		return p.parseObj()
	}

	item := p.next()
	node := &Node{}

	switch item.typ {
	case itemIntVal:
		node.Type = nodeInt
	case itemFloatVal:
		node.Type = nodeFloat
	case itemStringVal:
		node.Type = nodeStr
	case itemBoolVal:
		node.Type = nodeBool
	case itemName:
		node.Type = nodeStr
	case itemVariable:
		node.Type = nodeVar
	default:
		return nil, fmt.Errorf("expecting a number, string, object, list or variable as an argument value (not %s)", p.next().val)
	}
	node.Val = item.val

	return node, nil
}
