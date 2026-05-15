package qcode

import (
	"strings"
	"testing"
)

// TestParseFrameClause exercises the backend frame grammar used by the internal
// analytics IR. Public GraphQL exposes concept directives instead of raw frame
// text, but keeping this parser covered protects the renderer contract.
func TestParseFrameClause(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Single-bound ROWS/RANGE.
		{"rows unbounded preceding", "ROWS UNBOUNDED PRECEDING"},
		{"rows current row", "ROWS CURRENT ROW"},
		{"rows 5 preceding", "ROWS 5 PRECEDING"},
		{"rows 12 following", "ROWS 12 FOLLOWING"},
		{"range unbounded preceding", "RANGE UNBOUNDED PRECEDING"},
		{"range current row", "RANGE CURRENT ROW"},
		{"range 0 preceding", "RANGE 0 PRECEDING"},

		// BETWEEN both ends explicit.
		{
			"rows between unbounded preceding and current row",
			"ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
		},
		{
			"rows between unbounded preceding and unbounded following",
			"ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING",
		},
		{
			"rows between current row and unbounded following",
			"ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING",
		},
		{
			"rows between 5 preceding and current row",
			"ROWS BETWEEN 5 PRECEDING AND CURRENT ROW",
		},
		{
			"rows between 5 preceding and 5 following",
			"ROWS BETWEEN 5 PRECEDING AND 5 FOLLOWING",
		},
		{
			"rows between unbounded preceding and 1 following",
			"ROWS BETWEEN UNBOUNDED PRECEDING AND 1 FOLLOWING",
		},
		{
			"rows between 3 preceding and unbounded following",
			"ROWS BETWEEN 3 PRECEDING AND UNBOUNDED FOLLOWING",
		},
		{
			"rows between current row and 7 following",
			"ROWS BETWEEN CURRENT ROW AND 7 FOLLOWING",
		},

		// RANGE counterparts (Snowflake-supported subset).
		{
			"range between unbounded preceding and current row",
			"RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
		},
		{
			"range between 1 preceding and 1 following",
			"RANGE BETWEEN 1 PRECEDING AND 1 FOLLOWING",
		},

		// Mixed whitespace and case still canonicalise.
		{"  ROWS    Between  5   PRECEDING  AND  Current   Row  ",
			"ROWS BETWEEN 5 PRECEDING AND CURRENT ROW"},
	}
	for _, c := range cases {
		got, err := parseFrameClause(c.in)
		if err != nil {
			t.Errorf("parseFrameClause(%q) errored: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseFrameClause(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseFrameClause_Errors(t *testing.T) {
	cases := []struct {
		in, sub string
	}{
		{"", "empty"},
		{"foo unbounded preceding", "ROWS or RANGE"},
		{"rows", "missing bound"},
		{"rows between", "BETWEEN"},
		{"rows between unbounded preceding and", "BETWEEN"},
		{"rows between and current row", "BETWEEN"},
		{"rows -1 preceding", "non-negative integer"},
		{"rows abc preceding", "non-negative integer"},
		{"rows unbounded somewhere", "unrecognised bound"},
	}
	for _, c := range cases {
		_, err := parseFrameClause(c.in)
		if err == nil || !strings.Contains(err.Error(), c.sub) {
			t.Errorf("parseFrameClause(%q): want error containing %q, got: %v", c.in, c.sub, err)
		}
	}
}
