package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (gj *graphjinEngine) initManagedQueryTables() error {
	if len(gj.managedQueryHandlers) == 0 {
		return nil
	}
	for dbName, handler := range gj.managedQueryHandlers {
		dbctx := gj.databases[dbName]
		if dbctx == nil {
			return fmt.Errorf("managed query handler database %q not configured", dbName)
		}
		if dbctx.dbinfo == nil {
			return fmt.Errorf("managed query handler database %q has no schema metadata", dbName)
		}
		schema := dbctx.dbinfo.Schema
		for _, table := range dbctx.dbinfo.Tables {
			if strings.HasPrefix(strings.ToLower(table.Name), "gj_") {
				return fmt.Errorf("reserved GraphJin system table prefix gj_ conflicts with existing table %q", table.Name)
			}
		}
		for _, table := range handler.ManagedQueryTables() {
			if strings.TrimSpace(table.Name) == "" {
				return fmt.Errorf("managed query table name is required")
			}
			if !strings.HasPrefix(table.Name, "gj_") {
				return fmt.Errorf("managed query table %q must use reserved gj_ prefix", table.Name)
			}
			if _, err := dbctx.dbinfo.GetTable(schema, table.Name); err == nil {
				return fmt.Errorf("reserved GraphJin system table %q conflicts with an existing table", table.Name)
			}
			dbctx.dbinfo.AddTable(managedDBTable(schema, table))
		}
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
	if s.r.operation != qcode.QTQuery || s.cs == nil || s.cs.st.qc == nil {
		return false, nil
	}

	dbName := s.database
	if dbName == "" {
		dbName = s.gj.defaultDB
	}
	handler := s.gj.managedQueryHandlers[dbName]
	if handler == nil {
		return false, nil
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
			managedRoots++
		} else {
			normalRoots++
		}
	}

	if managedRoots == 0 {
		return false, nil
	}
	if normalRoots != 0 {
		return true, fmt.Errorf("GraphJin system roots cannot be mixed with normal database roots; run discovery/control-plane calls separately")
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
			Where:     managedFilterToValue(sel.Where.Exp, s.vmap),
			OrderBy:   managedOrderBy(sel.OrderBy),
			Limit:     int(sel.Paging.Limit),
			Offset:    int(sel.Paging.Offset),
		}
		req.Roots = append(req.Roots, root)
	}

	data, err := handler.ExecuteManagedQuery(c, req)
	if err != nil {
		return true, err
	}
	s.data = data
	s.dhash = sha256.Sum256(s.data)
	s.data, err = encryptValues(s.data,
		s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	return true, err
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
