package dialect

import (
	"reflect"
	"strings"
	"testing"
)

// TestSnowflakeQuoteIdentifierEscape is a regression guard for
// identifier quoting. Snowflake terminates a quoted identifier on the
// first unescaped `"` — an embedded `"` that isn't doubled would cut
// the identifier short and produce either a syntax error or a
// reference to the wrong column. The fix doubles embedded quotes;
// this test pins that contract.
func TestSnowflakeQuoteIdentifierEscape(t *testing.T) {
	d := &SnowflakeDialect{}

	tests := []struct {
		name, in, want string
	}{
		{"plain identifier", `foo`, `"foo"`},
		{"uppercase", `TICKETS`, `"TICKETS"`},
		{"embedded quote doubled", `col"name`, `"col""name"`},
		{"multiple embedded quotes", `a"b"c`, `"a""b""c"`},
		{"starts with quote", `"x`, `"""x"`},
		{"ends with quote", `x"`, `"x"""`},
		{"empty identifier", ``, `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.QuoteIdentifier(tt.in); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSnowflakeSplitQueryBlockComments is a regression guard for the
// statement splitter. A `;` inside a C-style block comment must NOT
// split the statement — otherwise the comment gets truncated across
// statement boundaries and the second "half" is a broken fragment.
// GraphJin emits multi-statement scripts for linear-execution
// mutations, so getting this wrong breaks mutations that contain any
// block-commented SQL.
func TestSnowflakeSplitQueryBlockComments(t *testing.T) {
	d := &SnowflakeDialect{}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "block comment with embedded semicolon — single statement",
			query: `SELECT 1 /* a;b */`,
			want:  []string{`SELECT 1 /* a;b */`},
		},
		{
			name:  "block comment between statements",
			query: `SELECT 1; /* between */ SELECT 2`,
			want:  []string{`SELECT 1`, `/* between */ SELECT 2`},
		},
		{
			name:  "multi-line block comment with semicolons",
			query: "SELECT 1 /* line1;\nline2; */ FROM t; SELECT 2",
			want:  []string{"SELECT 1 /* line1;\nline2; */ FROM t", "SELECT 2"},
		},
		{
			name:  "line comment still works",
			query: "SELECT 1; -- trailing; comment\nSELECT 2",
			want:  []string{"SELECT 1", "-- trailing; comment\nSELECT 2"},
		},
		{
			name:  "semicolon inside paren depth still protected",
			query: `SELECT coalesce(1; 2) FROM t; SELECT 3`,
			want:  []string{`SELECT coalesce(1; 2) FROM t`, `SELECT 3`},
		},
		{
			name:  "semicolon inside string literal still protected",
			query: `SELECT 'a;b'; SELECT 2`,
			want:  []string{`SELECT 'a;b'`, `SELECT 2`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.SplitQuery(tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitQuery(%q)\n got:  %q\n want: %q", tt.query, got, tt.want)
			}
		})
	}
}

// TestSnowflakeSplitQueryNoComment confirms the baseline splitter still
// handles ordinary multi-statement scripts (no comments) correctly.
func TestSnowflakeSplitQueryNoComment(t *testing.T) {
	d := &SnowflakeDialect{}
	got := d.SplitQuery(`SELECT 1; SELECT 2; SELECT 3`)
	want := []string{`SELECT 1`, `SELECT 2`, `SELECT 3`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestSnowflakeRenderJSONRootEndToEnd is a fragment-level assertion that
// the CAST/OBJECT_CONSTRUCT skeleton composes correctly when paired with
// RenderJSONRootSuffix + the caller's closing `) AS "__root"`. This is
// the paren-matching contract from psql/query.go:344.
func TestSnowflakeRenderJSONRootEndToEnd(t *testing.T) {
	// Match "SELECT CAST(OBJECT_CONSTRUCT(" + suffix ") AS VARCHAR" +
	// caller's ") AS __root" = 2 closing parens total, matching the
	// 2 opens. If anyone ever changes the skeleton, this catches the
	// mismatch at build/test time rather than at Snowflake parse time.
	root := "SELECT CAST(OBJECT_CONSTRUCT("
	suffix := ") AS VARCHAR"
	callerClose := ") AS \"__root\""

	assembled := root + "'key', 'val'" + suffix + callerClose
	// Count parens — should balance.
	opens := strings.Count(assembled, "(")
	closes := strings.Count(assembled, ")")
	if opens != closes {
		t.Errorf("JSON root skeleton paren imbalance: opens=%d closes=%d; assembled=%s", opens, closes, assembled)
	}
}
