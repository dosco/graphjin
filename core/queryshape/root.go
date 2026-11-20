package queryshape

import (
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

// Shape is what a GraphQL operation asks for, read by the engine's own parser.
type Shape struct {
	// Operation is query, mutation or subscription.
	Operation string
	// Variables is true when the operation declares or references a variable:
	// its text alone does not say what it reads.
	Variables bool
	// Literals are the string values written into its arguments.
	Literals []string
	Roots    []Root
}

// Root is one top-level selection.
type Root struct {
	// Name is the table or root field; Key is where its result lands (the
	// alias when there is one).
	Name string
	Key  string
	// Limit is the literal limit, or -1 when the root sets none.
	Limit int
	// OrderBy lists the ordered fields in the order written.
	OrderBy []string
	// Fields are the scalar fields it selects directly.
	Fields []Field
	// Nested is true when it also selects a relationship or other child
	// selection.
	Nested bool
}

// Field is a selected scalar: the column it reads and the response key its
// value lands under.
type Field struct {
	Name string
	Key  string
}

// Inspect parses an operation and describes its top-level selections.
func Inspect(query string) (Shape, error) {
	op, err := graph.Parse([]byte(query))
	if err != nil {
		return Shape{}, err
	}
	shape := Shape{Variables: len(op.VarDef) != 0}
	switch op.Type {
	case graph.OpMutate:
		shape.Operation = "mutation"
	case graph.OpSub:
		shape.Operation = "subscription"
	default:
		shape.Operation = "query"
	}
	var visitNode func(*graph.Node)
	visitNode = func(n *graph.Node) {
		if n == nil {
			return
		}
		switch n.Type {
		case graph.NodeVar:
			shape.Variables = true
		case graph.NodeStr:
			shape.Literals = append(shape.Literals, n.Val)
		}
		for _, child := range n.Children {
			visitNode(child)
		}
	}
	for _, f := range op.Fields {
		for _, arg := range f.Args {
			visitNode(arg.Val)
		}
	}
	for _, f := range op.Fields {
		if f.ParentID != -1 {
			continue
		}
		root := Root{Name: f.Name, Key: f.Name, Limit: -1}
		if f.Alias != "" {
			root.Key = f.Alias
		}
		for _, arg := range f.Args {
			switch arg.Name {
			case "limit":
				if arg.Val != nil && arg.Val.Type == graph.NodeNum {
					if n, err := strconv.Atoi(arg.Val.Val); err == nil {
						root.Limit = n
					}
				}
			case "order_by":
				root.OrderBy = orderByFields(arg.Val)
			}
		}
		for _, id := range f.Children {
			child := op.Fields[id]
			if len(child.Children) != 0 {
				root.Nested = true
				continue
			}
			field := Field{Name: child.Name, Key: child.Name}
			if child.Alias != "" {
				field.Key = child.Alias
			}
			root.Fields = append(root.Fields, field)
		}
		shape.Roots = append(shape.Roots, root)
	}
	return shape, nil
}

// orderByFields reads `order_by: { a: desc, b: asc }` and its list form
// `order_by: [{ a: desc }, { b: asc }]`.
func orderByFields(n *graph.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	switch n.Type {
	case graph.NodeObj:
		for _, child := range n.Children {
			if child.Name != "" {
				out = append(out, child.Name)
			}
		}
	case graph.NodeList:
		for _, child := range n.Children {
			out = append(out, orderByFields(child)...)
		}
	}
	return out
}
