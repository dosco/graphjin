package core

import (
	"strings"

	"github.com/chirino/graphql"
	"github.com/chirino/graphql/resolvers"
	"github.com/chirino/graphql/schema"
	"github.com/dosco/graphjin/core/internal/sdata"
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

type intro struct {
	*schema.Schema
	*sdata.DBSchema
	query        *schema.Object
	mutation     *schema.Object
	subscription *schema.Object
	exptNeeded   map[string]bool
}

func (gj *GraphJin) initGraphQLEgine() error {
	engine := graphql.New()
	in := &intro{
		Schema:       engine.Schema,
		DBSchema:     gj.schema,
		query:        &schema.Object{Name: "Query", Fields: schema.FieldList{}},
		mutation:     &schema.Object{Name: "Mutation", Fields: schema.FieldList{}},
		subscription: &schema.Object{Name: "Subscribe", Fields: schema.FieldList{}},
		exptNeeded:   map[string]bool{},
	}

	if err := in.Parse(`enum OrderDirection { asc desc }`); err != nil {
		return err
	}

	in.Types[in.query.Name] = in.query
	in.Types[in.mutation.Name] = in.mutation
	in.Types[in.subscription.Name] = in.subscription

	in.EntryPoints[schema.Query] = in.query
	in.EntryPoints[schema.Mutation] = in.mutation
	in.EntryPoints[schema.Subscription] = in.subscription

	if err := in.addTables(); err != nil {
		return err
	}
	in.addExpressions()

	if err := in.ResolveTypes(); err != nil {
		return err
	}

	engine.Resolver = resolvers.Func(revolverFunc)
	gj.ge = engine
	return nil
}

func revolverFunc(request *resolvers.ResolveRequest, next resolvers.Resolution) resolvers.Resolution {
	resolver := resolvers.MetadataResolver.Resolve(request, next)
	if resolver != nil {
		return resolver
	}
	resolver = resolvers.MethodResolver.Resolve(request, next) // needed by the MetadataResolver
	if resolver != nil {
		return resolver
	}

	return nil
}

func (in *intro) addTables() error {
	for _, t := range in.GetTables() {
		if err := in.addTable(t.Name, t); err != nil {
			return err
		}
	}

	for name, t := range in.GetAliases() {
		if err := in.addTable(name, t); err != nil {
			return err
		}
	}
	return nil
}

func (in *intro) addTable(name string, ti sdata.DBTable) error {
	if ti.Blocked {
		return nil
	}

	// outputType
	ot := &schema.Object{
		Name: name + "Output", Fields: schema.FieldList{},
	}
	in.Types[ot.Name] = ot

	// inputType
	it := &schema.InputObject{
		Name: name + "Input", Fields: schema.InputValueList{},
	}
	in.Types[it.Name] = it

	// orderByType
	obt := &schema.InputObject{
		Name: name + "OrderBy", Fields: schema.InputValueList{},
	}
	in.Types[obt.Name] = obt

	ot.Fields = append(ot.Fields, &schema.Field{
		Name: name,
		Type: &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: name + "Output"}}}},
	})

	// expressionType
	exptName := name + "Expression"
	expt := &schema.InputObject{
		Name: exptName,
		Fields: schema.InputValueList{
			&schema.InputValue{
				Name: "and",
				Type: &schema.TypeName{Name: exptName},
			},
			&schema.InputValue{
				Name: "or",
				Type: &schema.TypeName{Name: exptName},
			},
			&schema.InputValue{
				Name: "not",
				Type: &schema.TypeName{Name: exptName},
			},
		},
	}
	in.Types[expt.Name] = expt

	for _, col := range ti.Columns {
		in.addColumn(name, ti, col, it, obt, expt, ot)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			name := getRelName(col.Name)

			ti, err := in.Find(col.FKeySchema, col.FKeyTable)
			if err != nil {
				return err
			}
			if ti.Blocked {
				continue
			}
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: name,
				Type: &schema.TypeName{Name: ti.Name + "Output"},
			})
		}
	}

	return nil
}

func (in *intro) addColumn(
	name string,
	ti sdata.DBTable, col sdata.DBColumn,
	it, obt, expt *schema.InputObject, ot *schema.Object) {

	colName := col.Name
	if col.Blocked {
		return
	}

	colType, typeName := getGQLType(col)

	ot.Fields = append(ot.Fields, &schema.Field{
		Name: colName,
		Type: colType,
	})

	for _, f := range in.GetFunctions() {
		if col.Type != f.Params[0].Type {
			continue
		}

		ot.Fields = append(ot.Fields, &schema.Field{
			Name: f.Name + "_" + colName,
			Type: colType,
		})

		// If it's a numeric type...
		if typeName == "Float" || typeName == "Int" {
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "avg_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "count_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "max_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "min_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "stddev_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "stddev_pop_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "stddev_samp_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "variance_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "var_pop_" + colName,
				Type: colType,
			})
			ot.Fields = append(ot.Fields, &schema.Field{
				Name: "var_samp_" + colName,
				Type: colType,
			})
		}
	}

	in.addArgs(name, ti, col, it, obt, expt, ot)

	it.Fields = append(it.Fields, &schema.InputValue{
		Name: colName,
		Type: colType,
	})
	obt.Fields = append(obt.Fields, &schema.InputValue{
		Name: colName,
		Type: &schema.NonNull{OfType: &schema.TypeName{Name: "OrderDirection"}},
	})

	in.exptNeeded[typeName] = true

	expt.Fields = append(expt.Fields, &schema.InputValue{
		Name: colName,
		Type: &schema.NonNull{OfType: &schema.TypeName{Name: typeName + "Expression"}},
	})
}

