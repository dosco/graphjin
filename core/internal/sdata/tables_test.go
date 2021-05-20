package sdata

import "testing"

func TestIsInList(t *testing.T) {
	list := []string{
		"foo",
		"bar_.*",
	}

	for value, isPresent := range map[string]bool{
		"foo":     true,
		"foo_bar": false,
		"baz":     false,
		"bar_foo": true,
	} {
		if isInList(value, list) != isPresent {
			expected := "not be"
			if isPresent {
				expected = "be"
			}
			t.Fatalf("expected %s to %s in %v", value, expected, list)
		}
	}
}
