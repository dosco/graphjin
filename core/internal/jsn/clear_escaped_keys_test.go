package jsn_test

import (
	"bytes"
	"encoding/json"
	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"testing"
)

func TestClearPreservesEscapedKeys(t *testing.T) {
	for _, input := range []string{`{"a\"b":1}`, `{"caf\u00e9":"value"}`, `{"path\\key":{"tab\tkey":true}}`} {
		var buf bytes.Buffer
		if err := jsn.Clear(&buf, []byte(input)); err != nil {
			t.Fatal(err)
		}
		var want, got map[string]any
		if err := json.Unmarshal([]byte(input), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON %s: %v", buf.String(), err)
		}
		for key := range want {
			if _, ok := got[key]; !ok {
				t.Fatalf("lost key %q: %s", key, buf.String())
			}
		}
	}
}
