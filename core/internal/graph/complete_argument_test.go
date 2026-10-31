package graph

import "testing"

func TestParseArgValueRequiresOneCompleteValue(t *testing.T) {
	for _, value := range []string{`{id: {eq: 1}`, `[1, 2`, `{id: 1} {id: 2}`, `1 2`, `"first" "second"`} {
		if _, err := ParseArgValue(value, false); err == nil {
			t.Errorf("accepted incomplete or trailing input %q", value)
		}
	}
}

func TestParseArgValueConsumesTerminalNames(t *testing.T) {
	for _, tc := range []struct {
		input string
		kind  ParserType
		value string
	}{
		{"true", NodeBool, "true"}, {"false", NodeBool, "false"},
		{"null", NodeLabel, "null"}, {"ACTIVE", NodeLabel, "ACTIVE"},
	} {
		node, err := ParseArgValue(tc.input, false)
		if err != nil {
			t.Errorf("%s: %v", tc.input, err)
			continue
		}
		if node.Type != tc.kind || node.Val != tc.value {
			t.Errorf("%s decoded as %#v", tc.input, node)
		}
	}
	for _, value := range []string{`{id: {eq: 1}}`, `[1, 2]`, `[]`, `{}`, `true # comment`, `"value"`} {
		if _, err := ParseArgValue(value, false); err != nil {
			t.Errorf("valid %q: %v", value, err)
		}
	}
}
