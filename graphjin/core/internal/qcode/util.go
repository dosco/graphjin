package qcode

import (
	"bytes"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (co *Compiler) ParseName(name string) string {
	if co.c.EnableCamelcase {
		return util.ToSnake(name)
	}
	return name
}

func GetQType(t graph.ParserType) QType {
	switch t {
	case graph.OpQuery:
		return QTQuery
	case graph.OpSub:
		return QTSubscription
	case graph.OpMutate:
		return QTMutation
	default:
		return QTUnknown
	}
}

func GetQTypeByName(t string) QType {
	switch t {
	case "query":
		return QTQuery
	case "subscription":
		return QTSubscription
	case "mutation":
		return QTMutation
	default:
		return QTUnknown
	}
}

func graphNodeToJSON(node *graph.Node, w *bytes.Buffer) {
	switch node.Type {
	case graph.NodeStr:
		w.WriteString(`"` + node.Val + `"`)

	case graph.NodeNum, graph.NodeBool:
		w.WriteString(node.Val)

	case graph.NodeObj:
		w.WriteString(`{`)
		for i, c := range node.Children {
			if i == 0 {
				w.WriteString(`"` + c.Name + `": `)
			} else {
				w.WriteString(`,"` + c.Name + `": `)
			}
			graphNodeToJSON(c, w)
		}
		w.WriteString(`}`)

	case graph.NodeList:
		w.WriteString(`[`)
		for i, c := range node.Children {
			if i != 0 {
				w.WriteString(`,`)
			}
			graphNodeToJSON(c, w)
		}
		w.WriteString(`]`)
	}
}
