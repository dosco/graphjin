package graph

import (
	"strings"
	"testing"
)

func TestFragmentsCanReferenceLaterDefinitions(t *testing.T) {
	_, err := Parse([]byte(`fragment A on products { id ...B } fragment B on products { name } query { products { ...A } }`))
	if err != nil {
		t.Fatal(err)
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
