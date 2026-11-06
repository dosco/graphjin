package graph

import "testing"

// Issue #422: the saved operation text must start at the operation, not at the
// last character of whatever definition precedes it.
func TestOperationQueryText(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"first", "query getUser { users { id } }", "query getUser { users { id } }"},
		{"leading comment", "# saved\nquery q { users { id } }", "# saved\nquery q { users { id } }"},
		{"after fragment", "fragment F on users { id }\nquery q { users { ...F } }", "query q { users { ...F } }"},
		{"anonymous after fragment", "fragment F on users { id }\n{ users { ...F } }", "{ users { ...F } }"},
		{"before fragment", "query q { users { ...F } }\nfragment F on users { id }", "query q { users { ...F } }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(op.Query); got != tc.want {
				t.Fatalf("op.Query = %q, want %q", got, tc.want)
			}
		})
	}
}
