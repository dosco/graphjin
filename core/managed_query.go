package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/nanodb"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (gj *graphjinEngine) initManagedQueryTables() error {
	if len(gj.managedQueryHandlers) == 0 {
		return nil
	}
	for dbName, handler := range gj.managedQueryHandlers {
		if err := gj.initManagedQueryTablesForDatabase(dbName, handler); err != nil {
			return err
		}
	}
	return nil
}

func (gj *graphjinEngine) initManagedQueryTablesForDatabase(dbName string, handler ManagedQueryHandler) error {
	if handler == nil {
		return nil
	}
	dbctx := gj.databases[dbName]
	if dbctx == nil {
		return fmt.Errorf("managed query handler database %q not configured", dbName)
	}
	if dbctx.dbinfo == nil {
		return fmt.Errorf("managed query handler database %q has no schema metadata", dbName)
	}
	schema := dbctx.dbinfo.Schema
	for _, table := range handler.ManagedQueryTables() {
		if strings.TrimSpace(table.Name) == "" {
			return fmt.Errorf("managed query table name is required")
		}
		if !strings.HasPrefix(table.Name, "gj_") {
			return fmt.Errorf("managed query table %q must use reserved gj_ prefix", table.Name)
		}
		if existing, err := dbctx.dbinfo.GetTable(schema, table.Name); err == nil {
			if dbctx.nano != nil {
				continue
			}
			switch existing.Type {
			case "managed", "nanodb":
				continue
			default:
				return fmt.Errorf("reserved GraphJin system table %q conflicts with an existing table", table.Name)
			}
		}
		dbctx.dbinfo.AddTable(managedDBTable(schema, table))
	}
	return nil
}

func managedDBTable(schema string, table ManagedTable) sdata.DBTable {
	cols := make([]sdata.DBColumn, 0, len(table.Columns))
	for i, col := range table.Columns {
		typ := strings.TrimSpace(col.Type)
		if typ == "" {
			typ = "text"
		}
		cols = append(cols, sdata.DBColumn{
			ID:         int32(i),
			Schema:     schema,
			Table:      table.Name,
			Name:       col.Name,
			Type:       typ,
			PrimaryKey: col.PrimaryKey,
			FullText:   col.FullText,
		})
	}
	return sdata.NewDBTable(schema, table.Name, "managed", cols)
}

func (s *gstate) executeManagedQuery(c context.Context) (bool, error) {
	handled, data, err := s.executeManagedQueryRaw(c, false)
	if !handled || err != nil {
		return handled, err
	}
	s.data = data
	s.dhash = sha256.Sum256(s.data)
	s.data, err = encryptValues(s.data,
		s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	return true, err
}

func (s *gstate) executeManagedQueryRaw(c context.Context, forSubscription bool) (bool, json.RawMessage, error) {
	if (s.r.operation != qcode.QTQuery && !forSubscription) || s.cs == nil || s.cs.st.qc == nil {
		return false, nil, nil
	}
	if forSubscription && s.r.operation != qcode.QTSubscription {
		return false, nil, nil
	}
	if !forSubscription && s.r.operation != qcode.QTQuery {
		return false, nil, nil
	}

	dbName := s.database
	if dbName == "" {
		dbName = s.gj.defaultDB
	}
	handler := s.gj.managedQueryHandlers[dbName]
	if handler == nil {
		return false, nil, nil
	}

	managedTables := make(map[string]struct{})
	for _, table := range handler.ManagedQueryTables() {
		managedTables[strings.ToLower(table.Name)] = struct{}{}
	}

	qc := s.cs.st.qc
	var managedRoots, normalRoots int
	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		if _, ok := managedTables[strings.ToLower(sel.Ti.Name)]; ok {
			// Role rules blocked this root; the handler answers from service
			// state, not the role-filtered SQL/nano path, so serving it here
			// would bypass the block.
			if sel.SkipRender == qcode.SkipTypeBlocked {
				return true, nil, fmt.Errorf("query blocked: %s (role: %s)", sel.FieldName, s.role)
			}
			managedRoots++
		} else {
			normalRoots++
		}
	}

	if managedRoots == 0 {
		return false, nil, nil
	}
	if normalRoots != 0 {
		return true, nil, fmt.Errorf("GraphJin system roots cannot be mixed with normal database roots; run discovery/control-plane calls separately")
	}

	req := ManagedQueryRequest{
		Database: dbName,
		Roots:    make([]ManagedQueryRoot, 0, len(qc.Roots)),
	}
	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		root := ManagedQueryRoot{
			FieldName: sel.FieldName,
			Table:     sel.Ti.Name,
			Fields:    managedSelectedFields(sel.Fields),
			Where:     managedFilterToValueWithoutSeek(sel.Where.Exp, s.vmap),
			OrderBy:   managedOrderBy(sel.OrderBy),
			Limit:     int(sel.Paging.Limit),
			Offset:    int(sel.Paging.Offset),
		}
		if sel.Paging.Cursor {
			root.Limit = 0
			root.Offset = 0
			root.Fields = appendManagedCursorFields(root.Fields, &sel)
		}
		req.Roots = append(req.Roots, root)
	}

	data, err := handler.ExecuteManagedQuery(c, req)
	if err != nil {
		return true, nil, err
	}
	data, err = s.pageManagedQueryResult(c, data, qc)
	return true, data, err
}

