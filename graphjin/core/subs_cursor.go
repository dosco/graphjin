package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

const systemCursorVersion = "m1:"

type cursorCheckpoint struct {
	selectionID int32
	values      map[string]any
	ok          bool
}

func encodeSystemCursor(selectionID int32, values []any) string {
	data, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(int64(selectionID), 10) + "," + systemCursorVersion +
		base64.RawURLEncoding.EncodeToString(data)
}

func decodeSystemCursor(raw string) (int32, []any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil, false
	}
	if strings.HasPrefix(raw, "gj-") {
		if i := strings.IndexByte(raw, ':'); i != -1 {
			raw = raw[i+1:]
		}
	}
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return 0, nil, false
	}
	id, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return 0, nil, false
	}
	if strings.HasPrefix(parts[1], systemCursorVersion) {
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(parts[1], systemCursorVersion))
		if err != nil {
			return 0, nil, false
		}
		var values []any
		if err := json.Unmarshal(data, &values); err != nil {
			return 0, nil, false
		}
		return int32(id), values, true
	}

	legacy := strings.Split(parts[1], ",")
	values := make([]any, len(legacy))
	for i := range legacy {
		values[i] = legacy[i]
	}
	return int32(id), values, true
}

func checkpointForSelect(sel *qcode.Select, vars map[string]json.RawMessage) cursorCheckpoint {
	if sel == nil || !sel.Paging.Cursor || sel.Paging.CursorVar == "" {
		return cursorCheckpoint{}
	}
	var raw string
	if err := json.Unmarshal(vars[sel.Paging.CursorVar], &raw); err != nil || raw == "" {
		return cursorCheckpoint{}
	}
	selectionID, values, ok := decodeSystemCursor(raw)
	if !ok || selectionID != sel.ID || len(values) < len(sel.OrderBy) {
		return cursorCheckpoint{}
	}
	cp := cursorCheckpoint{
		selectionID: selectionID,
		values:      make(map[string]any, len(sel.OrderBy)),
		ok:          true,
	}
	for i, order := range sel.OrderBy {
		cp.values[systemCursorOrderKey(order)] = values[i]
	}
	return cp
}

func systemCursorOrderKey(order qcode.OrderBy) string {
	if order.Key != "" {
		return order.Col.Name + "_" + order.Key
	}
	return order.Col.Name
}

func systemCursorValues(sel *qcode.Select, row map[string]any) []any {
	if sel == nil || len(sel.OrderBy) == 0 || row == nil {
		return nil
	}
	values := make([]any, 0, len(sel.OrderBy))
	for _, order := range sel.OrderBy {
		values = append(values, systemOrderValue(row, order))
	}
	return values
}

func systemOrderValue(row map[string]any, order qcode.OrderBy) any {
	value := row[order.Col.Name]
	if order.Key == "" {
		return value
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed[order.Key]
	case json.RawMessage:
		var object map[string]any
		if json.Unmarshal(typed, &object) == nil {
			return object[order.Key]
		}
	case []byte:
		var object map[string]any
		if json.Unmarshal(typed, &object) == nil {
			return object[order.Key]
		}
	case string:
		var object map[string]any
		if json.Unmarshal([]byte(typed), &object) == nil {
			return object[order.Key]
		}
	}
	return nil
}

func findSeekExp(ex *qcode.Exp) *qcode.Exp {
	if ex == nil {
		return nil
	}
	if isSeekExp(ex) {
		return ex
	}
	for _, child := range ex.Children {
		if found := findSeekExp(child); found != nil {
			return found
		}
	}
	return nil
}

func isSeekExp(ex *qcode.Exp) bool {
	return ex != nil &&
		ex.Op == qcode.OpOr &&
		len(ex.Children) != 0 &&
		ex.Children[0] != nil &&
		ex.Children[0].Op == qcode.OpIsNull &&
		ex.Children[0].Left.Table == "__cur"
}

func systemRowLess(sel *qcode.Select, left, right map[string]any) bool {
	if sel == nil {
		return false
	}
	for _, order := range sel.OrderBy {
		cmp := compareValues(systemOrderValue(left, order), systemOrderValue(right, order))
		if cmp == 0 {
			continue
		}
		switch order.Order {
		case qcode.OrderDesc, qcode.OrderDescNullsFirst, qcode.OrderDescNullsLast:
			return cmp > 0
		default:
			return cmp < 0
		}
	}
	return false
}

func pagingLimit(sel *qcode.Select, vars map[string]json.RawMessage) (int, error) {
	if sel == nil {
		return 0, nil
	}
	if sel.Paging.LimitVar != "" {
		return pagingIntVar("limit", sel.Paging.LimitVar, vars, false)
	}
	return int(sel.Paging.Limit), nil
}

func pagingOffset(sel *qcode.Select, vars map[string]json.RawMessage) (int, error) {
	if sel == nil {
		return 0, nil
	}
	if sel.Paging.OffsetVar != "" {
		return pagingIntVar("offset", sel.Paging.OffsetVar, vars, true)
	}
	return int(sel.Paging.Offset), nil
}

func pagingIntVar(kind, name string, vars map[string]json.RawMessage, allowZero bool) (int, error) {
	raw := vars[name]
	if len(raw) == 0 {
		return 0, fmt.Errorf("%s variable %q is required", kind, name)
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, fmt.Errorf("%s variable %q must be an integer", kind, name)
	}
	value, err := strconv.ParseInt(number.String(), 10, 32)
	if err != nil || value < 0 || (!allowZero && value == 0) {
		qualifier := "a positive integer"
		if allowZero {
			qualifier = "a non-negative integer"
		}
		return 0, fmt.Errorf("%s variable %q must be %s", kind, name, qualifier)
	}
	return int(value), nil
}
