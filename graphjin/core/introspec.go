package core

import (
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

func (sg *SuperGraph) initGraphQLEgine() error {
	engine := graphql.New()
	engineSchema := engine.Schema
	dbSchema := sg.schema

	if err := engineSchema.Parse(`enum OrderDirection { asc desc }`); err != nil {
		return err
	}

	gqltype := func(col psql.DBColumn) schema.Type {
		typeName := typeMap[strings.ToLower(col.Type)]
		if typeName == "" {
			typeName = "String"
		}
		var t schema.Type = &schema.TypeName{Name: typeName}
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

	//validGraphQLIdentifierRegex := regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)

	scalarExpressionTypesNeeded := map[string]bool{}
	tableNames := dbSchema.GetTableNames()
	funcs := dbSchema.GetFunctions()

	for _, table := range tableNames {
		ti, err := dbSchema.GetTable(table)
		if err != nil {
			return err
		}

		if !ti.IsSingular {
			continue
		}

		singularName := ti.Singular
		// if !validGraphQLIdentifierRegex.MatchString(singularName) {
		// 	return errors.New("table name is not a valid GraphQL identifier: " + singularName)
		// }
		pluralName := ti.Plural
		// if !validGraphQLIdentifierRegex.MatchString(pluralName) {
		// 	return errors.New("table name is not a valid GraphQL identifier: " + pluralName)
		// }

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
					Name: "and",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: expressionTypeName}},
				},
				&schema.InputValue{
					Name: "or",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: expressionTypeName}},
				},
				&schema.InputValue{
					Name: "not",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: expressionTypeName}},
				},
			},
		}
		engineSchema.Types[expressionType.Name] = expressionType

		for _, col := range ti.Columns {
			colName := col.Name
			// if !validGraphQLIdentifierRegex.MatchString(colName) {
			// 	return errors.New("column name is not a valid GraphQL identifier: " + colName)
			// }

			colType := gqltype(col)
			nullableColType := ""
			if x, ok := colType.(*schema.NonNull); ok {
				nullableColType = x.OfType.(*schema.TypeName).Name
			} else {
				nullableColType = colType.(*schema.TypeName).Name
			}

			outputType.Fields = append(outputType.Fields, &schema.Field{
				Name: colName,
				Type: colType,
			})

			for _, f := range funcs {
				if col.Type != f.Params[0].Type {
					continue
				}
				outputType.Fields = append(outputType.Fields, &schema.Field{
					Name: f.Name + "_" + colName,
					Type: colType,
				})
			}

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
				Name: colName,
				Type: colType,
			})
			orderByType.Fields = append(orderByType.Fields, &schema.InputValue{
				Name: colName,
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "OrderDirection"}},
			})

			scalarExpressionTypesNeeded[nullableColType] = true

			expressionType.Fields = append(expressionType.Fields, &schema.InputValue{
				Name: colName,
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: nullableColType + "Expression"}},
			})
		}

		outputTypeName := &schema.TypeName{Name: outputType.Name}
		inputTypeName := &schema.TypeName{Name: inputType.Name}
		pluralOutputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: outputType.Name}}}}
		pluralInputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: inputType.Name}}}}

		args := schema.InputValueList{
			&schema.InputValue{
				Desc: schema.Description{Text: "To sort or ordering results just use the order_by argument. This can be combined with where, search, etc to build complex queries to fit you needs."},
				Name: "order_by",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: orderByType.Name}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "where",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: expressionType.Name}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "limit",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Int"}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "offset",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Int"}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "first",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Int"}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "last",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Int"}},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "before",
				Type: &schema.TypeName{Name: "String"},
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "after",
				Type: &schema.TypeName{Name: "String"},
			},
		}
		if ti.PrimaryCol != nil {
			t := gqltype(*ti.PrimaryCol)
			if _, ok := t.(*schema.NonNull); !ok {
				t = &schema.NonNull{OfType: t}
			}
			args = append(args, &schema.InputValue{
				Desc: schema.Description{Text: "Finds the record by the primary key"},
				Name: "id",
				Type: t,
			})
		}

		if ti.TSVCol != nil {
			args = append(args, &schema.InputValue{
				Desc: schema.Description{Text: "Performs full text search using a TSV index"},
				Name: "search",
				Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
			})
		}

		query.Fields = append(query.Fields, &schema.Field{
			Desc: schema.Description{Text: ""},
			Name: singularName,
			Type: outputTypeName,
			Args: args,
		})
		query.Fields = append(query.Fields, &schema.Field{
			Desc: schema.Description{Text: ""},
			Name: pluralName,
			Type: pluralOutputTypeName,
			Args: args,
		})

		mutationArgs := append(args, schema.InputValueList{
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "insert",
				Type: inputTypeName,
			},
			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "update",
				Type: inputTypeName,
			},

			&schema.InputValue{
				Desc: schema.Description{Text: ""},
				Name: "upsert",
				Type: inputTypeName,
			},
		}...)

		mutation.Fields = append(mutation.Fields, &schema.Field{
			Name: singularName,
			Args: mutationArgs,
			Type: outputType,
		})
		mutation.Fields = append(mutation.Fields, &schema.Field{
			Name: pluralName,
			Args: append(mutationArgs, schema.InputValueList{
				&schema.InputValue{
					Desc: schema.Description{Text: ""},
					Name: "inserts",
					Type: pluralInputTypeName,
				},
				&schema.InputValue{
					Desc: schema.Description{Text: ""},
					Name: "updates",
					Type: pluralInputTypeName,
				},
				&schema.InputValue{
					Desc: schema.Description{Text: ""},
					Name: "upserts",
					Type: pluralInputTypeName,
				},
			}...),
			Type: outputType,
		})
	}

	for typeName := range scalarExpressionTypesNeeded {
		expressionType := &schema.InputObject{
			Name: typeName + "Expression",
			Fields: schema.InputValueList{
				&schema.InputValue{
					Name: "eq",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "equals",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "neq",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "not_equals",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "gt",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "greater_than",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "lt",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "lesser_than",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "gte",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "greater_or_equals",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "lte",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "lesser_or_equals",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "in",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},
				&schema.InputValue{
					Name: "nin",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},
				&schema.InputValue{
					Name: "not_in",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},

				&schema.InputValue{
					Name: "like",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "nlike",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "not_like",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "ilike",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "nilike",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "not_ilike",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "similar",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "nsimilar",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "not_similar",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "has_key",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}},
				},
				&schema.InputValue{
					Name: "has_key_any",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},
				&schema.InputValue{
					Name: "has_key_all",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},
				&schema.InputValue{
					Name: "contains",
					Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: typeName}}}},
				},
				&schema.InputValue{
					Name: "contained_in",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "is_null",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Boolean"}},
				},
			},
		}
		engineSchema.Types[expressionType.Name] = expressionType
	}

	if err := engineSchema.ResolveTypes(); err != nil {
		return err
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

	sg.ge = engine
	return nil
}
