package core

import (
	"encoding/json"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// RootLimitInfo describes the compiled paging posture of one rendered select
// in a query result. It lets callers (the server agent, MCP tools) detect
// that a returned list is a limit-clamped page rather than the full
// population — the difference between "these are the rows" and "these are
// some rows".
type RootLimitInfo struct {
	// FieldName is the JSON key under Data (alias-aware).
	FieldName string
	Table     string
	Database  string
	// Path is the JSON key path from the Data root, e.g. {"accounts","invoices"}.
	Path []string
	// Limit is the compiled row limit; 0 when NoLimit.
	Limit int32
	// NoLimit is true for analytics mode, pure aggregate selects, or an
	// explicit no-limit request.
	NoLimit bool
	// Aggregate is true when the database computed aggregates for this
	// select (global aggregates or grouped columns).
	Aggregate bool
	Singular  bool
}

// TruncatedRootInfo reports a list in Data whose row count reached its
// compiled limit; more rows may exist.
type TruncatedRootInfo struct {
	Path  string `json:"path"` // dotted JSON path, e.g. "accounts" or "accounts.invoices"
	Rows  int    `json:"rows"`
	Limit int32  `json:"limit"`
}

// RootLimits returns the compiled paging posture of each rendered select in
// this result. It is empty for mutations, subscriptions, and multi-database
// parallel-root queries (those compile per database and no combined query
// plan is retained).
func (r *Result) RootLimits() []RootLimitInfo {
	return r.rootLimits
}

// TruncatedRoots compares each limited list in Data against its compiled
// limit and reports the lists that reached it. A list with exactly limit
// rows is reported even when the table holds exactly that many rows — the
// signal is "reached its limit; more rows may exist", never a certainty.
func (r *Result) TruncatedRoots() []TruncatedRootInfo {
	if len(r.rootLimits) == 0 || len(r.Data) == 0 {
		return nil
	}
	var data any
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil
	}
	var out []TruncatedRootInfo
	for _, info := range r.rootLimits {
		if info.NoLimit || info.Limit <= 0 || info.Singular || len(info.Path) == 0 {
			continue
		}
		if rows, found := maxListLen(data, info.Path, 0); found && rows >= int(info.Limit) {
			out = append(out, TruncatedRootInfo{
				Path:  joinPath(info.Path),
				Rows:  rows,
				Limit: info.Limit,
			})
		}
	}
	return out
}

// maxListLen walks path through maps and arrays and returns the largest list
// length found at the leaf. Intermediate arrays (parent rows) are fanned
// through with a bounded node budget.
func maxListLen(data any, path []string, depth int) (int, bool) {
	const nodeBudget = 1000
	current := []any{data}
	for _, segment := range path {
		next := make([]any, 0, len(current))
		for _, node := range current {
			switch typed := node.(type) {
			case map[string]any:
				if child, ok := typed[segment]; ok {
					next = append(next, child)
				}
			case []any:
				for _, item := range typed {
					if m, ok := item.(map[string]any); ok {
						if child, ok := m[segment]; ok {
							next = append(next, child)
						}
					}
					if len(next) > nodeBudget {
						break
					}
				}
			}
			if len(next) > nodeBudget {
				next = next[:nodeBudget]
				break
			}
		}
		if len(next) == 0 {
			return 0, false
		}
		current = next
	}
	maxLen, found := 0, false
	for _, node := range current {
		if list, ok := node.([]any); ok {
			found = true
			if len(list) > maxLen {
				maxLen = len(list)
			}
		}
	}
	return maxLen, found
}

func joinPath(path []string) string {
	out := ""
	for i, segment := range path {
		if i > 0 {
			out += "."
		}
		out += segment
	}
	return out
}

// rootLimitInfoFromQCode extracts per-select paging info from a compiled
// query. Only rendered query selects are included; children recurse to a
// small depth since deeper nesting rarely carries aggregate risk worth the
// walk cost.
func rootLimitInfoFromQCode(qc *qcode.QCode) []RootLimitInfo {
	if qc == nil || qc.Type != qcode.QTQuery {
		return nil
	}
	var out []RootLimitInfo
	var walk func(id int32, prefix []string, depth int)
	walk = func(id int32, prefix []string, depth int) {
		if depth > 3 || int(id) >= len(qc.Selects) {
			return
		}
		sel := &qc.Selects[id]
		if sel.SkipRender != qcode.SkipTypeNone {
			return
		}
		path := make([]string, len(prefix)+1)
		copy(path, prefix)
		path[len(prefix)] = sel.FieldName
		// A limit resolved from a request variable is unknown at compile
		// time; skip rather than guess.
		if sel.Paging.LimitVar == "" {
			out = append(out, RootLimitInfo{
				FieldName: sel.FieldName,
				Table:     sel.Table,
				Database:  sel.Database,
				Path:      path,
				Limit:     sel.Paging.Limit,
				NoLimit:   sel.Paging.NoLimit,
				Aggregate: sel.GlobalAgg || sel.GroupCols,
				Singular:  sel.Singular,
			})
		}
		for _, child := range sel.Children {
			walk(child, path, depth+1)
		}
	}
	for _, root := range qc.Roots {
		walk(root, nil, 0)
	}
	return out
}
