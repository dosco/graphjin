//go:generate stringer -type=ParserType,FieldType -output=./gen_string.go
package graph

import (
	"errors"
	"fmt"
	"hash/maphash"
	"sync"
	"unsafe"
)

var (
	errEOT = errors.New("end of tokens")
)

const (
	maxFields = 1200
	maxArgs   = 25
)

type ParserType int8

const (
	parserError ParserType = iota
	parserEOF
	OpQuery
	OpMutate
	OpSub
	NodeStr
	NodeNum
	NodeBool
	NodeObj
	NodeList
	NodeVar
)

type FieldType int8

const (
	FieldUnion FieldType = iota + 1
	FieldMember
	FieldKeyword
)

type Operation struct {
	Type    ParserType
	Name    string
	Args    []Arg
	argsA   [10]Arg
	Fields  []Field
	fieldsA [10]Field
}

type Fragment struct {
	Name    string
	On      string
	Fields  []Field
	fieldsA [10]Field
}

type Field struct {
	ID         int32
	ParentID   int32
	Type       FieldType
	Name       string
	Alias      string
	Args       []Arg
	argsA      [5]Arg
	Directives []Directive
	Children   []int32
	childrenA  [5]int32
}

type Arg struct {
	Name string
	Val  *Node
}

type Directive struct {
	Name string
	Args []Arg
}

type Node struct {
	Type     ParserType
	Name     string
	Val      string
	Parent   *Node
	Children []*Node
}

var nodePool = sync.Pool{
	New: func() interface{} { return new(Node) },
}

var zeroNode = Node{}

func (n *Node) Reset() {
	*n = zeroNode
}

func (n *Node) Free() {
	nodePool.Put(n)
}

type Parser struct {
	frags map[string]Fragment
	ff    func(name string) (string, error)
	h     maphash.Hash
	input []byte // the string being scanned
	pos   int
	items []item
	err   error
}

func Parse(gql []byte, fetchFrag func(name string) (string, error)) (Operation, error) {
	var l lexer
	var op Operation
	var err error

	if len(gql) == 0 {
		return op, errors.New("blank query")
	}

	if l, err = lex(gql); err != nil {
		return op, err
	}

	p := Parser{
		ff:    fetchFrag,
		input: l.input,
		pos:   -1,
		items: l.items,
	}
	op.Fields = op.fieldsA[:0]

	s := -1
	qf := false

	for {
		if p.peek(itemEOF) {
			p.ignore()
			break
		}

		if p.peek(itemFragment) {
			p.ignore()
			if _, err = p.parseFragment(); err != nil {
				return op, err
			}

		} else {
			if !qf && p.peek(itemQuery, itemMutation, itemSub, itemObjOpen) {
				s = p.pos
				qf = true
			}
			p.ignore()
		}
	}

	p.reset(s)
	if op, err = p.parseOp(); err != nil {
		return op, err
	}

	for i, f := range op.Fields {
		if f.ParentID == -1 && len(f.Args) == 0 && len(f.Children) == 0 {
			op.Fields[i].Type = FieldKeyword
		}
	}
	return op, nil
}

func (p *Parser) parseFragment() (Fragment, error) {
	var err error
	var frag Fragment

	if p.peek(itemName) {
		frag.Name = p.val(p.next())
	} else {
		return frag, errors.New("fragment: missing name")
	}

	if p.peek(itemOn) {
		p.ignore()
	} else {
		return frag, errors.New("fragment: missing 'on' keyword")
	}

	if p.peek(itemName) {
		frag.On = p.vall(p.next())
	} else {
		return frag, errors.New("fragment: missing table name after 'on' keyword")
	}

	if p.peek(itemObjOpen) {
		p.ignore()
	} else {
		return frag, fmt.Errorf("fragment: expecting a '{', got: %s", p.next())
	}

	frag.Fields, err = p.parseFields(frag.Fields)
	if err != nil {
		return frag, fmt.Errorf("fragment: %v", err)
	}

	if p.frags == nil {
		p.frags = make(map[string]Fragment)
	}

	p.frags[frag.Name] = frag
	return frag, nil
}

func (p *Parser) parseOp() (Operation, error) {
	var err error
	var typeSet bool
	var op Operation

	if p.peek(itemQuery, itemMutation, itemSub) {
		if err = p.parseOpTypeAndArgs(&op); err != nil {
			return op, fmt.Errorf("%s: %v", op.Type, err)
		}
		typeSet = true
	}

	if p.peek(itemObjOpen) {
		p.ignore()
		if !typeSet {
			op.Type = OpQuery
		}

		for {
			if p.peek(itemEOF, itemFragment) {
				p.ignore()
				break
			}

			op.Fields, err = p.parseFields(op.Fields)
			if err != nil {
				return op, fmt.Errorf("%s: %v", op.Type, err)
			}
		}
	} else {
		return op, fmt.Errorf("expecting a query, mutation or subscription, got: %s", p.next())
	}
	return op, nil
}

