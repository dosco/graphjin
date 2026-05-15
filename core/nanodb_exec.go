package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/nanodb"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func (s *gstate) executeNanoDB(ctx context.Context, dbCtx *dbContext) error {
	if dbCtx == nil || dbCtx.nano == nil {
		return fmt.Errorf("nanodb database not configured")
	}
	if err := s.validateAndUpdateVars(ctx); err != nil {
		return err
	}
	s.setDefaultVars()
	raw, err := s.renderNanoDBQuery(dbCtx, s.cs.st.qc)
	if err != nil {
		return err
	}
	s.dhash = sha256.Sum256(raw)
	s.data, err = encryptValues(raw, s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	return err
}

func (s *gstate) renderNanoDBQuery(dbCtx *dbContext, qc *qcode.QCode) ([]byte, error) {
	if qc == nil {
		return []byte(`{}`), nil
	}
	out := make(map[string]any, len(qc.Roots))
	for _, id := range qc.Roots {
		sel := &qc.Selects[id]
		value, err := s.renderNanoSelect(dbCtx, qc, sel, nil)
		if err != nil {
			return nil, err
		}
		out[sel.FieldName] = value
	}
	return json.Marshal(out)
}

func (s *gstate) renderNanoSelect(
	dbCtx *dbContext,
	qc *qcode.QCode,
	sel *qcode.Select,
	parent map[int32]nanodb.Row,
) (any, error) {
	switch sel.SkipRender {
	case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked, qcode.SkipTypeNulled:
		if sel.Singular {
			return nil, nil
		}
		return []any{}, nil
	case qcode.SkipTypeDrop:
		return nil, nil
	}

	snap := dbCtx.nano.Snapshot()
	if snap == nil {
		return nil, fmt.Errorf("nanodb %s has no snapshot", dbCtx.name)
	}
	rows, ok := snap.Rows(sel.Ti.Schema, sel.Ti.Name)
	if !ok {
		return nil, fmt.Errorf("nanodb table not found: %s.%s", sel.Ti.Schema, sel.Ti.Name)
	}

	search := nanoSearchTerm(sel, s.vmap)
	parentRows := parent
	filtered := make([]nanodb.Row, 0, len(rows))
	for _, row := range rows {
		if !nanoAliasMatch(sel, row) {
			continue
		}
		if !s.nanoEval(sel.Where.Exp, row, parentRows) {
			continue
		}
		if search != "" && snap.SearchRank(sel.Ti.Schema, sel.Ti.Name, row, search) <= 0 {
			continue
		}
		filtered = append(filtered, row)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return s.nanoLess(snap, sel, filtered[i], filtered[j], search)
	})

	start := int(sel.Paging.Offset)
	if start > len(filtered) {
		start = len(filtered)
	}
	limit := int(sel.Paging.Limit)
	if limit <= 0 || sel.Paging.NoLimit {
		limit = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	filtered = filtered[start:end]

	items := make([]any, 0, len(filtered))
	for _, row := range filtered {
		item, err := s.nanoRenderRow(dbCtx, qc, sel, row, parentRows, search)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if sel.Singular {
		if len(items) == 0 {
			return nil, nil
		}
		return items[0], nil
	}
	return items, nil
}

func (s *gstate) nanoRenderRow(
	dbCtx *dbContext,
	qc *qcode.QCode,
	sel *qcode.Select,
	row nanodb.Row,
	parent map[int32]nanodb.Row,
	search string,
) (map[string]any, error) {
	snap := dbCtx.nano.Snapshot()
	out := make(map[string]any, len(sel.Fields)+len(sel.Children))
	for _, f := range sel.Fields {
		switch f.Type {
		case qcode.FieldTypeCol:
			name := f.Col.Name
			if name == "" {
				name = f.FieldName
			}
			if name == "search_rank" || name == "score" {
				out[f.FieldName] = snap.SearchRank(sel.Ti.Schema, sel.Ti.Name, row, search)
			} else {
				out[f.FieldName] = row[name]
			}
		case qcode.FieldTypeFunc:
			if f.FieldName == "search_rank" || f.FieldName == "score" || f.Func.Name == "search_rank" || f.Func.Name == "score" {
				out[f.FieldName] = snap.SearchRank(sel.Ti.Schema, sel.Ti.Name, row, search)
			}
		}
	}

	nextParent := make(map[int32]nanodb.Row, len(parent)+1)
	for k, v := range parent {
		nextParent[k] = v
	}
	nextParent[sel.ID] = row

	for _, childID := range sel.Children {
		child := &qc.Selects[childID]
		if child.SkipRender == qcode.SkipTypeDatabaseJoin {
			joinCol := child.Rel.Right.Col.Name
			if joinCol == "" {
				joinCol = child.Rel.Left.Col.Name
			}
			out["__"+child.FieldName+"_db_join"] = row[joinCol]
			continue
		}
		if child.SkipRender == qcode.SkipTypeRemote {
			continue
		}
		value, err := s.renderNanoSelect(dbCtx, qc, child, nextParent)
		if err != nil {
			return nil, err
		}
		out[child.FieldName] = value
	}
	return out, nil
}

func nanoAliasMatch(sel *qcode.Select, row nanodb.Row) bool {
	if sel == nil || sel.Ti.Name != "gj_catalog" {
		return true
	}
	switch sel.FieldName {
	case "columns":
		return fmt.Sprint(row["kind"]) == "column"
	case "relationships":
		return fmt.Sprint(row["kind"]) == "relationship"
	default:
		return true
	}
}

func nanoSearchTerm(sel *qcode.Select, vars map[string]json.RawMessage) string {
	if sel == nil {
		return ""
	}
	for _, arg := range sel.IArgs {
		if arg.Name != "search" {
			continue
		}
		if arg.Type == qcode.ArgTypeVar {
			return rawJSONScalar(vars[arg.Val])
		}
		return arg.Val
	}
	return nanoSearchTermFromExp(sel.Where.Exp, vars)
}

func nanoSearchTermFromExp(ex *qcode.Exp, vars map[string]json.RawMessage) string {
	if ex == nil {
		return ""
	}
	if ex.Op == qcode.OpTsQuery {
		return nanoRightValue(ex, nil, vars)
	}
	for _, child := range ex.Children {
		if v := nanoSearchTermFromExp(child, vars); v != "" {
			return v
		}
	}
	return ""
}

func (s *gstate) nanoLess(snap *nanodb.Snapshot, sel *qcode.Select, a, b nanodb.Row, search string) bool {
	for _, ob := range sel.OrderBy {
		col := ob.Col.Name
		var av, bv any
		if col == "search_rank" || col == "score" || ob.Alias == "search_rank" || ob.Alias == "score" {
			av = snap.SearchRank(sel.Ti.Schema, sel.Ti.Name, a, search)
			bv = snap.SearchRank(sel.Ti.Schema, sel.Ti.Name, b, search)
		} else {
			av = a[col]
			bv = b[col]
		}
		cmp := compareValues(av, bv)
		if cmp == 0 {
			continue
		}
		if ob.Order == qcode.OrderDesc {
			return cmp > 0
		}
		return cmp < 0
	}
	return false
}

func (s *gstate) nanoEval(ex *qcode.Exp, row nanodb.Row, parent map[int32]nanodb.Row) bool {
	if ex == nil {
		return true
	}
	switch ex.Op {
	case qcode.OpAnd:
		for _, child := range ex.Children {
			if !s.nanoEval(child, row, parent) {
				return false
			}
		}
		return true
	case qcode.OpOr:
		for _, child := range ex.Children {
			if s.nanoEval(child, row, parent) {
				return true
			}
		}
		return false
	case qcode.OpNot:
		if len(ex.Children) == 0 {
			return true
		}
		return !s.nanoEval(ex.Children[0], row, parent)
	case qcode.OpTsQuery:
		return true
	case qcode.OpFalse:
		return false
	}

	left := row[ex.Left.Col.Name]
	right := s.nanoRight(ex, parent)
	switch ex.Op {
	case qcode.OpEquals, qcode.OpNotDistinct:
		return equalValues(left, right)
	case qcode.OpNotEquals, qcode.OpDistinct:
		return !equalValues(left, right)
	case qcode.OpIn:
		for _, v := range nanoRightList(ex, s.vmap) {
			if equalValues(left, v) {
				return true
			}
		}
		return false
	case qcode.OpNotIn:
		for _, v := range nanoRightList(ex, s.vmap) {
			if equalValues(left, v) {
				return false
			}
		}
		return true
	case qcode.OpGreaterThan:
		return compareValues(left, right) > 0
	case qcode.OpGreaterOrEquals:
		return compareValues(left, right) >= 0
	case qcode.OpLesserThan:
		return compareValues(left, right) < 0
	case qcode.OpLesserOrEquals:
		return compareValues(left, right) <= 0
	case qcode.OpLike, qcode.OpILike:
		return likeValue(left, right, ex.Op == qcode.OpILike)
	case qcode.OpNotLike, qcode.OpNotILike:
		return !likeValue(left, right, ex.Op == qcode.OpNotILike)
	case qcode.OpRegex, qcode.OpIRegex:
		return regexValue(left, right, ex.Op == qcode.OpIRegex)
	case qcode.OpNotRegex, qcode.OpNotIRegex:
		return !regexValue(left, right, ex.Op == qcode.OpNotIRegex)
	case qcode.OpIsNull:
		return isNullish(left)
	case qcode.OpIsNotNull:
		return !isNullish(left)
	default:
		return true
	}
}

func (s *gstate) nanoRight(ex *qcode.Exp, parent map[int32]nanodb.Row) any {
	if ex == nil {
		return nil
	}
	if ex.Right.ID >= 0 && ex.Right.Col.Name != "" {
		if row, ok := parent[ex.Right.ID]; ok {
			return row[ex.Right.Col.Name]
		}
	}
	return nanoRightValue(ex, parent, s.vmap)
}

func nanoRightValue(ex *qcode.Exp, _ map[int32]nanodb.Row, vars map[string]json.RawMessage) string {
	if ex == nil {
		return ""
	}
	switch ex.Right.ValType {
	case qcode.ValVar:
		return rawJSONScalar(vars[ex.Right.Val])
	default:
		return ex.Right.Val
	}
}

func nanoRightList(ex *qcode.Exp, vars map[string]json.RawMessage) []any {
	if ex == nil {
		return nil
	}
	if ex.Right.ValType == qcode.ValVar {
		var out []any
		if err := json.Unmarshal(vars[ex.Right.Val], &out); err == nil {
			return out
		}
	}
	out := make([]any, len(ex.Right.ListVal))
	for i, v := range ex.Right.ListVal {
		out[i] = v
	}
	return out
}

func rawJSONScalar(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func equalValues(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func compareValues(a, b any) int {
	af, aok := numberValue(a)
	bf, bok := numberValue(b)
	if aok && bok {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	return strings.Compare(as, bs)
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f, err == nil
	}
}

func likeValue(a, b any, insensitive bool) bool {
	s := fmt.Sprint(a)
	pattern := fmt.Sprint(b)
	if insensitive {
		s = strings.ToLower(s)
		pattern = strings.ToLower(pattern)
	}
	pattern = strings.ReplaceAll(regexp.QuoteMeta(pattern), "%", ".*")
	pattern = strings.ReplaceAll(pattern, "_", ".")
	ok, _ := regexp.MatchString("^"+pattern+"$", s)
	return ok
}

func regexValue(a, b any, insensitive bool) bool {
	pattern := fmt.Sprint(b)
	if insensitive {
		pattern = "(?i)" + pattern
	}
	ok, _ := regexp.MatchString(pattern, fmt.Sprint(a))
	return ok
}

func isNullish(v any) bool {
	return v == nil || fmt.Sprint(v) == ""
}
