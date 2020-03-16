package qcode

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/dosco/super-graph/util"
)

var (
	errEOT = errors.New("end of tokens")
)

type parserType int32

const (
	maxFields = 100
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

type Operation struct {
	Type    parserType
	Name    string
	Args    []Arg
	argsA   [10]Arg
	Fields  []Field
	fieldsA [10]Field
}

var zeroOperation = Operation{}

func (o *Operation) Reset() {
	*o = zeroOperation
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

var lexPool = sync.Pool{
	New: func() interface{} { return new(lexer) },
}

func Parse(gql []byte) (*Operation, error) {
	return parseSelectionSet(gql)
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

func parseSelectionSet(gql []byte) (*Operation, error) {
	var err error

	if len(gql) == 0 {
		return nil, errors.New("blank query")
	}

	l := lexPool.Get().(*lexer)
	l.Reset()

	if err = lex(l, gql); err != nil {
		return nil, err
	}

	p := &Parser{
		input: l.input,
		pos:   -1,
		items: l.items,
	}

	var op *Operation

	if p.peek(itemObjOpen) {
		p.ignore()
		op, err = p.parseQueryOp()
	} else {
		op, err = p.parseOp()
	}

	if err != nil {
		return nil, err
	}

	if p.peek(itemObjClose) {
		p.ignore()
	} else {
		return nil, fmt.Errorf("operation missing closing '}'")
	}

	if !p.peek(itemEOF) {
		p.ignore()
		return nil, fmt.Errorf("invalid '%s' found after closing '}'", p.current())
	}

	lexPool.Put(l)

	return op, err
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

func (p *Parser) current() string {
	item := p.items[p.pos]
	return b2s(p.input[item.pos:item.end])
}

func (p *Parser) peek(types ...itemType) bool {
	n := p.pos + 1
	// if p.items[n]._type == itemEOF {
	// 	return false
	// }
	if n >= len(p.items) {
		return false
	}
	for i := 0; i < len(types); i++ {
		if p.items[n]._type == types[i] {
			return true
		}
	}
	return false
}

func (p *Parser) parseOp() (*Operation, error) {
	if !p.peek(itemQuery, itemMutation, itemSub) {
		err := errors.New("expecting a query, mutation or subscription")
		return nil, err
	}
	item := p.next()

	op := opPool.Get().(*Operation)
	op.Reset()

	switch item._type {
	case itemQuery:
		op.Type = opQuery
	case itemMutation:
		op.Type = opMutate
	case itemSub:
		op.Type = opSub
	}

	op.Fields = op.fieldsA[:0]
	op.Args = op.argsA[:0]

	var err error

	if p.peek(itemName) {
		op.Name = p.val(p.next())
	}

	if p.peek(itemArgsOpen) {
		p.ignore()

		op.Args, err = p.parseOpParams(op.Args)
		if err != nil {
			return nil, err
		}
	}

	if p.peek(itemObjOpen) {
		p.ignore()

		for n := 0; n < 10; n++ {
			if !p.peek(itemName) {
				break
			}

			op.Fields, err = p.parseFields(op.Fields)
			if err != nil {
				return nil, err
			}
		}
	}

	return op, nil
}

func (p *Parser) parseQueryOp() (*Operation, error) {
	op := opPool.Get().(*Operation)
	op.Reset()

	op.Type = opQuery
	op.Fields = op.fieldsA[:0]
	op.Args = op.argsA[:0]

	var err error

	for n := 0; n < 10; n++ {
		if !p.peek(itemName) {
			break
		}

		op.Fields, err = p.parseFields(op.Fields)
		if err != nil {
			return nil, err
		}
	}

	return op, nil
}

func (p *Parser) parseFields(fields []Field) ([]Field, error) {
	st := util.NewStack()

	for {
		if len(fields) >= maxFields {
			return nil, fmt.Errorf("too many fields (max %d)", maxFields)
		}

		if p.peek(itemObjClose) {
			p.ignore()
			st.Pop()

			if st.Len() == 0 {
				break
			} else {
				continue
			}
		}

		if !p.peek(itemName) {
			return nil, errors.New("expecting an alias or field name")
		}

		fields = append(fields, Field{ID: int32(len(fields))})

		f := &fields[(len(fields) - 1)]
		f.Args = f.argsA[:0]
		f.Children = f.childrenA[:0]

		// Parse the inside of the the fields () parentheses
		// in short parse the args like id, where, etc
		if err := p.parseField(f); err != nil {
			return nil, err
		}

		intf := st.Peek()
		if pid, ok := intf.(int32); ok {
			f.ParentID = pid
			fields[pid].Children = append(fields[pid].Children, f.ID)
		} else {
			f.ParentID = -1
		}

		// The first opening curley brackets after this
		// comes the columns or child fields
		if p.peek(itemObjOpen) {
			p.ignore()
			st.Push(f.ID)

		} else if p.peek(itemObjClose) {
			if st.Len() == 0 {
				break
			} else {
				continue
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

	return nil
}

func (p *Parser) parseOpParams(args []Arg) ([]Arg, error) {
	for {
		if len(args) >= maxArgs {
			return nil, fmt.Errorf("too many args (max %d)", maxArgs)
		}

		if p.peek(itemArgsClose) {
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

		if p.peek(itemArgsClose) {
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
		if p.peek(itemObjClose) {
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
	return fmt.Sprintf("<%s>", v)
}

// type Frees struct {
// 	n   *Node
// 	loc int
// }

// var freeList []Frees

// func FreeNode(n *Node, loc int) {
// 	j := -1

// 	for i := range freeList {
// 		if n == freeList[i].n {
// 			j = i
// 			break
// 		}
// 	}

// 	if j == -1 {
// 		nodePool.Put(n)
// 		freeList = append(freeList, Frees{n, loc})
// 	} else {
// 		fmt.Printf(">>>>(%d) RE_FREE %d %p %s %s\n", loc, freeList[j].loc, freeList[j].n, n.Name, n.Type)
// 	}
// }

func FreeNode(n *Node, loc int) {
	nodePool.Put(n)
}
