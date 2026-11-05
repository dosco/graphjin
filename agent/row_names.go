package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// Row names.
//
// Label widening puts the name a which-question needs into the rows, but the
// worker writes its answer in the same code block that runs the read, from
// the fields it chose itself. Measured on flash-lite, "which account renews
// soonest" came back "account ID 1" in two of three runs with "Meridian
// Robotics" sitting in the result, and in all three lead-eval runs.
//
// So a which-answer that cites a row by its id while the result held that
// row's name is restated once, tool-less, to name it — and if the restatement
// still leaves the name out, a plain note gives it. Nothing is refused: the
// answer was right, only less useful than the data allowed.

// rowName is one row the run's reads returned with both its id and its name.
type rowName struct {
	entity, id, name string
}

// maxRowNames bounds how many named rows one run keeps.
const maxRowNames = 20

// recordRowNames keeps, from a which-question's read, each row's id and the
// name it came back with: the root's own label column, or the label of the
// row a foreign key references.
func (s *discoveryState) recordRowNames(query string, out any, labels map[string]string) {
	if !asksWhich(s.instruction) || len(s.rowNames) >= maxRowNames {
		return
	}
	roots, ok := scanRootFields(query)
	if !ok {
		return
	}
	data := resultData(out)
	for _, root := range roots {
		rows, _ := data[root.key].([]any)
		table := strings.ToLower(root.name)
		for _, raw := range rows {
			row := mapValue(raw)
			if label := labels[table]; label != "" {
				s.addRowName(strings.TrimSuffix(table, "s"), scalarText(row["id"]), stringValue(row[label]))
			}
			for column, target := range s.relations[table] {
				relation := strings.TrimSuffix(column, "_id")
				if label := labels[target]; label != "" && relation != column {
					s.addRowName(relation, scalarText(row[column]), stringValue(mapValue(row[relation])[label]))
				}
			}
		}
	}
}

func (s *discoveryState) addRowName(entity, id, name string) {
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if id == "" || name == "" || len(s.rowNames) >= maxRowNames {
		return
	}
	for _, seen := range s.rowNames {
		if seen.entity == entity && seen.id == id {
			return
		}
	}
	s.rowNames = append(s.rowNames, rowName{entity: entity, id: id, name: name})
}

func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
	}
	return fmt.Sprint(v)
}

// unnamedRows returns the rows an answer cites by id — "account ID 1",
// "account #1", "ID 1" — without naming them. A bare number is never read as
// an id: "1 account" or a count of 1 says nothing about which row.
func unnamedRows(answer string, names []rowName) []rowName {
	lower := strings.ToLower(answer)
	var out []rowName
	for _, row := range names {
		if mentionsLabel(lower, strings.ToLower(row.name)) {
			continue
		}
		pattern := `(?:\bid\b|#|\b` + regexp.QuoteMeta(strings.ToLower(row.entity)) + `\b)[\s:#*]*(?:id\b)?[\s:#*]*` + regexp.QuoteMeta(strings.ToLower(row.id)) + `\b`
		if regexp.MustCompile(pattern).MatchString(lower) {
			out = append(out, row)
		}
	}
	return out
}

const rowNameRewriteSignature = `"The draft answer identifies rows by their id, but each row has a name in row_names. Restate the answer so it names each row, keeping its id in parentheses if the draft gave one. Keep everything else the draft said, and use only values from row_names and the draft."
instruction:string "The user's original request.",
draft_answer:string "The draft answer.",
row_names:string "Each row's id and name, as JSON."
-> answer:string "The restated answer."`

// rowNameNote names the rows in plain words when a restatement could not.
func rowNameNote(rows []rowName) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s %s is %s", row.entity, row.id, row.name))
	}
	return "\n\n(" + strings.ToUpper(parts[0][:1]) + strings.Join(parts, "; ")[1:] + ".)"
}

// nameCitedRows makes an answer that cites rows by id name them: one
// tool-less restatement, then a plain note if the names are still missing.
func (a *Agent) nameCitedRows(ctx context.Context, client ax.AIClient, protocol *protocolRuntime, cfg Config, req Request, resp Response, rateLimiter ax.AxRateLimiter) Response {
	state := protocol.state
	if resp.Status != StatusAnswered || state.rowNameRewriteUsed || len(state.rowNames) == 0 {
		return resp
	}
	rows := unnamedRows(resp.Answer, state.rowNames)
	if len(rows) == 0 {
		return resp
	}
	state.rowNameRewriteUsed = true
	if a != nil && a.newFinalizer != nil && client != nil {
		named := make([]any, 0, len(rows))
		for _, row := range rows {
			named = append(named, map[string]any{"entity": row.entity, "id": row.id, "name": row.name})
		}
		if finalizer := a.newFinalizer(rowNameRewriteSignature, map[string]ax.Value{}); finalizer != nil {
			out, err := finalizer.Forward(ctx, client, map[string]ax.Value{
				"instruction":  strings.TrimSpace(req.Instruction),
				"draft_answer": resp.Answer,
				"row_names":    stringify(named),
			}, modelForwardOptions(cfg, rateLimiter))
			resp.Usage = mergedUsage(resp.Usage, usageSummary(finalizer))
			if err == nil {
				answer := strings.TrimSpace(stringValue(mapValue(normalizeValue(out))["answer"]))
				if answer != "" && len(state.ungroundedAnswerTokens(answer)) == 0 && len(unnamedRows(answer, rows)) == 0 && namesAll(answer, rows) {
					resp.Answer = answer
					return resp
				}
			}
		}
	}
	resp.Answer += rowNameNote(rows)
	return resp
}

func namesAll(answer string, rows []rowName) bool {
	lower := strings.ToLower(answer)
	for _, row := range rows {
		if !mentionsLabel(lower, strings.ToLower(row.name)) {
			return false
		}
	}
	return true
}
