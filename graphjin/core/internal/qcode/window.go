package qcode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
)

// WindowSpec captures the backend window definition produced by public
// analytics directives such as @running, @previous, and @rank. When a Field has
// a non-nil Window, the SQL emitter wraps the function as
//
//	<func>(args) OVER (PARTITION BY <p1>, ... ORDER BY <o1>, ... <frame>)
//
// and the field does NOT trigger a GROUP BY on the enclosing select: analytics
// directives return one row per input row, not one per group.
//
// All three sections are optional individually. An empty WindowSpec
// emits a bare `OVER ()`, which is valid SQL for internal window functions that
// support the implicit window frame.
type WindowSpec struct {
	Partition []string
	OrderBy   []WindowOrder
	Frame     string // canonical, uppercase frame clause; "" = engine default
}

// NullsHandling controls placement of NULL values in the backend OVER ORDER BY.
// The public analytics directives do not expose null placement yet.
type NullsHandling int8

const (
	NullsDefault NullsHandling = iota
	NullsFirst
	NullsLast
)

// WindowOrder is one column entry in the OVER (... ORDER BY ...) list.
type WindowOrder struct {
	Col   string
	Desc  bool
	Nulls NullsHandling
}

// WindowFunc is an internal SQL analytic/window function selected by public
// GraphJin analytics directives. These are rendered by the SQL compiler rather
// than treated as user-defined database functions.
type WindowFunc int8

const (
	WindowFuncNone WindowFunc = iota
	WindowFuncRowNumber
	WindowFuncRank
	WindowFuncDenseRank
	WindowFuncLag
	WindowFuncLead
	WindowFuncFirstValue
	WindowFuncLastValue
)

func (wf WindowFunc) String() string {
	switch wf {
	case WindowFuncRowNumber:
		return "row_number"
	case WindowFuncRank:
		return "rank"
	case WindowFuncDenseRank:
		return "dense_rank"
	case WindowFuncLag:
		return "lag"
	case WindowFuncLead:
		return "lead"
	case WindowFuncFirstValue:
		return "first_value"
	case WindowFuncLastValue:
		return "last_value"
	default:
		return ""
	}
}

func (wf WindowFunc) IsValueFunc() bool {
	switch wf {
	case WindowFuncLag, WindowFuncLead, WindowFuncFirstValue, WindowFuncLastValue:
		return true
	default:
		return false
	}
}

func WindowSpecHasNulls(w *WindowSpec) bool {
	if w == nil {
		return false
	}
	for _, ord := range w.OrderBy {
		if ord.Nulls != NullsDefault {
			return true
		}
	}
	return false
}

func (co *Compiler) compileDirectiveWindow(sel *Select, f *Field, d graph.Directive) error {
	return fmt.Errorf("@window has been replaced by GraphJin analytics directives; use @running, @moving, @previous, @next, @first, @last, @rank, @denseRank, or @rowNumber")
}

// parseFrameClause normalises a user-supplied frame string into the
// canonical, uppercased SQL form. Token-based parsing keeps the
// allowed shapes explicit while still accepting numeric offsets — every
// `<n>` is parsed as a non-negative integer rather than passed through
// as a string, so SQL injection via the frame argument is impossible.
//
// Supported (uppercase here for clarity, lower/mixed case input
// accepted):
//
//	ROWS UNBOUNDED PRECEDING
//	ROWS CURRENT ROW
//	ROWS <n> PRECEDING
//	ROWS <n> FOLLOWING
//	ROWS BETWEEN <bound> AND <bound>
//	(same with RANGE)
//
// where `<bound>` is one of: UNBOUNDED PRECEDING / UNBOUNDED FOLLOWING
// / CURRENT ROW / `<n> PRECEDING` / `<n> FOLLOWING`.
//
// INTERVAL constants for timestamp-typed RANGE frames are not yet
// supported; they're a Snowflake/Postgres-specific extension that
// requires a separate type-aware code path.
func parseFrameClause(in string) (string, error) {
	toks := strings.Fields(strings.ToLower(in))
	if len(toks) == 0 {
		return "", fmt.Errorf("frame clause is empty")
	}
	mode := strings.ToUpper(toks[0])
	switch mode {
	case "ROWS", "RANGE":
	default:
		return "", fmt.Errorf("frame must start with ROWS or RANGE, got %q", toks[0])
	}
	rest := toks[1:]
	if len(rest) == 0 {
		return "", fmt.Errorf("frame %s missing bound", mode)
	}

	// Two shapes: BETWEEN <a> AND <b>, or single-bound `<a>`.
	if rest[0] == "between" {
		andIdx := -1
		for i, t := range rest {
			if t == "and" {
				andIdx = i
				break
			}
		}
		if andIdx == -1 || andIdx == 1 || andIdx == len(rest)-1 {
			return "", fmt.Errorf("frame BETWEEN requires <bound> AND <bound>: %q", in)
		}
		startStr, err := renderBound(rest[1:andIdx])
		if err != nil {
			return "", fmt.Errorf("frame %q: start bound: %w", in, err)
		}
		endStr, err := renderBound(rest[andIdx+1:])
		if err != nil {
			return "", fmt.Errorf("frame %q: end bound: %w", in, err)
		}
		return fmt.Sprintf("%s BETWEEN %s AND %s", mode, startStr, endStr), nil
	}

	bound, err := renderBound(rest)
	if err != nil {
		return "", fmt.Errorf("frame %q: %w", in, err)
	}
	return fmt.Sprintf("%s %s", mode, bound), nil
}

// renderBound takes the trailing tokens of a frame bound and returns
// the canonical SQL fragment, or an error if the tokens don't form a
// recognised bound. `toks` MUST be non-empty.
func renderBound(toks []string) (string, error) {
	switch len(toks) {
	case 2:
		switch {
		case toks[0] == "unbounded" && toks[1] == "preceding":
			return "UNBOUNDED PRECEDING", nil
		case toks[0] == "unbounded" && toks[1] == "following":
			return "UNBOUNDED FOLLOWING", nil
		case toks[0] == "current" && toks[1] == "row":
			return "CURRENT ROW", nil
		case toks[1] == "preceding" || toks[1] == "following":
			n, err := strconv.Atoi(toks[0])
			if err != nil || n < 0 {
				return "", fmt.Errorf("expected non-negative integer offset, got %q", toks[0])
			}
			return fmt.Sprintf("%d %s", n, strings.ToUpper(toks[1])), nil
		}
	}
	return "", fmt.Errorf("unrecognised bound %q", strings.Join(toks, " "))
}
