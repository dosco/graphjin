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
	sg.Engine = graphql.New()
	engineSchema := sg.Engine.Schema
	dbSchema := sg.schema

	sanitizeForGraphQLSchema := func(value string) string {
		// TODO:
		return value
	}

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

	tableNames := dbSchema.GetTableNames()
	for _, table := range tableNames {

		ti, err := dbSchema.GetTable(table)
		if err != nil {
			return err
		}

		if !ti.IsSingular {
			continue
		}

		pti, err := dbSchema.GetTable(ti.Plural)
		if err != nil {
			return err
		}

		outputType := &schema.Object{
			Name:   sanitizeForGraphQLSchema(ti.Singular) + "Output",
			Fields: schema.FieldList{},
		}

		inputType := &schema.InputObject{
			Name:   sanitizeForGraphQLSchema(ti.Singular) + "Input",
			Fields: schema.InputValueList{},
		}

		for _, col := range ti.Columns {
			outputType.Fields = append(outputType.Fields, &schema.Field{
				Name: sanitizeForGraphQLSchema(col.Name),
				Type: gqltype(col),
			})
			inputType.Fields = append(inputType.Fields, &schema.InputValue{
				Name: schema.Ident{Text: sanitizeForGraphQLSchema(col.Name)},
				Type: gqltype(col),
			})
		}

		engineSchema.Types[outputType.Name] = outputType
		engineSchema.Types[inputType.Name] = inputType

		outputTypeName := &schema.TypeName{Ident: schema.Ident{Text: outputType.Name}}
		inputTypeName := &schema.TypeName{Ident: schema.Ident{Text: inputType.Name}}
		pluralOutputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: outputType.Name}}}}}
		pluralInputTypeName := &schema.NonNull{OfType: &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Ident: schema.Ident{Text: inputType.Name}}}}}

		args := schema.InputValueList{}
		if ti.PrimaryCol != nil {
			t := gqltype(*ti.PrimaryCol)
			if _, ok := t.(*schema.NonNull); !ok {
				t = &schema.NonNull{OfType: t}
			}
			arg := &schema.InputValue{
				Name: schema.Ident{Text: "id"},
				Type: t,
			}
			args = append(args, arg)
		}

		query.Fields = append(query.Fields, &schema.Field{
			Name: sanitizeForGraphQLSchema(ti.Singular),
			Type: outputTypeName,
			Args: args,
		})
		query.Fields = append(query.Fields, &schema.Field{
			Name: sanitizeForGraphQLSchema(pti.Plural),
			Type: pluralOutputTypeName,
			Args: args,
		})

		mutation.Fields = append(mutation.Fields, &schema.Field{
			Name: sanitizeForGraphQLSchema(ti.Singular),
			Args: append(args, schema.InputValueList{
				&schema.InputValue{
					Name: schema.Ident{Text: "insert"},
					Type: inputTypeName,
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "inserts"},
					Type: pluralInputTypeName,
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "update"},
					Type: inputTypeName,
				},
				&schema.InputValue{
					Name: schema.Ident{Text: "updates"},
					Type: pluralInputTypeName,
				},
			}...),
			Type: outputType,
		})

	}
	err := engineSchema.ResolveTypes()
	if err != nil {
		return err
	}

	sg.Engine.Resolver = resolvers.Func(func(request *resolvers.ResolveRequest, next resolvers.Resolution) resolvers.Resolution {
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
	return nil
}
