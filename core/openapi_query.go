package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// applyOpenAPIQuery applies GraphJin's common query arguments to a resolved
// OpenAPI payload. OpenAPI parameters are pushed upstream separately through
// Select.ExtraArgs; where/order_by/limit/offset are GraphJin arguments and must
// be enforced after the call because an arbitrary REST contract has no generic
// representation for them.
func applyOpenAPIQuery(body []byte, req ResolverReq) ([]byte, error) {
	sel := req.Sel
	if sel == nil || !openAPISelectNeedsProcessing(sel) {
		return body, nil
	}
	if len(sel.DistinctOn) != 0 {
		return nil, fmt.Errorf("openapi: distinct is not supported on API response fields")
	}
	if sel.Paging.Cursor || sel.Paging.CursorVar != "" || sel.Paging.Backward {
		return nil, fmt.Errorf("openapi: cursor pagination is not supported on API response fields; use limit and offset")
	}
	if err := validateOpenAPIWhere(sel.Where.Exp); err != nil {
		return nil, err
	}
	if err := validateOpenAPIOrderBy(sel.OrderBy); err != nil {
		return nil, err
	}

	payload, err := decodeOpenAPIJSON(body)
	if err != nil {
		return nil, fmt.Errorf("openapi: decode query response: %w", err)
	}

	switch value := payload.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(value))
		for i, item := range value {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("openapi: query response row %d must be a JSON object", i)
			}
			match, err := openAPIJSONMatches(row, sel.Where.Exp, req.Vars)
			if err != nil {
				return nil, err
			}
			if match {
				rows = append(rows, row)
			}
		}
		if err := sortOpenAPIRows(rows, sel.OrderBy); err != nil {
			return nil, err
		}
		offset, limit, err := openAPIPage(sel, req.Vars)
		if err != nil {
			return nil, err
		}
		rows = pageOpenAPIRows(rows, offset, limit)
		return json.Marshal(rows)

	case map[string]any:
		match, err := openAPIJSONMatches(value, sel.Where.Exp, req.Vars)
		if err != nil {
			return nil, err
		}
		offset, _, err := openAPIPage(sel, req.Vars)
		if err != nil {
			return nil, err
		}
		if !match || offset > 0 {
			return []byte("null"), nil
		}
		return json.Marshal(value)

	case nil:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("openapi: query response must be a JSON object or array, got %T", payload)
	}
}

func openAPISelectNeedsProcessing(sel *qcode.Select) bool {
	if sel == nil {
		return false
	}
	return sel.Where.Exp != nil || len(sel.OrderBy) != 0 ||
		len(sel.DistinctOn) != 0 || sel.Paging.Cursor ||
		sel.Paging.CursorVar != "" || sel.Paging.Backward ||
		sel.Paging.Limit != 0 || sel.Paging.LimitVar != "" ||
		sel.Paging.Offset != 0 || sel.Paging.OffsetVar != ""
}

func decodeOpenAPIJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func openAPIJSONMatches(row map[string]any, ex *qcode.Exp, vars map[string]json.RawMessage) (bool, error) {
	if ex == nil {
		return true, nil
	}
	switch ex.Op {
	case qcode.OpNop:
		return true, nil
	case qcode.OpAnd:
		for _, child := range ex.Children {
			ok, err := openAPIJSONMatches(row, child, vars)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case qcode.OpOr:
		for _, child := range ex.Children {
			ok, err := openAPIJSONMatches(row, child, vars)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case qcode.OpNot:
		if len(ex.Children) == 0 {
			return false, nil
		}
		ok, err := openAPIJSONMatches(row, ex.Children[0], vars)
		return !ok, err
	case qcode.OpFalse:
		return false, nil
	}

	left, exists := openAPIObjectValue(row, openAPIExpColumnName(ex))
	switch ex.Op {
	case qcode.OpIsNull:
		want, err := openAPIExpOptionalBool(ex, vars, true)
		return (!exists || left == nil) == want, err
	case qcode.OpIsNotNull:
		return exists && left != nil, nil
	case qcode.OpEqualsTrue:
		return exists && left == true, nil
	case qcode.OpNotEqualsTrue:
		return !exists || left != true, nil
	}
	if !exists || left == nil {
		return false, nil
	}

	if ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn {
		values, err := openAPIExpList(ex, vars)
		if err != nil {
			return false, err
		}
		found := false
		for _, value := range values {
			if openAPIEqual(left, value) {
				found = true
				break
			}
		}
		if ex.Op == qcode.OpNotIn {
			found = !found
		}
		return found, nil
	}

	right, err := openAPIExpScalar(ex, vars)
	if err != nil {
		return false, err
	}
	switch ex.Op {
	case qcode.OpEquals, qcode.OpNotDistinct:
		return openAPIEqual(left, right), nil
	case qcode.OpNotEquals, qcode.OpDistinct:
		return !openAPIEqual(left, right), nil
	case qcode.OpGreaterThan, qcode.OpGreaterOrEquals, qcode.OpLesserThan, qcode.OpLesserOrEquals:
		cmp, err := compareOpenAPIValues(left, right)
		if err != nil {
			return false, fmt.Errorf("openapi where column %q: %w", openAPIExpColumnName(ex), err)
		}
		switch ex.Op {
		case qcode.OpGreaterThan:
			return cmp > 0, nil
		case qcode.OpGreaterOrEquals:
			return cmp >= 0, nil
		case qcode.OpLesserThan:
			return cmp < 0, nil
		default:
			return cmp <= 0, nil
		}
	case qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike:
		insensitive := ex.Op == qcode.OpILike || ex.Op == qcode.OpNotILike
		ok, err := openAPILike(fmt.Sprint(left), fmt.Sprint(right), insensitive)
		if ex.Op == qcode.OpNotLike || ex.Op == qcode.OpNotILike {
			ok = !ok
		}
		return ok, err
	case qcode.OpRegex, qcode.OpNotRegex, qcode.OpIRegex, qcode.OpNotIRegex:
		pattern := fmt.Sprint(right)
		if ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotIRegex {
			pattern = "(?i)" + pattern
		}
		ok, err := regexp.MatchString(pattern, fmt.Sprint(left))
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ok = !ok
		}
		return ok, err
	case qcode.OpContains:
		return openAPIContains(left, right), nil
	case qcode.OpContainedIn:
		return openAPIContains(right, left), nil
	case qcode.OpHasInCommon:
		return openAPIHasInCommon(left, right), nil
	case qcode.OpHasKey:
		return openAPIHasKey(left, fmt.Sprint(right)), nil
	case qcode.OpHasKeyAny, qcode.OpHasKeyAll:
		keys, ok := right.([]any)
		if !ok {
			return false, fmt.Errorf("openapi where operator %s expects a list", ex.Op)
		}
		matched := ex.Op == qcode.OpHasKeyAll
		for _, key := range keys {
			has := openAPIHasKey(left, fmt.Sprint(key))
			if ex.Op == qcode.OpHasKeyAny && has {
				return true, nil
			}
			if ex.Op == qcode.OpHasKeyAll && !has {
				return false, nil
			}
		}
		return matched, nil
	default:
		return false, fmt.Errorf("openapi where does not support operator %s", ex.Op)
	}
}

func validateOpenAPIWhere(ex *qcode.Exp) error {
	if ex == nil {
		return nil
	}
	switch ex.Op {
	case qcode.OpNop, qcode.OpFalse:
		return nil
	case qcode.OpAnd, qcode.OpOr, qcode.OpNot:
		for _, child := range ex.Children {
			if err := validateOpenAPIWhere(child); err != nil {
				return err
			}
		}
		return nil
	case qcode.OpEquals, qcode.OpNotEquals,
		qcode.OpGreaterOrEquals, qcode.OpLesserOrEquals,
		qcode.OpGreaterThan, qcode.OpLesserThan,
		qcode.OpIn, qcode.OpNotIn,
		qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike,
		qcode.OpRegex, qcode.OpNotRegex, qcode.OpIRegex, qcode.OpNotIRegex,
		qcode.OpContains, qcode.OpContainedIn, qcode.OpHasInCommon,
		qcode.OpHasKey, qcode.OpHasKeyAny, qcode.OpHasKeyAll,
		qcode.OpIsNull, qcode.OpIsNotNull,
		qcode.OpNotDistinct, qcode.OpDistinct,
		qcode.OpEqualsTrue, qcode.OpNotEqualsTrue:
		if openAPIExpColumnName(ex) == "" {
			return fmt.Errorf("openapi where supports response columns only")
		}
		if ex.Right.Table != "" || ex.Right.Col.Name != "" || ex.Right.ColName != "" {
			return fmt.Errorf("openapi where does not support column-reference operands")
		}
		return nil
	default:
		return fmt.Errorf("openapi where does not support operator %s", ex.Op)
	}
}

func openAPIExpColumnName(ex *qcode.Exp) string {
	if ex == nil {
		return ""
	}
	if ex.Left.Col.Name != "" {
		return ex.Left.Col.Name
	}
	return ex.Left.ColName
}

func openAPIObjectValue(row map[string]any, name string) (any, bool) {
	value, ok := row[name]
	if ok {
		return value, true
	}
	for key, candidate := range row {
		if strings.EqualFold(key, name) {
			return candidate, true
		}
	}
	return nil, false
}

func openAPIExpScalar(ex *qcode.Exp, vars map[string]json.RawMessage) (any, error) {
	if ex.Right.Table != "" || ex.Right.Col.Name != "" || ex.Right.ColName != "" {
		return nil, fmt.Errorf("openapi where does not support column-reference operands")
	}
	switch ex.Right.ValType {
	case qcode.ValVar:
		return openAPIVar(ex.Right.Val, vars)
	case qcode.ValNum:
		return json.Number(ex.Right.Val), nil
	case qcode.ValBool:
		return strconv.ParseBool(ex.Right.Val)
	case qcode.ValList:
		return openAPIExpList(ex, vars)
	default:
		return ex.Right.Val, nil
	}
}

func openAPIExpOptionalBool(ex *qcode.Exp, vars map[string]json.RawMessage, fallback bool) (bool, error) {
	if ex.Right.Val == "" && ex.Right.ValType == 0 {
		return fallback, nil
	}
	value, err := openAPIExpScalar(ex, vars)
	if err != nil {
		return false, err
	}
	switch value := value.(type) {
	case bool:
		return value, nil
	case string:
		return strconv.ParseBool(value)
	default:
		return false, fmt.Errorf("openapi where expects a boolean, got %T", value)
	}
}

func openAPIExpList(ex *qcode.Exp, vars map[string]json.RawMessage) ([]any, error) {
	if ex.Right.ValType == qcode.ValVar {
		value, err := openAPIVar(ex.Right.Val, vars)
		if err != nil {
			return nil, err
		}
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("openapi variable %q must be a list", ex.Right.Val)
		}
		return list, nil
	}
	values := make([]any, 0, len(ex.Right.ListVal))
	for _, raw := range ex.Right.ListVal {
		switch ex.Right.ListType {
		case qcode.ValNum:
			values = append(values, json.Number(raw))
		case qcode.ValBool:
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		default:
			values = append(values, raw)
		}
	}
	return values, nil
}

