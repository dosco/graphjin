package core

import (
	"fmt"
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

type expInfo struct {
	name, vtype string
	list        bool
	desc, db    string
}

const (
	likeDesc = "Value matching pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values having 'r' in second position"

	notLikeDesc = "Value not matching pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values not having 'r' in second position"

	iLikeDesc = "Value matching (case-insensitive) pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values having 'r' in second position"

	notILikeDesc = "Value not matching (case-insensitive) pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values not having 'r' in second position"

	similarDesc = "Value matching regex pattern. Similar to the 'like' operator but with support for regex. Pattern must match entire value."

	notSimilarDesc = "Value not matching regex pattern. Similar to the 'like' operator but with support for regex. Pattern must not match entire value."
)

var expList []expInfo = []expInfo{
	{"eq", "", false, "Equals value", ""},
	{"equals", "", false, "Equals value", ""},
	{"neq", "", false, "Does not equal value", ""},
	{"not_equals", "", false, "Does not equal value", ""},
	{"gt", "", false, "Is greater than value", ""},
	{"greater_than", "", false, "Is greater than value", ""},
	{"lt", "", false, "Is lesser than value", ""},
	{"lesser_than", "", false, "Is lesser than value", ""},
	{"gte", "", false, "Is greater than or equals value", ""},
	{"greater_or_equals", "", false, "Is greater than or equals value", ""},
	{"lte", "", false, "Is lesser than or equals value", ""},
	{"lesser_or_equals", "", false, "Is lesser than or equals value", ""},
	{"in", "", true, "Is in list of values", ""},
	{"nin", "", true, "Is not in list of values", ""},
	{"not_in", "", true, "Is not in list of values", ""},
	{"like", "String", false, likeDesc, ""},
	{"nlike", "String", false, notLikeDesc, ""},
	{"not_like", "String", false, notLikeDesc, ""},
	{"ilike", "String", false, iLikeDesc, ""},
	{"nilike", "String", false, notILikeDesc, ""},
	{"not_ilike", "String", false, notILikeDesc, ""},
	{"similar", "String", false, similarDesc, ""},
	{"nsimilar", "String", false, notSimilarDesc, ""},
	{"not_similar", "String", false, notSimilarDesc, ""},
	{"regex", "String", false, "Value matches regex pattern", ""},
	{"nregex", "String", false, "Value not matching regex pattern", ""},
	{"not_regex", "String", false, "Value not matching regex pattern", ""},
	{"iregex", "String", false, "Value matches (case-insensitive) regex pattern", ""},
	{"niregex", "String", false, "Value not matching (case-insensitive) regex pattern", ""},
	{"not_iregex", "String", false, "Value not matching (case-insensitive) regex pattern", ""},
	{"has_key", "", false, "JSON value contains this key", ""},
	{"has_key_any", "", true, "JSON value contains any of these keys", ""},
	{"has_key_all", "", true, "JSON value contains all of these keys", ""},
	{"contains", "", false, "JSON value matches any of they key/value pairs", ""},
	{"contained_in", "", false, "JSON value contains all of they key/value pairs", ""},
	{"is_null", "Boolean", false, "Is value null (true) or not null (false)", ""},
}

type funcInfo struct {
	name, desc, db string
}

var funcCount = funcInfo{"count", "Count the number of rows", ""}

var funcListNum []funcInfo = []funcInfo{
	{"avg", "Calculate an average %s", ""},
	{"max", "Find the maximum %s", ""},
	{"min", "Find the minimum %s", ""},
	{"stddev", "Calculate the standard deviation of %s values", ""},
	{"stddev_pop", "Calculate the population standard deviation of %s values", ""},
	{"stddev_samp", "Calculate the sample standard deviation of %s values", ""},
	{"variance", "Calculate the sample variance of %s values", ""},
	{"var_samp", "Calculate the sample variance of %s values", ""},
	{"var_pop", "Calculate the population sample variance of %s values", ""},
}

var funcListString []funcInfo = []funcInfo{
	{"length", "Calculate the length of %s", ""},
	{"lower", "Convert %s to lowercase", ""},
	{"upper", "Convert %s to uppercase", ""},
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
	if gj.prod {
		return nil
	}

	engine := graphql.New()
	in := &intro{
		Schema:       engine.Schema,
		DBSchema:     gj.schema,
		query:        &schema.Object{Name: "Query", Fields: schema.FieldList{}},
		mutation:     &schema.Object{Name: "Mutation", Fields: schema.FieldList{}},
		subscription: &schema.Object{Name: "Subscribe", Fields: schema.FieldList{}},
		exptNeeded:   map[string]bool{},
	}

	in.Types[in.query.Name] = in.query
	in.Types[in.mutation.Name] = in.mutation
	in.Types[in.subscription.Name] = in.subscription

	in.EntryPoints[schema.Query] = in.query
	in.EntryPoints[schema.Mutation] = in.mutation
	in.EntryPoints[schema.Subscription] = in.subscription

	in.Types["OrderDirection"] = &schema.Enum{Name: "OrderDirection", Values: []*schema.EnumValue{
		{
			Name: "asc",
			Desc: schema.NewDescription("Ascending"),
		}, {
			Name: "desc",
			Desc: schema.NewDescription("Descending"),
		},
	}}

	in.Types["Cursor"] = &schema.Scalar{
		Name: "Cursor",
		Desc: schema.NewDescription("A cursor is an encoded string use for pagination"),
	}

	if err := in.addTables(); err != nil {
		return err
	}
	in.addExpressions()
	in.addDirectives()

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
		if err := in.addTable(t.Name, t, false); err != nil {
			return err
		}

		if err := in.addTable(t.Name, t, true); err != nil {
			return err
		}
	}

	for name, t := range in.GetAliases() {
		if err := in.addTable(name, t, false); err != nil {
			return err
		}
	}

	return nil
}

