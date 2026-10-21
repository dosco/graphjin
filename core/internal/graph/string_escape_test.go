package graph

import (
	"encoding/json"
	"testing"
)

func TestParseEscapedStringValues(t *testing.T) {
	for _, raw := range []string{`"C:\\"`, `"\\"`, `"\uD83D\uDE00"`, `"\uD834\uDD1E"`} {
		t.Run(raw, func(t *testing.T) {
			var want string
			if err := json.Unmarshal([]byte(raw), &want); err != nil {
				t.Fatal(err)
			}
			op, err := Parse([]byte(`query { users(where: {name: {eq: ` + raw + `}}) { id } }`))
			if err != nil {
				t.Fatal(err)
			}
			got := op.Fields[0].Args[0].Val.Children[0].Children[0].Val
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}