func appendManagedCursorFields(fields []ManagedMutationField, sel *qcode.Select) []ManagedMutationField {
	used := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		used[field.Name] = struct{}{}
	}
	for _, order := range sel.OrderBy {
		name := managedCursorFieldName(order.Col.Name, used)
		used[name] = struct{}{}
		fields = append(fields, ManagedMutationField{Name: name, Column: order.Col.Name})
	}
	return fields
}

func managedCursorFieldName(column string, used map[string]struct{}) string {
	base := "__gj_cursor_" + column
	name := base
	for index := 2; ; index++ {
		if _, exists := used[name]; !exists {
			return name
		}
		name = base + "_" + strconv.Itoa(index)
	}
}

func (s *gstate) pageManagedQueryResult(
	ctx context.Context,
	data json.RawMessage,
	qc *qcode.QCode,
) (json.RawMessage, error) {
	hasCursor := false
	for _, rootID := range qc.Roots {
		if qc.Selects[rootID].Paging.Cursor {
			hasCursor = true
			break
		}
	}
	if !hasCursor {
		return data, nil
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("managed query result must be an object: %w", err)
	}
	for _, rootID := range qc.Roots {
		sel := &qc.Selects[rootID]
		if !sel.Paging.Cursor {
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal(top[sel.FieldName], &rows); err != nil {
			return nil, fmt.Errorf("managed query root %s must return an array: %w", sel.FieldName, err)
		}
		orderFields, err := managedCursorOrderFields(sel, rows)
		if err != nil {
			return nil, err
		}
		checkpoint := checkpointForSelect(sel, s.vmap)
		seek := findSeekExp(sel.Where.Exp)
		filtered := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			evalRow := make(nanodb.Row, len(row)+len(orderFields))
			for key, value := range row {
				evalRow[key] = value
			}
			for index, order := range sel.OrderBy {
				evalRow[order.Col.Name] = row[orderFields[index]]
			}
			if seek != nil && !s.nanoEval(ctx, seek, evalRow, nil, checkpoint) {
				continue
			}
			filtered = append(filtered, row)
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			return managedRowLess(sel, filtered[i], filtered[j], orderFields)
		})
		start, err := pagingOffset(sel, s.vmap)
		if err != nil {
			return nil, err
		}
		if start > len(filtered) {
			start = len(filtered)
		}
		limit, err := pagingLimit(sel, s.vmap)
		if err != nil {
			return nil, err
		}
		end := len(filtered)
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		filtered = filtered[start:end]

		var cursor any
		if len(filtered) != 0 {
			last := managedOrderRow(sel, filtered[len(filtered)-1], orderFields)
			cursor = string(s.gj.printFormat) + encodeSystemCursor(sel.ID, systemCursorValues(sel, last))
		}
		for _, row := range filtered {
			for _, field := range orderFields {
				delete(row, field)
			}
		}
		rootData, err := json.Marshal(filtered)
		if err != nil {
			return nil, err
		}
		cursorData, err := json.Marshal(cursor)
		if err != nil {
			return nil, err
		}
		top[sel.FieldName] = rootData
		top[sel.FieldName+"_cursor"] = cursorData
	}
	return json.Marshal(top)
}

func managedCursorOrderFields(sel *qcode.Select, rows []map[string]any) ([]string, error) {
	used := make(map[string]struct{})
	for _, field := range managedSelectedFields(sel.Fields) {
		used[field.Name] = struct{}{}
	}
	names := make([]string, 0, len(sel.OrderBy))
	for _, order := range sel.OrderBy {
		name := managedCursorFieldName(order.Col.Name, used)
		used[name] = struct{}{}
		names = append(names, name)
		for _, row := range rows {
			value, exists := row[name]
			if !exists {
				return nil, fmt.Errorf("managed query handler must return cursor order column %q as %q", order.Col.Name, name)
			}
			if value == nil && order.Col.NotNull {
				return nil, fmt.Errorf("managed query handler returned null for non-null cursor order column %q", order.Col.Name)
			}
		}
	}
	return names, nil
}

func managedOrderRow(sel *qcode.Select, row map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for index, order := range sel.OrderBy {
		out[order.Col.Name] = row[fields[index]]
	}
	return out
}

func managedRowLess(sel *qcode.Select, left, right map[string]any, fields []string) bool {
	return systemRowLess(sel, managedOrderRow(sel, left, fields), managedOrderRow(sel, right, fields))
}

func managedSelectedFields(fields []qcode.Field) []ManagedMutationField {
	out := make([]ManagedMutationField, 0, len(fields))
	for _, f := range fields {
		if f.Type != qcode.FieldTypeCol {
			continue
		}
		out = append(out, ManagedMutationField{Name: f.FieldName, Column: f.Col.Name})
	}
	return out
}