// func (in *intro) addToTable(name, desc string, ti sdata.DBTable) {
// 	k := name + "Output"
// 	var ot *schema.Object = in.Types[k].(*schema.Object)

// 	ot.Fields = append(ot.Fields, &schema.Field{
// 		Name: ti.Name,
// 		Type: &schema.TypeName{Name: ti.Name + "Output"},
// 		Desc: schema.NewDescription(desc),
// 	})
// }

func (in *intro) addTable(name string, ti sdata.DBTable, singular bool) error {
	if ti.Blocked {
		return nil
	}

	if len(ti.Columns) == 0 {
		return nil
	}

	if singular {
		name = name + in.SingularSuffix.Value
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
		in.addColumn(name, ti, col, it, obt, expt, ot, singular)
	}

	relTables1, err := in.GetFirstDegree(ti.Schema, ti.Name)
	if err != nil {
		return err
	}

	for k, t := range relTables1 {
		ot.Fields = append(ot.Fields, &schema.Field{
			Name: k,
			Type: &schema.TypeName{Name: t.Name + "Output"},
		})
	}

	relTables2, err := in.GetSecondDegree(ti.Schema, ti.Name)
	if err != nil {
		return err
	}

	for k, t := range relTables2 {
		ot.Fields = append(ot.Fields, &schema.Field{
			Name: k,
			Type: &schema.TypeName{Name: t.Name + "Output"},
		})
	}

	return nil
}

func (in *intro) addDirectives() {
	in.DeclaredDirectives["object"] = &schema.DirectiveDecl{
		Name: "object",
		Desc: schema.NewDescription("Directs the executor to change the return type from a list to a object. All but the first entry of the list will be truncated"),
		Locs: []string{"FIELD"},
	}

	in.DeclaredDirectives["through"] = &schema.DirectiveDecl{
		Name: "through",
		Desc: schema.NewDescription("Directs the executor to use the specified table as a join-table to connect this field and it's parent"),
		Locs: []string{"FIELD"},
		Args: schema.InputValueList{
			{
				Name: "table",
				Desc: schema.NewDescription("Table name"),
				Type: &schema.TypeName{Name: "String"},
			},
		},
	}
}

func (in *intro) addColumn(
	name string,
	ti sdata.DBTable, col sdata.DBColumn,
	it, obt, expt *schema.InputObject, ot *schema.Object, singular bool) {

	colName := col.Name
	if col.Blocked {
		return
	}

	colType, typeName := getGQLType(col)

	ot.Fields = append(ot.Fields, &schema.Field{
		Name: colName,
		Type: colType,
	})

	if col.PrimaryKey {
		ot.Fields = append(ot.Fields, &schema.Field{
			Name: funcCount.name + "_" + colName,
			Type: colType,
			Desc: schema.NewDescription(funcCount.desc),
		})
	}

	// No functions on foreign key columns
	if col.FKeyCol == "" {
		// If it's a numeric type...
		if typeName == "Float" || typeName == "Int" {
			for _, v := range funcListNum {
				desc := fmt.Sprintf(v.desc, colName)
				ot.Fields = append(ot.Fields, &schema.Field{
					Name: v.name + "_" + colName,
					Type: colType,
					Desc: schema.NewDescription(desc),
				})
			}
		}

		if typeName == "String" {
			for _, v := range funcListString {
				desc := fmt.Sprintf(v.desc, colName)
				ot.Fields = append(ot.Fields, &schema.Field{
					Name: v.name + "_" + colName,
					Type: colType,
					Desc: schema.NewDescription(desc),
				})
			}
		}

		for _, f := range in.GetFunctions() {
			if col.Type != f.Params[0].Type {
				continue
			}

			ot.Fields = append(ot.Fields, &schema.Field{
				Name: f.Name + "_" + colName,
				Type: colType,
			})
		}
	}

	in.addArgs(name, ti, col, it, obt, expt, ot, singular)

	it.Fields = append(it.Fields, &schema.InputValue{
		Name: colName,
		Type: colType,
	})
	obt.Fields = append(obt.Fields, &schema.InputValue{
		Name: colName,
		Type: &schema.TypeName{Name: "OrderDirection"},
	})

	in.exptNeeded[typeName] = true

	expt.Fields = append(expt.Fields, &schema.InputValue{
		Name: colName,
		Type: &schema.TypeName{Name: typeName + "Expression"},
	})
}