func openAPIVar(name string, vars map[string]json.RawMessage) (any, error) {
	raw, ok := vars[name]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("openapi variable %q not found", name)
	}
	value, err := decodeOpenAPIJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("openapi variable %q: %w", name, err)
	}
	return value, nil
}

func sortOpenAPIRows(rows []map[string]any, orderBy []qcode.OrderBy) error {
	if err := validateOpenAPIOrderBy(orderBy); err != nil {
		return err
	}
	if len(orderBy) == 0 || len(rows) < 2 {
		return nil
	}
	var sortErr error
	sort.SliceStable(rows, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		for _, order := range orderBy {
			left, leftOK := openAPIObjectValue(rows[i], order.Col.Name)
			right, rightOK := openAPIObjectValue(rows[j], order.Col.Name)
			leftNull := !leftOK || left == nil
			rightNull := !rightOK || right == nil
			if leftNull || rightNull {
				if leftNull && rightNull {
					continue
				}
				nullsFirst := order.Order == qcode.OrderAscNullsFirst || order.Order == qcode.OrderDescNullsFirst
				if leftNull {
					return nullsFirst
				}
				return !nullsFirst
			}
			cmp, err := compareOpenAPIValues(left, right)
			if err != nil {
				sortErr = fmt.Errorf("openapi order_by column %q: %w", order.Col.Name, err)
				return false
			}
			if cmp == 0 {
				continue
			}
			if openAPIOrderDesc(order.Order) {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	return sortErr
}

func validateOpenAPIOrderBy(orderBy []qcode.OrderBy) error {
	for _, order := range orderBy {
		if order.Alias != "" || order.Col.Name == "" || order.IsFunc ||
			order.Key != "" || order.KeyVar != "" || order.Var != "" {
			return fmt.Errorf("openapi order_by supports response columns only")
		}
	}
	return nil
}

func openAPIOrderDesc(order qcode.Order) bool {
	return order == qcode.OrderDesc || order == qcode.OrderDescNullsFirst || order == qcode.OrderDescNullsLast
}

func openAPIPage(sel *qcode.Select, vars map[string]json.RawMessage) (offset, limit int, err error) {
	if sel == nil {
		return 0, 0, nil
	}
	if sel.Paging.OffsetVar != "" {
		offset, err = openAPIIntVar("offset", sel.Paging.OffsetVar, vars)
		if err != nil {
			return 0, 0, err
		}
	} else if sel.Paging.Offset > 0 {
		offset = int(sel.Paging.Offset)
	}
	if sel.Paging.NoLimit {
		return offset, 0, nil
	}
	if sel.Paging.LimitVar != "" {
		limit, err = openAPIIntVar("limit", sel.Paging.LimitVar, vars)
		return offset, limit, err
	}
	if sel.Paging.Limit > 0 {
		limit = int(sel.Paging.Limit)
	}
	return offset, limit, nil
}

func openAPIIntVar(arg, name string, vars map[string]json.RawMessage) (int, error) {
	value, err := openAPIVar(name, vars)
	if err != nil {
		return 0, err
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("openapi %s variable %q must be an integer", arg, name)
	}
	n, err := strconv.ParseInt(string(number), 10, 32)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("openapi %s variable %q must be a non-negative integer", arg, name)
	}
	return int(n), nil
}

func pageOpenAPIRows(rows []map[string]any, offset, limit int) []map[string]any {
	if offset >= len(rows) {
		return []map[string]any{}
	}
	if offset > 0 {
		rows = rows[offset:]
	}
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return rows
}

func compareOpenAPIValues(left, right any) (int, error) {
	if lf, ok := openAPINumber(left); ok {
		rf, ok := openAPINumber(right)
		if !ok {
			return 0, fmt.Errorf("cannot compare number with %T", right)
		}
		switch {
		case lf < rf:
			return -1, nil
		case lf > rf:
			return 1, nil
		default:
			return 0, nil
		}
	}
	switch value := left.(type) {
	case string:
		rightValue, ok := right.(string)
		if !ok {
			return 0, fmt.Errorf("cannot compare string with %T", right)
		}
		return strings.Compare(value, rightValue), nil
	case bool:
		rightValue, ok := right.(bool)
		if !ok {
			return 0, fmt.Errorf("cannot compare boolean with %T", right)
		}
		if value == rightValue {
			return 0, nil
		}
		if !value {
			return -1, nil
		}
		return 1, nil
	default:
		if openAPIEqual(left, right) {
			return 0, nil
		}
		return 0, fmt.Errorf("values %T and %T are not orderable", left, right)
	}
}

func openAPINumber(value any) (float64, bool) {
	switch value := value.(type) {
	case json.Number:
		n, err := value.Float64()
		return n, err == nil
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case int32:
		return float64(value), true
	default:
		return 0, false
	}
}

func openAPIEqual(left, right any) bool {
	if lf, ok := openAPINumber(left); ok {
		rf, ok := openAPINumber(right)
		return ok && lf == rf
	}
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	switch leftValue := left.(type) {
	case string:
		rightValue, ok := right.(string)
		return ok && leftValue == rightValue
	case bool:
		rightValue, ok := right.(bool)
		return ok && leftValue == rightValue
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for i := range leftValue {
			if !openAPIEqual(leftValue[i], rightValue[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, leftItem := range leftValue {
			rightItem, exists := rightValue[key]
			if !exists || !openAPIEqual(leftItem, rightItem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func openAPILike(value, pattern string, insensitive bool) (bool, error) {
	var b strings.Builder
	b.WriteByte('^')
	for _, r := range pattern {
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteByte('$')
	expr := b.String()
	if insensitive {
		expr = "(?i)" + expr
	}
	return regexp.MatchString(expr, value)
}

func openAPIContains(container, candidate any) bool {
	switch container := container.(type) {
	case string:
		return strings.Contains(container, fmt.Sprint(candidate))
	case []any:
		candidateList, isList := candidate.([]any)
		if !isList {
			for _, value := range container {
				if openAPIEqual(value, candidate) {
					return true
				}
			}
			return false
		}
		for _, wanted := range candidateList {
			if !openAPIContains(container, wanted) {
				return false
			}
		}
		return true
	case map[string]any:
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			return false
		}
		for key, wanted := range candidateMap {
			actual, exists := container[key]
			if !exists || !openAPIEqual(actual, wanted) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func openAPIHasInCommon(left, right any) bool {
	leftList, leftOK := left.([]any)
	rightList, rightOK := right.([]any)
	if !leftOK || !rightOK {
		return false
	}
	for _, leftValue := range leftList {
		for _, rightValue := range rightList {
			if openAPIEqual(leftValue, rightValue) {
				return true
			}
		}
	}
	return false
}

func openAPIHasKey(value any, key string) bool {
	switch value := value.(type) {
	case map[string]any:
		_, ok := value[key]
		return ok
	case []any:
		index, err := strconv.Atoi(key)
		return err == nil && index >= 0 && index < len(value)
	default:
		return false
	}
}