func (p *Parser) parseOpTypeAndArgs(op *Operation) error {
	item := p.next()

	switch item._type {
	case itemQuery:
		op.Type = OpQuery
	case itemMutation:
		op.Type = OpMutate
	case itemSub:
		op.Type = OpSub
	}

	op.Args = op.argsA[:0]

	var err error

	if p.peek(itemName) {
		op.Name = p.val(p.next())
	}

	if p.peek(itemArgsOpen) {
		p.ignore()

		op.Args, err = p.parseOpParams(op.Args)
		if err != nil {
			return err
		}
	}

	return nil
}

func ParseArgValue(argVal string) (*Node, error) {
	l, err := lex([]byte(argVal))
	if err != nil {
		return nil, err
	}

	p := Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}
	return p.parseValue()
}

func ParseFragment(fragment string) (Fragment, error) {
	var f Fragment
	var err error

	l, err := lex([]byte(fragment))
	if err != nil {
		return f, err
	}

	p := Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}

	if p.peek(itemFragment) {
		p.ignore()
		f, err = p.parseFragment()
	}
	return f, err
}

func (p *Parser) parseFields(fields []Field) ([]Field, error) {
	var err error
	st := NewStack()

	if !p.peek(itemName, itemSpread) {
		return nil, fmt.Errorf("unexpected token: %s", p.peekNext())
	}

	for {
		if p.peek(itemEOF) {
			p.ignore()
			return nil, errors.New("invalid query")
		}

		if p.peek(itemObjClose) {
			p.ignore()

			if st.Len() != 0 {
				st.Pop()
				continue
			} else {
				break
			}
		}

		if len(fields) >= maxFields {
			return nil, fmt.Errorf("too many fields (max %d)", maxFields)
		}

		isFrag := false

		if p.peek(itemSpread) {
			p.ignore()
			isFrag = true
		}

		if isFrag {
			fields, err = p.parseFragmentFields(st, fields)
		} else {
			fields, err = p.parseNormalFields(st, fields)
		}

		if err != nil {
			return nil, err
		}
	}

	return fields, nil
}

func (p *Parser) parseNormalFields(st *Stack, fields []Field) ([]Field, error) {
	if !p.peek(itemName) {
		return nil, fmt.Errorf("expecting an alias or field name, got: %s", p.next())
	}

	fields = append(fields, Field{ID: int32(len(fields))})

	f := &fields[(len(fields) - 1)]
	f.Args = f.argsA[:0]
	f.Children = f.childrenA[:0]

	// Parse the field
	if err := p.parseField(f); err != nil {
		return nil, err
	}

	if st.Len() == 0 {
		f.ParentID = -1
	} else {
		pid := st.Peek()
		f.ParentID = pid
		fields[pid].Children = append(fields[pid].Children, f.ID)
	}

	// The first opening curley brackets after this
	// comes the columns or child fields
	if p.peek(itemObjOpen) {
		p.ignore()
		st.Push(f.ID)
	}

	return fields, nil
}

func (p *Parser) parseFragmentFields(st *Stack, fields []Field) ([]Field, error) {
	var err error
	var fr Fragment
	var ok bool

	pid := st.Peek()

	if p.peek(itemOn) {
		p.ignore()
		fields[pid].Type = FieldUnion

		if fields, err = p.parseNormalFields(st, fields); err != nil {
			return nil, err
		}

		// If parent is a union selector than copy over args from the parent
		// to the first child which is the root selector for each union type.
		for i := pid + 1; i < int32(len(fields)); i++ {
			f := &fields[i]
			if f.ParentID == pid {
				f.Args = fields[pid].Args
				f.Type = FieldMember
			}
		}

	} else {
		if !p.peek(itemName) {
			return nil, fmt.Errorf("expecting a fragment name, got: %s", p.next())
		}

		name := p.val(p.next())

		if p.ff != nil {
			fval, err := p.ff(name)
			if err != nil {
				return nil, fmt.Errorf("fragment not found: %s", name)
			}
			if fr, err = ParseFragment(fval); err != nil {
				return nil, err
			}
		} else {
			fr, ok = p.frags[name]
			if !ok {
				return nil, fmt.Errorf("fragment not defined: %s", name)
			}
		}

		ff := fr.Fields

		n := int32(len(fields))
		fields = append(fields, ff...)

		for i := 0; i < len(ff); i++ {
			k := (n + int32(i))
			f := &fields[k]
			f.ID = int32(k)

			// Nothing to do here if fields was originally empty
			if n != 0 {
				// If this is the top-level, point the parent to the parent of the
				// previous field.
				if f.ParentID == -1 {
					f.ParentID = pid
					fields[pid].Children = append(fields[pid].Children, f.ID)

					// Update all the other parents id's by our new place in this new array
				} else {
					f.ParentID += n
				}
			}

			// Copy over children since fields append is not a deep copy
			f.Children = make([]int32, len(f.Children))
			copy(f.Children, ff[i].Children)

			// Copy over args since args append is not a deep copy
			f.Args = make([]Arg, len(f.Args))
			copy(f.Args, ff[i].Args)

			// Update all the children which is needed.
			for j := range f.Children {
				f.Children[j] += n
			}
		}
	}

	return fields, nil
}

