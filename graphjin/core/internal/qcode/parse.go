package qcode

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

type parserType int32

const (
	maxFields = 1200
	maxArgs   = 25
)

const (
	parserError parserType = iota
	parserEOF
	opQuery
	opMutate
	opSub
	NodeStr
	NodeInt
	NodeFloat
	NodeBool
	NodeObj
	NodeList
	NodeVar
)

type SelectionSet struct {
	Name    string
	Args    []Arg
	argsA   [10]Arg
	Fields  []Field
	fieldsA [10]Field
}

type Operation struct {
	Type parserType
	SelectionSet
}

var zeroOperation = Operation{}

func (o *Operation) Reset() {
	*o = zeroOperation
}

type Fragment struct {
	Name string
	On   string
	SelectionSet
}

var zeroFragment = Fragment{}

func (f *Fragment) Reset() {
	*f = zeroFragment
}

type Field struct {
	ID        int32
	ParentID  int32
	Name      string
	Alias     string
	Args      []Arg
	argsA     [5]Arg
	Children  []int32
	childrenA [5]int32
}

type Arg struct {
	Name string
	Val  *Node
	df   bool
}

type Node struct {
	Type     parserType
	Name     string
	Val      string
	Parent   *Node
	Children []*Node
	exp      *Exp
}

var zeroNode = Node{}

func (n *Node) Reset() {
	*n = zeroNode
}

type Parser struct {
	frags map[uint64]*Fragment
	h     maphash.Hash
	input []byte // the string being scanned
	pos   int
	items []item
	err   error
}

var nodePool = sync.Pool{
	New: func() interface{} { return new(Node) },
}

var opPool = sync.Pool{
	New: func() interface{} { return new(Operation) },
}

var fragPool = sync.Pool{
	New: func() interface{} { return new(Fragment) },
}

var lexPool = sync.Pool{
	New: func() interface{} { return new(lexer) },
}

func Parse(gql []byte) (*Operation, error) {
	var err error

	if len(gql) == 0 {
		return nil, errors.New("blank query")
	}

	l := lexPool.Get().(*lexer)
	l.Reset()
	defer lexPool.Put(l)

	if err = lex(l, gql); err != nil {
		return nil, err
	}

	p := &Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}

	op := opPool.Get().(*Operation)
	op.Reset()
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
			if err = p.parseFragment(op); err != nil {
				return nil, err
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
	if err := p.parseOp(op); err != nil {
		return nil, err
	}

	return op, nil
}

func (p *Parser) parseFragment(op *Operation) error {
	frag := fragPool.Get().(*Fragment)
	frag.Reset()

	frag.Fields = frag.SelectionSet.fieldsA[:0]
	frag.Args = frag.SelectionSet.argsA[:0]

	if p.peek(itemName) {
		frag.Name = p.val(p.next())
	}

	if p.peek(itemOn) {
		p.ignore()
	} else {
		return errors.New("fragment: missing 'on' keyword")
	}

	if p.peek(itemName) {
		frag.On = p.vall(p.next())
	} else {
		return errors.New("fragment: missing table name after 'on' keyword")
	}

	if p.peek(itemObjOpen) {
		p.ignore()
	} else {
		return fmt.Errorf("fragment: expecting a '{', got: %s", p.next())
	}

	if err := p.parseSelectionSet(&frag.SelectionSet); err != nil {
		return fmt.Errorf("fragment: %v", err)
	}

	if p.frags == nil {
		p.frags = make(map[uint64]*Fragment)
	}

	_, _ = p.h.WriteString(frag.Name)
	k := p.h.Sum64()
	p.h.Reset()

	p.frags[k] = frag

	return nil
}

func (p *Parser) parseOp(op *Operation) error {
	var err error
	var typeSet bool

	if p.peek(itemQuery, itemMutation, itemSub) {
		err = p.parseOpTypeAndArgs(op)

		if err != nil {
			return fmt.Errorf("%s: %v", op.Type, err)
		}
		typeSet = true
	}

	if p.peek(itemObjOpen) {
		p.ignore()
		if !typeSet {
			op.Type = opQuery
		}

		for {
			if p.peek(itemEOF, itemFragment) {
				p.ignore()
				break
			}

			err = p.parseSelectionSet(&op.SelectionSet)
			if err != nil {
				return fmt.Errorf("%s: %v", op.Type, err)
			}
		}
	} else {
		return fmt.Errorf("expecting a query, mutation or subscription, got: %s", p.next())
	}

	return nil
}

