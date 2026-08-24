package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// reshapeHasuraMutationData restores the response shape the Hasura mutation
// dialect asked for after GraphJin has executed the lowered native write.
// GraphJin returns the written rows directly under the root; Hasura nests them
// under `returning` and reports `affected_rows` beside them. Raw JSON leaves
// are preserved so numbers never pass through a float64 conversion.
func reshapeHasuraMutationData(data []byte, plans []qcode.HasuraMutationRoot) ([]byte, error) {
	if len(data) == 0 || len(plans) == 0 {
		return data, nil
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if !plan.Returning && !plan.AffectedRows && !plan.Single {
			// The caller selected columns directly, exactly as GraphJin
			// returns them; there is nothing to restore.
			continue
		}
		raw, ok := document[plan.ResponseKey]
		if !ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}

		rows, err := hasuraMutationRows(raw)
		if err != nil {
			return nil, fmt.Errorf("root %q: %w", plan.ResponseKey, err)
		}

		// A _by_pk root addresses one row and Hasura returns one object, not
		// a list — unless the caller wrapped it in returning, which is a list
		// in every Hasura response.
		if plan.Single && !plan.Returning && !plan.AffectedRows {
			var single json.RawMessage = json.RawMessage("null")
			if len(rows) != 0 {
				single = rows[0]
			}
			document[plan.ResponseKey] = single
			continue
		}

		root := map[string]json.RawMessage{}
		if plan.Returning {
			returning, err := json.Marshal(rows)
			if err != nil {
				return nil, err
			}
			root["returning"] = returning
		}
		if plan.AffectedRows {
			root["affected_rows"] = json.RawMessage(strconv.Itoa(len(rows)))
		}
		reshaped, err := json.Marshal(root)
		if err != nil {
			return nil, err
		}
		document[plan.ResponseKey] = reshaped
	}
	return json.Marshal(document)
}

// hasuraMutationRows reads the written rows from a native mutation response,
// which GraphJin returns as a list, or as a single object for a write that
// touched one row.
func hasuraMutationRows(raw []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) != 0 && trimmed[0] == '[' {
		var rows []json.RawMessage
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, fmt.Errorf("expected written rows: %w", err)
		}
		return rows, nil
	}
	var row json.RawMessage
	if err := json.Unmarshal(trimmed, &row); err != nil {
		return nil, fmt.Errorf("expected a written row: %w", err)
	}
	return []json.RawMessage{row}, nil
}