func (in *intro) addArgs(
	name string,
	ti sdata.DBTable, col sdata.DBColumn,
	it, obt, expt *schema.InputObject, ot *schema.Object) {

	otName := &schema.TypeName{Name: ot.Name}
	itName := &schema.TypeName{Name: it.Name}

	potName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: ot.Name}}}}
	pitName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: it.Name}}}}

	args := schema.InputValueList{
		&schema.InputValue{
			Desc: schema.Description{Text: "To sort or ordering results just use the order_by argument. This can be combined with where, search, etc to build complex queries to fit your needs."},
			Name: "order_by",
			Type: &schema.TypeName{Name: obt.Name},
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "where",
			Type: &schema.TypeName{Name: expt.Name},
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "limit",
			Type: &schema.TypeName{Name: "Int"},
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "offset",
			Type: &schema.TypeName{Name: "Int"},
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "first",
			Type: &schema.TypeName{Name: "Int"},
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "last",
			Type: &schema.TypeName{Name: "Int"},
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

	if ti.PrimaryCol.Name != "" {
		colType, _ := getGQLType(col)
		args = append(args, &schema.InputValue{
			Desc: schema.Description{Text: "Finds the record by the primary key"},
			Name: "id",
			Type: colType,
		})
	}

	if len(ti.FullText) == 0 {
		args = append(args, &schema.InputValue{
			Desc: schema.Description{Text: "Performs a full text search"},
			Name: "search",
			Type: &schema.TypeName{Name: "String"},
		})
	}

	in.query.Fields = append(in.query.Fields, &schema.Field{
		Desc: schema.Description{Text: ""},
		Name: name,
		Type: otName,
		Args: args,
	})
	in.query.Fields = append(in.query.Fields, &schema.Field{
		Desc: schema.Description{Text: ""},
		Name: name,
		Type: potName,
		Args: args,
	})

	in.subscription.Fields = append(in.subscription.Fields, &schema.Field{
		Desc: schema.Description{Text: ""},
		Name: name,
		Type: otName,
		Args: args,
	})
	in.subscription.Fields = append(in.subscription.Fields, &schema.Field{
		Desc: schema.Description{Text: ""},
		Name: name,
		Type: potName,
		Args: args,
	})

	mutationArgs := append(args, schema.InputValueList{
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "insert",
			Type: pitName,
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "update",
			Type: itName,
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "upsert",
			Type: itName,
		},
		&schema.InputValue{
			Desc: schema.Description{Text: ""},
			Name: "delete",
			Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Boolean"}},
		},
	}...)
	in.mutation.Fields = append(in.mutation.Fields, &schema.Field{
		Name: name,
		Args: mutationArgs,
		Type: ot,
	})
}

func (in *intro) addExpressions() {
	// scalarExpressionTypesNeeded
	for typeName := range in.exptNeeded {
		ext := &schema.InputObject{
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
					Name: "regex",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "nregex",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "not_regex",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "iregex",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "niregex",
					Type: &schema.NonNull{OfType: &schema.TypeName{Name: "String"}},
				},
				&schema.InputValue{
					Name: "not_iregex",
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
		in.Types[ext.Name] = ext
	}
}

func getGQLType(col sdata.DBColumn) (schema.Type, string) {
	var typeName string
	var ok bool

	k := strings.ToLower(col.Type)
	if i := strings.IndexAny(k, "(["); i != -1 {
		k = k[:i]
	}

	if col.PrimaryKey {
		typeName = "ID"
	} else if typeName = typeMap[k]; !ok {
		typeName = "String"
	}

	var t schema.Type = &schema.TypeName{Name: typeName}
	if col.Array {
		t = &schema.List{OfType: t}
	}
	if col.NotNull {
		t = &schema.NonNull{OfType: t}
	}
	return t, typeName
}

func getRelName(colName string) string {
	cn := strings.ToLower(colName)

	if strings.HasSuffix(cn, "_id") {
		return colName[:len(colName)-3]
	}

	if strings.HasSuffix(cn, "_ids") {
		return colName[:len(colName)-4]
	}

	if strings.HasPrefix(cn, "id_") {
		return colName[3:]
	}

	if strings.HasPrefix(cn, "ids_") {
		return colName[4:]
	}

	return ""
}