func (p *Parser) parseOpTypeAndArgs(op *Operation) error {
	item := p.next()

	switch item._type {
	case itemQuery:
		op.Type = opQuery
	case itemMutation:
		op.Type = opMutate
	case itemSub:
		op.Type = opSub
	}

	op.Args = op.SelectionSet.argsA[:0]

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

func (p *Parser) parseSelectionSet(selset *SelectionSet) error {
	var err error

	selset.Fields, err = p.parseFields(selset.Fields)
	if err != nil {
		return err
	}

	return nil
}

func ParseArgValue(argVal string) (*Node, error) {
	l := lexPool.Get().(*lexer)
	l.Reset()

	if err := lex(l, []byte(argVal)); err != nil {
		return nil, err
	}

	p := &Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}
	op, err := p.parseValue()
	lexPool.Put(l)

	return op, err
}

func (p *Parser) parseFields(fields []Field) ([]Field, error) {
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

		if !p.peek(itemName) {
			if isFrag {
				return nil, fmt.Errorf("expecting a fragment name, got: %s", p.next())
			} else {
				return nil, fmt.Errorf("expecting an alias or field name, got: %s", p.next())
			}
		}

		var f *Field

		if isFrag {
			name := p.val(p.next())
			_, _ = p.h.WriteString(name)
			id := p.h.Sum64()
			p.h.Reset()

			fr, ok := p.frags[id]
			if !ok {
				return nil, fmt.Errorf("no fragment named '%s' defined", name)
			}

			n := int32(len(fields))
			fields = append(fields, fr.Fields...)

			for i := 0; i < len(fr.Fields); i++ {
				k := (n + int32(i))
				f := &fields[k]
				f.ID = int32(k)

				// If this is the top-level point the parent to the parent of the
				// previous field.
				if f.ParentID == -1 {
					pid := st.Peek()
					f.ParentID = pid
					if f.ParentID != -1 {
						fields[pid].Children = append(fields[f.ParentID].Children, f.ID)
					}
					// Update all the other parents id's by our new place in this new array
				} else {
					f.ParentID += n
				}

				f.Children = make([]int32, len(f.Children))
				copy(f.Children, fr.Fields[i].Children)

				f.Args = make([]Arg, len(f.Args))
				copy(f.Args, fr.Fields[i].Args)

				// Update all the children which is needed.
				for j := range f.Children {
					f.Children[j] += n
				}
			}

		} else {
			fields = append(fields, Field{ID: int32(len(fields))})

			f = &fields[(len(fields) - 1)]
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
		}

		// The first opening curley brackets after this
		// comes the columns or child fields
		if p.peek(itemObjOpen) {
			p.ignore()
			st.Push(f.ID)
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
			return nil, errors.New("expecting an argument name")
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

func (p *Parser) parseList() (*Node, error) {
	nodes := []*Node{}

	parent := nodePool.Get().(*Node)
	parent.Reset()

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
	case itemIntVal:
		node.Type = NodeInt
	case itemFloatVal:
		node.Type = NodeFloat
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
	return b2s(p.input[v.pos:v.end])
}

func (p *Parser) vall(v item) string {
	lowercase(p.input, v.pos, v.end)
	return b2s(p.input[v.pos:v.end])
}

func (p *Parser) peek(types ...itemType) bool {
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
	return b2s(p.input[item.pos:item.end])
}

func (p *Parser) reset(to int) {
	p.pos = to
}

func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

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
	case NodeStr:
		v = "node-string"
	case NodeInt:
		v = "node-int"
	case NodeFloat:
		v = "node-float"
	case NodeBool:
		v = "node-bool"
	case NodeVar:
		v = "node-var"
	case NodeObj:
		v = "node-obj"
	case NodeList:
		v = "node-list"
	}
	return v
}

func FreeNode(n *Node) {
	nodePool.Put(n)
}