func (in *intro) addArgs(
	name string,
	ti sdata.DBTable, col sdata.DBColumn,
	it, obt, expt *schema.InputObject, ot *schema.Object, singular bool) {

	otName := &schema.TypeName{Name: ot.Name}
	itName := &schema.TypeName{Name: it.Name}

	potName := &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: ot.Name}}}
	pitName := &schema.List{OfType: &schema.NonNull{OfType: &schema.TypeName{Name: it.Name}}}

	var args schema.InputValueList

	if !singular {
		args = schema.InputValueList{
			&schema.InputValue{
				Desc: schema.NewDescription("Sort or order results. Use key 'asc' for ascending and 'desc' for descending"),
				Name: "order_by",
				Type: &schema.TypeName{Name: obt.Name},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Filter results based on column values or values of columns in related tables"),
				Name: "where",
				Type: &schema.TypeName{Name: expt.Name},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Limit the number of returned rows"),
				Name: "limit",
				Type: &schema.TypeName{Name: "Int"},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Offset the number of returned rows (Not efficient for pagination, please use a cursor for that)"),
				Name: "offset",
				Type: &schema.TypeName{Name: "Int"},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Number of rows to return from the top. Combine with 'after' or 'before' arguments for cursor pagination"),
				Name: "first",
				Type: &schema.TypeName{Name: "Int"},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Number of rows to return from the bottom. Combine with 'after' or 'before' arguments for cursor pagination"),
				Name: "last",
				Type: &schema.TypeName{Name: "Int"},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Pass the cursor to this argument for backward pagination"),
				Name: "before",
				Type: &schema.TypeName{Name: "Cursor"},
			},
			&schema.InputValue{
				Desc: schema.NewDescription("Pass the cursor to this argument for forward pagination"),
				Name: "after",
				Type: &schema.TypeName{Name: "Cursor"},
			},
		}

		if len(ti.FullText) == 0 {
			args = append(args, &schema.InputValue{
				Desc: schema.NewDescription("Performs a full text search"),
				Name: "search",
				Type: &schema.TypeName{Name: "String"},
			})
		}
	}

	if ti.PrimaryCol.Name != "" && singular {
		colType, _ := getGQLType(ti.PrimaryCol)
		args = append(args, &schema.InputValue{
			Desc: schema.NewDescription("Finds the record by the primary key"),
			Name: "id",
			Type: colType,
		})
	}

	if singular {
		in.query.Fields = append(in.query.Fields, &schema.Field{
			//Desc: schema.NewDescription(""),
			Name: name,
			Type: otName,
			Args: args,
		})

		in.subscription.Fields = append(in.subscription.Fields, &schema.Field{
			//Desc: schema.NewDescription(""),
			Name: name,
			Type: otName,
			Args: args,
		})
	} else {
		in.query.Fields = append(in.query.Fields, &schema.Field{
			//Desc: schema.NewDescription(""),
			Name: name,
			Type: potName,
			Args: args,
		})

		in.subscription.Fields = append(in.subscription.Fields, &schema.Field{
			//Desc: schema.NewDescription(""),
			Name: name,
			Type: potName,
			Args: args,
		})
	}

	mutationArgs := append(args, schema.InputValueList{
		&schema.InputValue{
			Desc: schema.NewDescription(fmt.Sprintf("Insert row into table %s", name)),
			Name: "insert",
			Type: pitName,
		},
		&schema.InputValue{
			Desc: schema.NewDescription(fmt.Sprintf("Update row in table %s", name)),
			Name: "update",
			Type: itName,
		},
		&schema.InputValue{
			Desc: schema.NewDescription(fmt.Sprintf("Update or Insert row in table %s", name)),
			Name: "upsert",
			Type: itName,
		},
		&schema.InputValue{
			Desc: schema.NewDescription(fmt.Sprintf("Delete row from table %s", name)),
			Name: "delete",
			Type: &schema.NonNull{OfType: &schema.TypeName{Name: "Boolean"}},
		},
	}...)
	in.mutation.Fields = append(in.mutation.Fields, &schema.Field{
		Name: name,
		Args: mutationArgs,
		Type: potName,
	})
}

func (in *intro) addExpressions() {
	// scalarExpressionTypesNeeded
	for typeName := range in.exptNeeded {
		var fields schema.InputValueList

		for _, v := range expList {
			vtype := v.vtype
			if v.vtype == "" {
				vtype = typeName
			}
			iv := &schema.InputValue{
				Name: v.name,
				Desc: schema.NewDescription(v.desc),
				Type: &schema.TypeName{Name: vtype},
			}
			if v.list {
				iv.Type = &schema.List{OfType: iv.Type}
			}
			fields = append(fields, iv)
		}

		ext := &schema.InputObject{
			Name:   typeName + "Expression",
			Fields: fields,
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
	} else if typeName, ok = typeMap[k]; !ok {
		typeName = "String"
	}

	var t schema.Type = &schema.TypeName{Name: typeName}
	if col.Array {
		t = &schema.List{OfType: t}
	}
	// if col.NotNull {
	// 	t = &schema.NonNull{OfType: t}
	// }
	return t, typeName
}
