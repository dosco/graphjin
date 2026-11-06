package qcode

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

func TestArgTypesText(t *testing.T) {
	for _, tc := range []struct {
		types []graph.ParserType
		want  string
	}{
		{[]graph.ParserType{graph.NodeObj}, "an object"},
		{[]graph.ParserType{graph.NodeVar}, "a variable"},
		{[]graph.ParserType{graph.NodeNum, graph.NodeVar}, "a number or a variable"},
		{[]graph.ParserType{graph.NodeStr, graph.NodeNum, graph.NodeObj}, "a string, a number or an object"},
		{[]graph.ParserType{graph.NodeList, graph.NodeStr}, "a list of strings"},
	} {
		if got := argTypes(tc.types); got != tc.want {
			t.Errorf("argTypes(%v) = %q, want %q", tc.types, got, tc.want)
		}
	}
}