func (p *Parser) parseField(f *Field) error {
	var err error
	v := p.next()

	if p.peek(itemColon) {
		p.ignore()

		if p.peek(itemName) {
			f.Alias = p.val(v)
			f.Name = p.vall(p.next())
		} else {
			return errors.New("expecting an aliased field name")
		}
	} else {
		f.Name = p.vall(v)
	}

	if p.peek(itemArgsOpen) {
		p.ignore()
		if f.Args, err = p.parseArgs(f.Args); err != nil {
			return err
		}
	}

	if p.peek(itemDirective) {
		p.ignore()
		if f.Directives, err = p.parseDirective(f.Directives); err != nil {
			return err
		}

	}

	return nil
}

func (p *Parser) parseOpParams(args []Arg) ([]Arg, error) {
	for {
		if len(args) >= maxArgs {
			return nil, fmt.Errorf("too many args (max %d)", maxArgs)
		}

		if p.peek(itemEOF, itemArgsClose) {
			p.ignore()
			break
		}
		p.next()
	}

	return args, nil
}

func (p *Parser) parseArgs(args []Arg) ([]Arg, error) {
	var err error

	for {
		if len(args) >= maxArgs {
			return nil, fmt.Errorf("too many args (max %d)", maxArgs)
		}

		if p.peek(itemEOF, itemArgsClose) {
			p.ignore()
			break
		}

		if !p.peek(itemName) {
			return nil, fmt.Errorf("expecting an argument name got: %s", p.peekNext())
		}
		args = append(args, Arg{Name: p.val(p.next())})
		arg := &args[(len(args) - 1)]

		if !p.peek(itemColon) {
			return nil, errors.New("missing ':' after argument name")
		}
		p.ignore()

		arg.Val, err = p.parseValue()
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

func (p *Parser) parseDirective(dirs []Directive) ([]Directive, error) {
	var err error
	var d Directive

	if p.peek(itemName) {
		d.Name = p.vall(p.next())
	} else {
		return nil, fmt.Errorf("expecting directive name after @ symbol got: %s", p.peekNext())
	}

	if p.peek(itemArgsOpen) {
		p.ignore()
		if d.Args, err = p.parseArgs(d.Args); err != nil {
			return nil, err
		}
	}

	return append(dirs, d), nil
}

func (p *Parser) parseList() (*Node, error) {
	nodes := []*Node{}

	parent := nodePool.Get().(*Node)
	parent.Reset()

	var ty ParserType
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
		} else if ty != node.Type {
			return nil, errors.New("All values in a list must be of the same type")
		}
		node.Parent = parent
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("List cannot be empty")
	}

	parent.Type = NodeList
	parent.Children = nodes

	return parent, nil
}

func (p *Parser) parseObj() (*Node, error) {
	nodes := []*Node{}

	parent := nodePool.Get().(*Node)
	parent.Reset()

	for {
		if p.peek(itemEOF, itemObjClose) {
			p.ignore()
			break
		}

		if !p.peek(itemName) {
			return nil, errors.New("expecting an argument name")
		}
		nodeName := p.val(p.next())

		if !p.peek(itemColon) {
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

	parent.Type = NodeObj
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
	node := nodePool.Get().(*Node)
	node.Reset()

	switch item._type {
	case itemNumberVal:
		node.Type = NodeNum
	case itemStringVal:
		node.Type = NodeStr
	case itemBoolVal:
		node.Type = NodeBool
	case itemName:
		node.Type = NodeStr
	case itemVariable:
		node.Type = NodeVar
	default:
		return nil, fmt.Errorf("expecting a number, string, object, list or variable as an argument value (not %s)", p.val(p.next()))
	}
	node.Val = p.val(item)

	return node, nil
}

func (p *Parser) val(v item) string {
	return b2s(v.val)
}

func (p *Parser) vall(v item) string {
	lowercase(v.val)
	return b2s(v.val)
}

func (p *Parser) peek(types ...MType) bool {
	n := p.pos + 1
	l := len(types)
	// if p.items[n]._type == itemEOF {
	// 	return false
	// }

	if n >= len(p.items) {
		return types[0] == itemEOF
	}

	if l == 1 {
		return p.items[n]._type == types[0]
	}

	for i := 0; i < l; i++ {
		if p.items[n]._type == types[i] {
			return true
		}
	}
	return false
}

func (p *Parser) next() item {
	n := p.pos + 1
	if n >= len(p.items) {
		p.err = errEOT
		return item{_type: itemEOF}
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

func (p *Parser) peekNext() string {
	item := p.items[p.pos+1]
	return b2s(item.val)
}

func (p *Parser) reset(to int) {
	p.pos = to
}

func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func FreeNode(n *Node) {
	nodePool.Put(n)
}