func managedOrderBy(orderBy []qcode.OrderBy) []ManagedOrderBy {
	out := make([]ManagedOrderBy, 0, len(orderBy))
	for _, ob := range orderBy {
		out = append(out, ManagedOrderBy{
			Column: ob.Col.Name,
			Order:  ob.Order.String(),
		})
	}
	return out
}

func managedFilterToValue(ex *qcode.Exp, vars map[string]json.RawMessage) map[string]interface{} {
	if ex == nil {
		return nil
	}
	v := managedExpToValue(ex, vars)
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"expr": v}
}

func managedFilterToValueWithoutSeek(ex *qcode.Exp, vars map[string]json.RawMessage) map[string]interface{} {
	if ex == nil {
		return nil
	}
	v := managedExpToValueWithoutSeek(ex, vars)
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"expr": v}
}

func managedExpToValueWithoutSeek(ex *qcode.Exp, vars map[string]json.RawMessage) interface{} {
	if ex == nil || isSeekExp(ex) {
		return nil
	}
	switch ex.Op {
	case qcode.OpAnd, qcode.OpOr:
		key := "and"
		if ex.Op == qcode.OpOr {
			key = "or"
		}
		items := make([]interface{}, 0, len(ex.Children))
		for _, child := range ex.Children {
			if value := managedExpToValueWithoutSeek(child, vars); value != nil {
				items = append(items, value)
			}
		}
		switch len(items) {
		case 0:
			return nil
		case 1:
			return items[0]
		default:
			return map[string]interface{}{key: items}
		}
	case qcode.OpNot:
		if len(ex.Children) == 0 {
			return nil
		}
		value := managedExpToValueWithoutSeek(ex.Children[0], vars)
		if value == nil {
			return nil
		}
		return map[string]interface{}{"not": value}
	default:
		return managedExpToValue(ex, vars)
	}
}

func managedExpToValue(ex *qcode.Exp, vars map[string]json.RawMessage) interface{} {
	if ex == nil {
		return nil
	}
	switch ex.Op {
	case qcode.OpAnd, qcode.OpOr:
		key := "and"
		if ex.Op == qcode.OpOr {
			key = "or"
		}
		items := make([]interface{}, 0, len(ex.Children))
		for _, child := range ex.Children {
			if v := managedExpToValue(child, vars); v != nil {
				items = append(items, v)
			}
		}
		if len(items) == 1 {
			return items[0]
		}
		return map[string]interface{}{key: items}
	case qcode.OpNot:
		if len(ex.Children) == 0 {
			return nil
		}
		return map[string]interface{}{"not": managedExpToValue(ex.Children[0], vars)}
	case qcode.OpTsQuery:
		return map[string]interface{}{"search": managedRightValue(ex, vars)}
	case qcode.OpIsNull, qcode.OpIsNotNull:
		col := managedLeftColumn(ex)
		if col == "" {
			return nil
		}
		return map[string]interface{}{col: map[string]interface{}{"is_null": ex.Op == qcode.OpIsNull}}
	default:
		col := managedLeftColumn(ex)
		if col == "" {
			return nil
		}
		op := managedOpName(ex.Op)
		if op == "" {
			return nil
		}
		return map[string]interface{}{col: map[string]interface{}{op: managedRightValue(ex, vars)}}
	}
}

func managedLeftColumn(ex *qcode.Exp) string {
	if ex.Left.Col.Name != "" {
		return ex.Left.Col.Name
	}
	return ex.Left.ColName
}

func managedOpName(op qcode.ExpOp) string {
	switch op {
	case qcode.OpEquals:
		return "eq"
	case qcode.OpNotEquals:
		return "neq"
	case qcode.OpGreaterThan:
		return "gt"
	case qcode.OpGreaterOrEquals:
		return "gte"
	case qcode.OpLesserThan:
		return "lt"
	case qcode.OpLesserOrEquals:
		return "lte"
	case qcode.OpIn:
		return "in"
	case qcode.OpNotIn:
		return "nin"
	case qcode.OpLike:
		return "like"
	case qcode.OpILike:
		return "ilike"
	case qcode.OpRegex:
		return "regex"
	case qcode.OpIRegex:
		return "iregex"
	case qcode.OpContains:
		return "contains"
	default:
		return ""
	}
}

func managedRightValue(ex *qcode.Exp, vars map[string]json.RawMessage) interface{} {
	switch ex.Right.ValType {
	case qcode.ValVar:
		raw := vars[ex.Right.Val]
		if len(raw) == 0 {
			return nil
		}
		var out interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return string(raw)
		}
		return out
	case qcode.ValList:
		out := make([]interface{}, 0, len(ex.Right.ListVal))
		for _, value := range ex.Right.ListVal {
			out = append(out, value)
		}
		return out
	case qcode.ValNum:
		if i, err := strconv.ParseInt(ex.Right.Val, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(ex.Right.Val, 64); err == nil {
			return f
		}
		return ex.Right.Val
	case qcode.ValBool:
		v, err := strconv.ParseBool(ex.Right.Val)
		if err != nil {
			return ex.Right.Val
		}
		return v
	default:
		return ex.Right.Val
	}
}
