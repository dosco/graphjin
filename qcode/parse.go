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
	Fields []*Field
}

type Field struct {
	Name     string
	Alias    string
	Args     []*Arg
	Parent   *Field
	Children []*Field
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

func (p *Parser) parseFields() ([]*Field, error) {
	var roots []*Field
	st := util.NewStack()

	for {
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

		field, err := p.parseField()
		if err != nil {
			return nil, err
		}

		if st.Len() == 0 {
			roots = append(roots, field)

		} else {
			intf := st.Peek()
			parent, ok := intf.(*Field)
			if !ok || parent == nil {
				return nil, fmt.Errorf("unexpected value encountered %v", intf)
			}
			field.Parent = parent
			parent.Children = append(parent.Children, field)
		}

		if p.peek(itemObjOpen) {
			p.ignore()
			st.Push(field)
		}
	}

	return roots, nil
}

func (p *Parser) parseField() (*Field, error) {
	var err error
	field := &Field{Name: p.next().val}

	if p.peek(itemColon) {
		p.ignore()

		if p.peek(itemName) {
			field.Alias = field.Name
			field.Name = p.next().val
		} else {
			return nil, errors.New("expecting an aliased field name")
		}
	}

	if p.peek(itemArgsOpen) {
		p.ignore()
		if field.Args, err = p.parseArgs(); err != nil {
			return nil, err
		}
	}

	return field, nil
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

func (p *Parser) parseList(parent *Node) ([]*Node, error) {
	var nodes []*Node
	var ty parserType

	if parent == nil {
		return nil, errors.New("list needs a parent")
	}

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
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("List cannot be empty")
	}

	parent.Type = nodeList
	parent.Children = nodes

	return nodes, nil
}

func (p *Parser) parseObj(parent *Node) ([]*Node, error) {
	var nodes []*Node

	if parent == nil {
		return nil, errors.New("object needs a parent")
	}

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

	return nodes, nil
}

func (p *Parser) parseValue() (*Node, error) {
	node := &Node{}

	var done bool
	var err error

	if p.peek(itemListOpen) {
		p.ignore()
		node.Children, err = p.parseList(node)
		done = true
	}

	if p.peek(itemObjOpen) {
		p.ignore()
		node.Children, err = p.parseObj(node)
		done = true
	}

	if err != nil {
		return nil, err
	}

	if !done {
		item := p.next()

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
	}

	return node, nil
}
