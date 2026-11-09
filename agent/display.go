package agent

import (
	"regexp"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

// Display hints. A reply carries markdown and raw rows, and a GUI had to
// guess which numbers were money, which column named the rows and whether a
// chart fits. The engine knows: the compiled query and the schema say which
// field is a key, a reference, a measure or a time. A hint is built from
// those facts and the rows of the query the answer rests on, never from the
// model's prose, and the model never sees it.

const maxDisplayRows = 50

// DisplayHint lays out one result root.
type DisplayHint struct {
	// Question is the delegated question the data answers, on a lead reply.
	Question string `json:"question,omitempty"`
	Root     string `json:"root"`
	// Kind suggests a form: value (one number), bar (one label against one
	// measure), line (measures over time) or table.
	Kind    string          `json:"kind"`
	Columns []DisplayColumn `json:"columns"`
	// Rows are the root's rows, at most maxDisplayRows; RowCount is how many
	// the query returned.
	Rows      []any `json:"rows"`
	RowCount  int   `json:"row_count"`
	Truncated bool  `json:"truncated,omitempty"`
}

// DisplayColumn is a field of the rows, named by its path inside a row.
type DisplayColumn struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Role      string `json:"role"`
	Aggregate string `json:"aggregate,omitempty"`
}

// executionColumns reads the columns a runtime attached to a result.
func executionColumns(out any) []core.ResultColumn {
	switch v := out.(type) {
	case executeResult:
		return v.Columns
	case *executeResult:
		if v != nil {
			return v.Columns
		}
	}
	return nil
}

// displayHints describes the data of the run's last successful query.
func (s *discoveryState) displayHints() []DisplayHint {
	if s == nil || len(s.lastColumns) == 0 {
		return nil
	}
	data, ok := s.lastExecutionData()
	if !ok {
		return nil
	}
	return buildDisplayHints(s.lastColumns, mapValue(data))
}

func buildDisplayHints(columns []core.ResultColumn, data map[string]any) []DisplayHint {
	byRoot := map[string][]DisplayColumn{}
	var roots []string
	for _, c := range columns {
		if _, seen := byRoot[c.Root]; !seen {
			roots = append(roots, c.Root)
		}
		byRoot[c.Root] = append(byRoot[c.Root], DisplayColumn{
			Name: strings.TrimPrefix(c.Path, c.Root+"."), Type: c.Type, Role: c.Role, Aggregate: c.Aggregate,
		})
	}
	var out []DisplayHint
	for _, root := range roots {
		rows := displayRows(data[root])
		if rows == nil {
			continue
		}
		hint := DisplayHint{Root: root, Columns: byRoot[root], RowCount: len(rows)}
		if len(rows) > maxDisplayRows {
			rows, hint.Truncated = rows[:maxDisplayRows], true
		}
		hint.Rows = rows
		markTextTimes(hint.Columns, rows)
		hint.Kind = displayKind(hint.Columns, hint.RowCount)
		out = append(out, hint)
	}
	return out
}

// isoTime matches an ISO 8601 date or timestamp.
var isoTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}(:?\d{2})?)?)?$`)

// markTextTimes marks a text column as a time when every value it holds is an
// ISO date. Databases like SQLite keep dates in TEXT columns, so the declared
// type alone would call them labels.
func markTextTimes(columns []DisplayColumn, rows []any) {
	for i, c := range columns {
		if c.Role != "text" {
			continue
		}
		seen := false
		dates := true
		for _, row := range rows {
			v := rowValue(row, c.Name)
			if v == nil {
				continue
			}
			s, ok := v.(string)
			if !ok || !isoTime.MatchString(s) {
				dates = false
				break
			}
			seen = true
		}
		if seen && dates {
			columns[i].Role = "time"
		}
	}
}

// rowValue reads a column by its path inside a row ("plan.name").
func rowValue(row any, path string) any {
	v := row
	for _, key := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return v
}

func displayRows(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case map[string]any:
		return []any{v}
	}
	return nil
}

// displayKind picks a form from the columns' roles alone. Keys never decide
// it: an id is not a label or a measure.
func displayKind(columns []DisplayColumn, rows int) string {
	var measures, times, labels int
	for _, c := range columns {
		switch c.Role {
		case "measure":
			measures++
		case "time":
			times++
		case "text", "reference":
			labels++
		}
	}
	switch {
	case rows == 1 && measures >= 1 && labels == 0 && times == 0:
		return "value"
	case rows > 1 && times == 1 && measures >= 1 && labels == 0:
		return "line"
	case rows >= 2 && rows <= 30 && labels == 1 && measures == 1 && times == 0:
		return "bar"
	}
	return "table"
}
