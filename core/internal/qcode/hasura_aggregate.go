package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const hasuraAggregateSuffix = "_aggregate"

// HasuraAggregateField maps one native GraphJin aggregate field to its
// requested nested Hasura-compatible response path.
type HasuraAggregateField struct {
	NativeField string
	Path        []string
}

// HasuraAggregateRoot describes one lowered <table>_aggregate query root.
type HasuraAggregateRoot struct {
	ResponseKey string
	Fields      []HasuraAggregateField
}

var hasuraAggregateFunctions = map[string]bool{
	"sum": true, "avg": true, "min": true, "max": true,
	"stddev": true, "variance": true,
}

// rewriteHasuraAggregates lowers the small, useful Hasura aggregate dialect
// into GraphJin's native aggregate fields before ordinary qcode compilation.
// It intentionally does not implement Hasura nodes, distinct count, or
// per-field aliases: those shapes fail loudly instead of being miscompiled.
func (co *Compiler) rewriteHasuraAggregates(op *graph.Operation) ([]HasuraAggregateRoot, error) {
	if op == nil {
		return nil, nil
	}

	var plans []HasuraAggregateRoot
	for i := range op.Fields {
		root := &op.Fields[i]
		if root.ParentID != -1 || !strings.HasSuffix(root.Name, hasuraAggregateSuffix) {
			continue
		}

		// An exact schema object always wins. This prevents the compatibility
		// syntax from shadowing a real table named, for example, audit_aggregate.
		if _, err := co.Find("", co.ParseName(root.Name)); err == nil {
			continue
		}

		baseName := strings.TrimSuffix(root.Name, hasuraAggregateSuffix)
		table, err := co.Find("", co.ParseName(baseName))
		if err != nil {
			continue
		}
		if op.Type == graph.OpSub {
			return nil, fmt.Errorf("Hasura-compatible aggregate root %q is not supported in subscriptions; use a query operation", root.Name)
		}
		if op.Type != graph.OpQuery {
			continue
		}

		plan, err := co.rewriteHasuraAggregateRoot(op, root, table, baseName)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (co *Compiler) rewriteHasuraAggregateRoot(op *graph.Operation, root *graph.Field, table sdata.DBTable, baseName string) (HasuraAggregateRoot, error) {
	requestedRoot := root.Name
	responseKey := requestedRoot
	if root.Alias != "" {
		responseKey = root.Alias
	}

	if len(root.Children) == 0 {
		return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q requires an aggregate selection", requestedRoot)
	}

	var aggregate *graph.Field
	for _, childID := range root.Children {
		child := &op.Fields[childID]
		switch child.Name {
		case "aggregate":
			if aggregate != nil {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q contains aggregate more than once", requestedRoot)
			}
			aggregate = child
		case "nodes":
			return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q does not support nodes; query the %q table root separately", requestedRoot, baseName)
		default:
			return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q supports only the aggregate selection; found %q", requestedRoot, child.Name)
		}
	}
	if aggregate == nil {
		return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q requires an aggregate selection", requestedRoot)
	}
	if aggregate.Alias != "" {
		return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q does not support an alias on aggregate", requestedRoot)
	}
	if len(aggregate.Args) != 0 || len(aggregate.Directives) != 0 {
		return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q does not support arguments or directives on aggregate", requestedRoot)
	}

	plan := HasuraAggregateRoot{ResponseKey: responseKey}
	nativeChildren := make([]int32, 0, len(aggregate.Children))
	seenNative := make(map[string]bool)
	for _, aggregateChildID := range aggregate.Children {
		field := &op.Fields[aggregateChildID]
		if field.Alias != "" {
			return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q on %q does not support aliases", field.Name, requestedRoot)
		}
		if len(field.Directives) != 0 {
			return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q on %q does not support directives", field.Name, requestedRoot)
		}

		switch field.Name {
		case "count":
			if len(field.Args) != 0 {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible count arguments such as columns or distinct are not supported on %q; use a filtered table root with count_<column>", requestedRoot)
			}
			if len(field.Children) != 0 {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible count on %q must be a scalar field", requestedRoot)
			}
			column, err := hasuraCountColumn(table)
			if err != nil {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible count on %q: %w", requestedRoot, err)
			}
			native := "count_" + column
			if seenNative[native] {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q is selected more than once on %q", native, requestedRoot)
			}
			seenNative[native] = true
			field.Name = native
			field.ParentID = root.ID
			nativeChildren = append(nativeChildren, field.ID)
			plan.Fields = append(plan.Fields, HasuraAggregateField{NativeField: native, Path: []string{"aggregate", "count"}})

		default:
			if !hasuraAggregateFunctions[field.Name] {
				return HasuraAggregateRoot{}, fmt.Errorf("unsupported Hasura-compatible aggregate field %q on %q; supported fields are count, sum, avg, min, max, stddev, and variance", field.Name, requestedRoot)
			}
			if len(field.Args) != 0 {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q on %q does not support arguments", field.Name, requestedRoot)
			}
			if len(field.Children) == 0 {
				return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q on %q requires at least one column", field.Name, requestedRoot)
			}
			functionName := field.Name
			for _, columnID := range field.Children {
				columnField := &op.Fields[columnID]
				if columnField.Alias != "" {
					return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate column %q under %s on %q does not support aliases", columnField.Name, functionName, requestedRoot)
				}
				if len(columnField.Args) != 0 || len(columnField.Directives) != 0 || len(columnField.Children) != 0 {
					return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate column %q under %s on %q must be a plain scalar selection", columnField.Name, functionName, requestedRoot)
				}
				columnName := co.ParseName(columnField.Name)
				if _, ok := table.GetColumnIndex(columnName); !ok {
					return HasuraAggregateRoot{}, fmt.Errorf("column %q is not present on table %q", columnField.Name, table.Name)
				}
				native := functionName + "_" + columnField.Name
				if seenNative[native] {
					return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate field %q is selected more than once on %q", native, requestedRoot)
				}
				seenNative[native] = true
				columnField.Name = native
				columnField.ParentID = root.ID
				nativeChildren = append(nativeChildren, columnField.ID)
				plan.Fields = append(plan.Fields, HasuraAggregateField{
					NativeField: native,
					Path:        []string{"aggregate", functionName, columnField.Name[len(functionName)+1:]},
				})
			}
		}
	}
	if len(plan.Fields) == 0 {
		return HasuraAggregateRoot{}, fmt.Errorf("Hasura-compatible aggregate root %q requires at least one aggregate field", requestedRoot)
	}

	root.Name = baseName
	root.Alias = responseKey
	root.Children = nativeChildren
	return plan, nil
}

func hasuraCountColumn(table sdata.DBTable) (string, error) {
	if table.PrimaryCol.Name != "" {
		return table.PrimaryCol.Name, nil
	}
	for _, column := range table.Columns {
		if column.NotNull {
			return column.Name, nil
		}
	}
	return "", fmt.Errorf("table %q has no primary key or non-null column to count", table.Name)
}
