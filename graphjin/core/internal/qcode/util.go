package qcode

import (
	"github.com/dosco/graphjin/core/internal/graph"
)

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
