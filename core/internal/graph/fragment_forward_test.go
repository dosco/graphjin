package graph

import (
	"fmt"
	"strings"
	"testing"
)

func TestFragmentsCanReferenceLaterDefinitions(t *testing.T) {
	_, err := Parse([]byte(`fragment A on products { id ...B } fragment B on products { name } query { products { ...A } }`))
	if err != nil {
		t.Fatal(err)
	}
}

func fragmentChain(count int, cyclic, reverse bool) []byte {
	var query strings.Builder
	for j := 0; j < count; j++ {
		i := j
		if reverse {
			i = count - j - 1
		}
		fmt.Fprintf(&query, "fragment F%d on products { ", i)
		if i+1 < count {
			fmt.Fprintf(&query, "...F%d", i+1)
		} else if cyclic {
			query.WriteString("...F0")
		} else {
			query.WriteString("id")
		}
		query.WriteString(" } ")
	}
	query.WriteString("query { products { ...F0 } }")
	return []byte(query.String())
}

func TestFragmentDependencyDepthIsBounded(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		if op, err := Parse(fragmentChain(100, false, reverse)); err != nil || len(op.Fields) != 2 {
			t.Fatalf("valid chain: fields=%d err=%v", len(op.Fields), err)
		}
		for _, cyclic := range []bool{false, true} {
			for _, count := range []int{101, 3000} {
				_, err := Parse(fragmentChain(count, cyclic, reverse))
				if err == nil {
					t.Fatalf("accepted chain of %d fragments (cyclic=%v, reverse=%v)", count, cyclic, reverse)
				}
				if len(err.Error()) > 256 {
					t.Fatalf("fragment error grew with dependency chain: %d bytes", len(err.Error()))
				}
			}
		}
	}
}

func TestSharedFragmentDependenciesRemainReusable(t *testing.T) {
	op, err := Parse([]byte(`fragment A on products { ...C } fragment B on products { ...C } fragment C on products { id } query { products { ...A ...B } }`))
	if err != nil || len(op.Fields) != 3 {
		t.Fatalf("shared dependency: fields=%d err=%v", len(op.Fields), err)
	}
}

func TestFragmentReferenceCyclesAndMissingNamesFail(t *testing.T) {
	for _, query := range []string{
		`fragment A on products { ...A } query { products { ...A } }`,
		`fragment A on products { ...B } fragment B on products { ...A } query { products { ...A } }`,
		`fragment A on products { ...Missing } query { products { ...A } }`,
		`fragment Unused on products { ...Missing } query { products { id } }`,
		`fragment Broken on products { id query { products { id } }`,
	} {
		if _, err := Parse([]byte(query)); err == nil {
			t.Errorf("accepted invalid fragment document %s", query)
		}
	}
}

func TestFragmentNameIsContextual(t *testing.T) {
	for _, query := range []string{
		`{ products { fragment } }`,
		`query fragment { products { id } }`,
		`query fragment($unused = {fragment: 1}) { products { fragment } } fragment Unused on products { id }`,
	} {
		if _, err := Parse([]byte(query)); err != nil {
			t.Errorf("%s: %v", query, err)
		}
	}
}

func TestExpandedFragmentsRespectFieldLimit(t *testing.T) {
	query := `fragment A on products { ` + strings.Repeat("id ", 700) + ` } query { products { ...A ...A } }`
	if _, err := Parse([]byte(query)); err == nil {
		t.Fatal("accepted fragment expansion beyond field limit")
	}
}

func TestFragmentIndexAcceptsContextualBooleanNames(t *testing.T) {
	for _, name := range []string{"true", "false"} {
		l, err := lex([]byte(name + " on products { id }"))
		if err != nil {
			t.Fatal(err)
		}
		p := Parser{input: l.input, pos: -1, items: l.items}
		if err := p.indexFragment(); err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
		if _, ok := p.fragPos[name]; !ok {
			t.Fatalf("fragment %s not indexed", name)
		}
	}
}
