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
			return nil, co.unknownHasuraAggregateRootError(root.Name, baseName)
		}
		if op.Type == graph.OpSub {
			return nil, hasuraAggregateSupportError(root.Name, baseName, table,
				"subscription roots are not supported; use a query operation")
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
	unsupported := func(detail string) error {
		return hasuraAggregateSupportError(requestedRoot, baseName, table, detail)
	}

	if len(root.Children) == 0 {
		return HasuraAggregateRoot{}, unsupported("an aggregate selection is required")
	}

	var aggregate *graph.Field
	var shallowChildren []int32
	for _, childID := range root.Children {
		child := &op.Fields[childID]
		switch child.Name {
		case "aggregate":
			if aggregate != nil {
				return HasuraAggregateRoot{}, unsupported("aggregate may be selected only once")
			}
			aggregate = child
		case "nodes":
			return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("nodes is not supported; query the %q table root separately", baseName))
		default:
			if !isHasuraAggregateFunction(child.Name) {
				return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("only aggregate function fields are supported; found %q", child.Name))
			}
			shallowChildren = append(shallowChildren, childID)
		}
	}
	if aggregate != nil && len(shallowChildren) != 0 {
		return HasuraAggregateRoot{}, unsupported("the aggregate wrapper and shallow aggregate fields cannot be mixed")
	}

	aggregateChildren := shallowChildren
	pathPrefix := []string(nil)
	if aggregate != nil {
		if aggregate.Alias != "" {
			return HasuraAggregateRoot{}, unsupported("an alias on aggregate is not supported")
		}
		if len(aggregate.Args) != 0 || len(aggregate.Directives) != 0 {
			return HasuraAggregateRoot{}, unsupported("arguments or directives on aggregate are not supported")
		}
		aggregateChildren = aggregate.Children
		pathPrefix = []string{"aggregate"}
	}
	if len(aggregateChildren) == 0 {
		return HasuraAggregateRoot{}, unsupported("at least one aggregate field is required")
	}

	plan := HasuraAggregateRoot{ResponseKey: responseKey}
	nativeChildren := make([]int32, 0, len(aggregateChildren))
	seenNative := make(map[string]bool)
	for _, aggregateChildID := range aggregateChildren {
		field := &op.Fields[aggregateChildID]
		if field.Alias != "" {
			return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("alias %q on aggregate field %q is not supported", field.Alias, field.Name))
		}
		if len(field.Directives) != 0 {
			return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("directives on aggregate field %q are not supported", field.Name))
		}

		switch field.Name {
		case "count":
			if len(field.Args) != 0 {
				return HasuraAggregateRoot{}, unsupported("count arguments such as columns or distinct are not supported; filter the aggregate root instead")
			}
			if len(field.Children) != 0 {
				return HasuraAggregateRoot{}, unsupported("count must be a scalar field")
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
			plan.Fields = append(plan.Fields, HasuraAggregateField{NativeField: native, Path: aggregateResponsePath(pathPrefix, "count")})

		default:
			if !hasuraAggregateFunctions[field.Name] {
				return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("aggregate field %q is unsupported", field.Name))
			}
			if len(field.Args) != 0 {
				return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("arguments on aggregate field %q are not supported", field.Name))
			}
			if len(field.Children) == 0 {
				return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("aggregate field %q requires at least one column", field.Name))
			}
			functionName := field.Name
			for _, columnID := range field.Children {
				columnField := &op.Fields[columnID]
				responseColumn := columnField.Name
				if columnField.Alias != "" {
					return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("alias %q on column %q under %s is not supported", columnField.Alias, columnField.Name, functionName))
				}
				if len(columnField.Args) != 0 || len(columnField.Directives) != 0 || len(columnField.Children) != 0 {
					return HasuraAggregateRoot{}, unsupported(fmt.Sprintf("column %q under %s must be a plain scalar selection", columnField.Name, functionName))
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
					Path:        aggregateResponsePath(pathPrefix, functionName, responseColumn),
				})
			}
		}
	}
	if len(plan.Fields) == 0 {
		return HasuraAggregateRoot{}, unsupported("at least one aggregate field is required")
	}

	root.Name = baseName
	root.Alias = responseKey
	root.Children = nativeChildren
	return plan, nil
}

func isHasuraAggregateFunction(name string) bool {
	return name == "count" || hasuraAggregateFunctions[name]
}

func aggregateResponsePath(prefix []string, parts ...string) []string {
	path := make([]string, 0, len(prefix)+len(parts))
	path = append(path, prefix...)
	return append(path, parts...)
}

func (co *Compiler) unknownHasuraAggregateRootError(requestedRoot, baseName string) error {
	decorate := func(name string) string { return name + hasuraAggregateSuffix }
	suggestions := decorateTableSuggestions(co.suggestTableNames(baseName), decorate)
	if hint := didYouMeanClause(suggestions); hint != "" {
		return fmt.Errorf("unknown Hasura-compatible aggregate root %q: table %q was not found%s",
			requestedRoot, baseName, hint)
	}
	// Same gap the plain root had, and the one the measured run still hit after
	// that was closed: every remaining dead end was `users_aggregate`, a base
	// name no edit distance reaches from any real table. The roots are decorated
	// here, so the fallback names them the way they would have to be written.
	return fmt.Errorf("unknown Hasura-compatible aggregate root %q: table %q was not found%s",
		requestedRoot, baseName,
		sdata.TableHint(co.ParseName(baseName), decorateTableSuggestions(co.tableNames(), decorate)))
}

func hasAggregateFunctionChildren(op *graph.Operation, field graph.Field) bool {
	if len(field.Children) == 0 {
		return false
	}
	for _, childID := range field.Children {
		if !isHasuraAggregateFunction(op.Fields[childID].Name) {
			return false
		}
	}
	return true
}

func (co *Compiler) hasSchemaFieldOrRelationship(table sdata.DBTable, name string) bool {
	if _, ok := table.ColumnExists(name); ok {
		return true
	}
	rels, err := co.s.GetFirstDegree(table)
	if err != nil {
		return false
	}
	for _, rel := range rels {
		if co.ParseName(rel.Name) == name || co.ParseName(rel.Table.Name) == name {
			return true
		}
	}
	return false
}

func hasuraAggregateSupportError(requestedRoot, baseName string, table sdata.DBTable, detail string) error {
	countColumn := "<primary_key>"
	if column, err := hasuraCountColumn(table); err == nil {
		countColumn = column
	}
	return fmt.Errorf(
		"Hasura-compatible aggregate root %q: %s. Supported form: %s { aggregate { count sum { <column> } avg { <column> } min { <column> } max { <column> } } }. Native equivalent: %s { count_%s sum_<column> avg_<column> min_<column> max_<column> }",
		requestedRoot, detail, requestedRoot, baseName, countColumn)
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
