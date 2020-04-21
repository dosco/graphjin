package core

import (
	"errors"
	"regexp"
	"strings"

	"github.com/chirino/graphql"
	"github.com/chirino/graphql/resolvers"
	"github.com/chirino/graphql/schema"
	"github.com/dosco/super-graph/core/internal/psql"
)

var typeMap map[string]string = map[string]string{
	"smallint":         "Int",
	"integer":          "Int",
	"bigint":           "Int",
	"smallserial":      "Int",
	"serial":           "Int",
	"bigserial":        "Int",
	"decimal":          "Float",
	"numeric":          "Float",
	"real":             "Float",
	"double precision": "Float",
	"money":            "Float",
	"boolean":          "Boolean",
}

func (sg *SuperGraph) createGraphQLEgine() (*graphql.Engine, error) {
	engine := graphql.New()
	engineSchema := engine.Schema
	dbSchema := sg.schema

	engineSchema.Parse(`
enum OrderDirection {
  asc
  desc
}
`)

	gqltype := func(col psql.DBColumn) schema.Type {
		typeName := typeMap[strings.ToLower(col.Type)]
		if typeName == "" {
			typeName = "String"
		}
		var t schema.Type = &schema.TypeName{Ident: schema.Ident{Text: typeName}}
		if col.NotNull {
			t = &schema.NonNull{OfType: t}
		}
		return t
	}

	query := &schema.Object{
		Name:   "Query",
		Fields: schema.FieldList{},
	}
	mutation := &schema.Object{
		Name:   "Mutation",
		Fields: schema.FieldList{},
	}
	engineSchema.Types[query.Name] = query
	engineSchema.Types[mutation.Name] = mutation
	engineSchema.EntryPoints[schema.Query] = query
	engineSchema.EntryPoints[schema.Mutation] = mutation

	validGraphQLIdentifierRegex := regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)

	scalarExpressionTypesNeeded := map[string]bool{}
	tableNames := dbSchema.GetTableNames()
	for _, table := range tableNames {

		ti, err := dbSchema.GetTable(table)
		if err != nil {
			return nil, err
		}

		if !ti.IsSingular {
			continue
		}

		singularName := ti.Singular
		if !validGraphQLIdentifierRegex.MatchString(singularName) {
			return nil, errors.New("table name is not a valid GraphQL identifier: " + singularName)
		}
		pluralName := ti.Plural
		if !validGraphQLIdentifierRegex.MatchString(pluralName) {
			return nil, errors.New("table name is not a valid GraphQL identifier: " + pluralName)
		}

		outputType := &schema.Object{
			Name:   singularName + "Output",
			Fields: schema.FieldList{},
		}
		engineSchema.Types[outputType.Name] = outputType

		inputType := &schema.InputObject{
			Name:   singularName + "Input",
			Fields: schema.InputValueList{},
		}
		engineSchema.Types[inputType.Name] = inputType

		orderByType := &schema.InputObject{
			Name:   singularName + "OrderBy",
			Fields: schema.InputValueList{},
		}
		engineSchema.Types[orderByType.Name] = orderByType

		expressionTypeName := singularName + "Expression"
		expressionType := &schema.InputObject{
			Name: expressionTypeName,
			Fields: schema.InputValueList{
				&schema.InputValue{
					Name: schema.Ident{Text: "and"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: expressionTypeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "or"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: expressionTypeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: expressionTypeName}}},
				},
			},
		}
		engineSchema.Types[expressionType.Name] = expressionType

		for _, col := range ti.Columns {
			colName := col.Name
			if !validGraphQLIdentifierRegex.MatchString(colName) {
				return nil, errors.New("column name is not a valid GraphQL identifier: " + colName)
			}

			colType := gqltype(col)
			nullableColType := ""
			if x, ok := colType.(*schema.NonNull); ok {
				nullableColType = x.OfType.(*schema.TypeName).Ident.Text
			} else {
				nullableColType = colType.(*schema.TypeName).Ident.Text
			}

			outputType.Fields = append(outputType.Fields, &schema.Field{
				Name: colName,
				Type: colType,
			})

			// If it's a numeric type...
			if nullableColType == "Float" || nullableColType == "Int" {
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "avg_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "count_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "max_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "min_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "stddev_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "stddev_pop_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "stddev_samp_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "variance_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "var_pop_" + colName,
					Type: colType,
				})
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: "var_samp_" + colName,
					Type: colType,
				})
			}

			inputType.Fields = append(inputType.Fields, &schema.InputValue{
				Name: schema.Ident{Text: colName},
				Type: colType,
			})
			orderByType.Fields = append(orderByType.Fields, &schema.InputValue{
				Name: schema.Ident{Text: colName},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "OrderDirection"}}},
			})

			scalarExpressionTypesNeeded[nullableColType] = true

			expressionType.Fields = append(expressionType.Fields, &schema.InputValue{
				Name: schema.Ident{Text: colName},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: nullableColType + "Expression"}}},
			})
		}

		outputTypeName := &schema.TypeName{Ident: schema.Ident{Text: outputType.Name}}
		inputTypeName := &schema.TypeName{Ident: schema.Ident{Text: inputType.Name}}
		pluralOutputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: outputType.Name}}}}}
		pluralInputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: inputType.Name}}}}}

		args := schema.InputValueList{
			&schema.InputValue{
				Desc: &schema.Description{Text: "To sort or ordering results just use the order_by argument. This can be combined with where, search, etc to build complex queries to fit you needs."},
				Name: schema.Ident{Text: "order_by"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: orderByType.Name}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "where"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: expressionType.Name}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "limit"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "Int"}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "offset"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "Int"}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "first"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "Int"}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "last"},
				Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "Int"}}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "before"},
				Type: &schema.TypeName{Ident: schema.Ident{Text: "String"}},
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "after"},
				Type: &schema.TypeName{Ident: schema.Ident{Text: "String"}},
			},
		}
		if ti.PrimaryCol != nil {
			t := gqltype(*ti.PrimaryCol)
			if _, ok := t.(*schema.NonNull); !ok {
				t = &schema.NonNull{OfType: t}
			}
			args = append(args, &schema.InputValue{
				Desc: &schema.Description{Text: "Finds the record by the primary key"},
				Name: schema.Ident{Text: "id"},
				Type: t,
			})
		}

		if ti.TSVCol != nil {
			args = append(args, &schema.InputValue{
				Desc: &schema.Description{Text: "Performs full text search using a TSV index"},
				Name: schema.Ident{Text: "search"},
				Type: &schema.TypeName{Ident: schema.Ident{Text: "String!"}},
			})
		}

		query.Fields = append(query.Fields, &schema.Field{
			Desc: &schema.Description{Text: ""},
			Name: singularName,
			Type: outputTypeName,
			Args: args,
		})
		query.Fields = append(query.Fields, &schema.Field{
			Desc: &schema.Description{Text: ""},
			Name: pluralName,
			Type: pluralOutputTypeName,
			Args: args,
		})

		mutationArgs := schema.InputValueList{
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "insert"},
				Type: inputTypeName,
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "inserts"},
				Type: pluralInputTypeName,
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "update"},
				Type: inputTypeName,
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "updates"},
				Type: pluralInputTypeName,
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "upsert"},
				Type: inputTypeName,
			},
			&schema.InputValue{
				Desc: &schema.Description{Text: ""},
				Name: schema.Ident{Text: "upserts"},
				Type: pluralInputTypeName,
			},
		}
		mutation.Fields = append(mutation.Fields, &schema.Field{
			Name: singularName,
			Args: append(args, mutationArgs...),
			Type: outputType,
		})
		mutation.Fields = append(mutation.Fields, &schema.Field{
			Name: pluralName,
			Args: append(args, mutationArgs...),
			Type: outputType,
		})

	}

	for typeName, _ := range scalarExpressionTypesNeeded {
		expressionType := &schema.InputObject{
			Name: typeName + "Expression",
			Fields: schema.InputValueList{
				&schema.InputValue{
					Name: schema.Ident{Text: "eq"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "equals"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "neq"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not_equals"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "gt"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "greater_than"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "lt"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "lesser_than"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "gte"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "greater_or_equals"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "lte"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "lesser_or_equals"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "in"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "nin"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not_in"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},

				&schema.InputValue{
					Name: schema.Ident{Text: "like"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "nlike"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not_like"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "ilike"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "nilike"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not_ilike"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "similar"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "nsimilar"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "not_similar"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "has_key"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "has_key_any"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "has_key_all"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "contains"},
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: typeName}}}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "contained_in"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "String"}}},
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "is_null"},
					Type: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: "Boolean"}}},
				},
			},
		}
		engineSchema.Types[expressionType.Name] = expressionType
	}

	err := engineSchema.ResolveTypes()
	if err != nil {
		return nil, err
	}

	engine.Resolver = resolvers.Func(func(request *resolvers.ResolveRequest, next resolvers.Resolution) resolvers.Resolution {
		resolver := resolvers.MetadataResolver.Resolve(request, next)
		if resolver != nil {
			return resolver
		}
		resolver = resolvers.MethodResolver.Resolve(request, next) // needed by the MetadataResolver
		if resolver != nil {
			return resolver
		}

		return nil
	})
	return engine, nil
}
