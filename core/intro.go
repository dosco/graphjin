package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
	"github.com/dosco/graphjin/core/v3/internal/valid"
)

const (
	KIND_SCALAR      = "SCALAR"
	KIND_OBJECT      = "OBJECT"
	KIND_NONNULL     = "NON_NULL"
	KIND_LIST        = "LIST"
	KIND_UNION       = "UNION"
	KIND_ENUM        = "ENUM"
	KIND_INPUT_OBJ   = "INPUT_OBJECT"
	LOC_QUERY        = "QUERY"
	LOC_MUTATION     = "MUTATION"
	LOC_SUBSCRIPTION = "SUBSCRIPTION"
	LOC_FIELD        = "FIELD"

	SUFFIX_EXP      = "Expression"
	SUFFIX_LISTEXP  = "ListExpression"
	SUFFIX_INPUT    = "Input"
	SUFFIX_ORDER_BY = "OrderByInput"
	SUFFIX_WHERE    = "WhereInput"
	SUFFIX_ARGS     = "ArgsInput"
	SUFFIX_ENUM     = "Enum"
)

var (
	TYPE_STRING  = "String"
	TYPE_INT     = "Int"
	TYPE_BOOLEAN = "Boolean"
	TYPE_FLOAT   = "Float"
	TYPE_JSON    = "JSON"
)

type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name"`
	OfType *TypeRef `json:"ofType"`
}

type InputValue struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Type         *TypeRef `json:"type"`
	DefaultValue *string  `json:"defaultValue"`
}

type FieldObject struct {
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	Args              []InputValue `json:"args"`
	Type              *TypeRef     `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason"`
}

type EnumValue struct {
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason"`
}

type FullType struct {
	Kind          string        `json:"kind"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Fields        []FieldObject `json:"fields"`
	InputFields   []InputValue  `json:"inputFields"`
	EnumValues    []EnumValue   `json:"enumValues"`
	Interfaces    []TypeRef     `json:"interfaces"`
	PossibleTypes []TypeRef     `json:"possibleTypes"`
}

type ShortFullType struct {
	Name string `json:"name"`
}

type DirectiveType struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Locations    []string     `json:"locations"`
	Args         []InputValue `json:"args"`
	IsRepeatable bool         `json:"isRepeatable"`
}

type IntrospectionSchema struct {
	Types            []FullType      `json:"types"`
	QueryType        *ShortFullType  `json:"queryType"`
	MutationType     *ShortFullType  `json:"mutationType"`
	SubscriptionType *ShortFullType  `json:"subscriptionType"`
	Directives       []DirectiveType `json:"directives"`
}

type IntroResult struct {
	Schema IntrospectionSchema `json:"__schema"`
}

// const singularSuffix = "ByID"

var stdTypes = []FullType{
	{
		Kind:        KIND_SCALAR,
		Name:        TYPE_BOOLEAN,
		Description: "The `Boolean` scalar type represents `true` or `false`",
	}, {
		Kind:        KIND_SCALAR,
		Name:        TYPE_FLOAT,
		Description: "The `Float` scalar type represents signed double-precision fractional values as specified by\n[IEEE 754](http://en.wikipedia.org/wiki/IEEE_floating_point).",
	}, {
		Kind:        KIND_SCALAR,
		Name:        TYPE_INT,
		Description: "The `Int` scalar type represents non-fractional signed whole numeric values. Int can represent\nvalues between -(2^31) and 2^31 - 1.\n",
		// Add Int expression after
	}, {
		Kind:        KIND_SCALAR,
		Name:        TYPE_STRING,
		Description: "The `String` scalar type represents textual data, represented as UTF-8 character sequences.\nThe String type is most often used by GraphQL to represent free-form human-readable text.\n",
	}, {
		Kind:        KIND_SCALAR,
		Name:        TYPE_JSON,
		Description: "The `JSON` scalar type represents json data",
	}, {
		Kind:       KIND_OBJECT,
		Name:       "Query",
		Interfaces: []TypeRef{},
		Fields:     []FieldObject{},
	}, {
		Kind:       KIND_OBJECT,
		Name:       "Subscription",
		Interfaces: []TypeRef{},
		Fields:     []FieldObject{},
	}, {
		Kind:       KIND_OBJECT,
		Name:       "Mutation",
		Interfaces: []TypeRef{},
		Fields:     []FieldObject{},
	}, {
		Kind: KIND_ENUM,
		Name: "FindSearchInput",
		EnumValues: []EnumValue{{
			Name:        "children",
			Description: "Children of parent row",
		}, {
			Name:        "parents",
			Description: "Parents of current row",
		}},
	}, {
		Kind:        "ENUM",
		Name:        "OrderDirection",
		Description: "Result ordering types",
		EnumValues: []EnumValue{{
			Name:        "asc",
			Description: "Ascending order",
		}, {
			Name:        "desc",
			Description: "Descending order",
		}, {
			Name:        "asc_nulls_first",
			Description: "Ascending nulls first order",
		}, {
			Name:        "desc_nulls_first",
			Description: "Descending nulls first order",
		}, {
			Name:        "asc_nulls_last",
			Description: "Ascending nulls last order",
		}, {
			Name:        "desc_nulls_last",
			Description: "Descending nulls last order",
		}},
	}, {
		Kind:        KIND_SCALAR,
		Name:        "ID",
		Description: "The `ID` scalar type represents a unique identifier, often used to refetch an object or as key for a cache.\nThe ID type appears in a JSON response as a String; however, it is not intended to be human-readable.\nWhen expected as an input type, any string (such as `\"4\"`) or integer (such as `4`) input value will be accepted\nas an ID.\n",
		// Add IDException after
	}, {
		Kind:        KIND_SCALAR,
		Name:        "Cursor",
		Description: "A cursor is an encoded string use for pagination",
	},
}

type introspection struct {
	schema      *sdata.DBSchema
	camelCase   bool
	types       map[string]FullType
	enumValues  map[string]EnumValue
	inputValues map[string]InputValue
	result      IntroResult
}

// introQuery returns the introspection query result
func (gj *GraphjinEngine) introQuery() (result json.RawMessage, err error) {

	// Initialize the introscpection object
	in := introspection{
		schema:      gj.schema,
		camelCase:   gj.conf.EnableCamelcase,
		types:       make(map[string]FullType),
		enumValues:  make(map[string]EnumValue),
		inputValues: make(map[string]InputValue),
	}

	// Initialize the schema
	in.result.Schema = IntrospectionSchema{
		QueryType:        &ShortFullType{Name: "Query"},
		SubscriptionType: &ShortFullType{Name: "Subscription"},
		MutationType:     &ShortFullType{Name: "Mutation"},
	}

	// Add the standard types
	// Add the standard types
	for _, v := range stdTypes {
		in.addType(v)
	}

	// Expression types
	v := append(expAll, expScalar...)
	in.addExpTypes(v, "ID", newTypeRef("", "ID", nil))
	in.addExpTypes(v, "String", newTypeRef("", "String", nil))
	in.addExpTypes(v, "Int", newTypeRef("", "Int", nil))
	in.addExpTypes(v, "Boolean", newTypeRef("", "Boolean", nil))
	in.addExpTypes(v, "Float", newTypeRef("", "Float", nil))
	in.addExpTypes(v, "ID", newTypeRef("", "ID", nil))
	in.addExpTypes(v, "String", newTypeRef("", "String", nil))
	in.addExpTypes(v, "Int", newTypeRef("", "Int", nil))
	in.addExpTypes(v, "Boolean", newTypeRef("", "Boolean", nil))
	in.addExpTypes(v, "Float", newTypeRef("", "Float", nil))

	// ListExpression Types
	v = append(expAll, expList...)
	in.addExpTypes(v, "StringList", newTypeRef("", "String", nil))
	in.addExpTypes(v, "IntList", newTypeRef("", "Int", nil))
	in.addExpTypes(v, "BooleanList", newTypeRef("", "Boolean", nil))
	in.addExpTypes(v, "FloatList", newTypeRef("", "Float", nil))
	in.addExpTypes(v, "StringList", newTypeRef("", "String", nil))
	in.addExpTypes(v, "IntList", newTypeRef("", "Int", nil))
	in.addExpTypes(v, "BooleanList", newTypeRef("", "Boolean", nil))
	in.addExpTypes(v, "FloatList", newTypeRef("", "Float", nil))

	v = append(expAll, expJSON...)
	in.addExpTypes(v, "JSON", newTypeRef("", "String", nil))
	in.addExpTypes(v, "JSON", newTypeRef("", "String", nil))

	// Add the roles
	// Add the roles
	in.addRolesEnumType(gj.roles)
	in.addTablesEnumType()

	// Get all the alias and add to the schema
	// Get all the alias and add to the schema
	for alias, t := range in.schema.GetAliases() {
		if err = in.addTable(t, alias); err != nil {
			return
		}
	}

	// Get all the tables and add to the schema
	// Get all the tables and add to the schema
	for _, t := range in.schema.GetTables() {
		if err = in.addTable(t, ""); err != nil {
			return
		}
	}

	// Add the directives
	// Add the directives
	for _, dt := range dirTypes {
		in.addDirType(dt)
	}
	in.addDirValidateType()

	// Add the types to the schema
	// Add the types to the schema
	for _, v := range in.types {
		in.result.Schema.Types = append(in.result.Schema.Types, v)
	}

	result, err = json.Marshal(in.result)
	return
}

// addTable adds a table to the introspection schema
func (in *introspection) addTable(table sdata.DBTable, alias string) (err error) {
	if table.Blocked || len(table.Columns) == 0 {
		return
	}
	var ftQS FullType

	// add table type to query and subscription
	if ftQS, err = in.addTableType(table, alias); err != nil {
		return
	}
	in.addTypeTo("Query", ftQS)
	in.addTypeTo("Subscription", ftQS)

	var ftM FullType

	// add table type to mutation
	if ftM, err = in.addInputType(table, ftQS); err != nil {
		return
	}
	in.addTypeTo("Mutation", ftM)

	// add tableByID type to query and subscription
	var ftQSByID FullType

	if ftQSByID, err = in.addTableType(table, alias); err != nil {
		return
	}

	ftQSByID.Name += "ByID"
	ftQSByID.addOrReplaceArg("id", newTypeRef(KIND_NONNULL, "", newTypeRef("", "ID", nil)))
	in.addType(ftQSByID)
	in.addTypeTo("Query", ftQSByID)
	in.addTypeTo("Subscription", ftQSByID)

	return
}

// addTypeTo adds a type to the introspection schema
func (in *introspection) addTypeTo(op string, ft FullType) {
	qt := in.types[op]
	qt.Fields = append(qt.Fields, FieldObject{
		Name:        ft.Name,
		Description: ft.Description,
		Args:        ft.InputFields,
		Type:        newTypeRef("", ft.Name, nil),
	})
	in.types[op] = qt
}

// getName returns the name of the type
func (in *introspection) getName(name string) string {
	if in.camelCase {
		return util.ToCamel(name)
	} else {
		return name
	}
}

// addExpTypes adds the expression types to the introspection schema
func (in *introspection) addExpTypes(exps []exp, name string, rt *TypeRef) {
	ft := FullType{
		Kind:        KIND_INPUT_OBJ,
		Name:        (name + SUFFIX_EXP),
		InputFields: []InputValue{},
		Interfaces:  []TypeRef{},
	}

	for _, ex := range exps {
		rtVal := rt
		if ex.etype != "" {
			rtVal = newTypeRef("", ex.etype, nil)
		}
		ft.InputFields = append(ft.InputFields, InputValue{
			Name:        ex.name,
			Description: ex.desc,
			Type:        rtVal,
		})
	}
	in.addType(ft)
}

// addTableType adds a table type to the introspection schema
func (in *introspection) addTableType(t sdata.DBTable, alias string) (ft FullType, err error) {
	return in.addTableTypeWithDepth(t, alias, 0)
}

// addTableTypeWithDepth adds a table type with depth to the introspection schema
func (in *introspection) addTableTypeWithDepth(
	table sdata.DBTable, alias string, depth int,
) (ft FullType, err error) {
	ft = FullType{
		Kind:        KIND_OBJECT,
		InputFields: []InputValue{},
		Interfaces:  []TypeRef{},
	}

	name := table.Name
	if alias != "" {
		name = alias
	}
	name = in.getName(name)

	ft.Name = name
	ft.Description = table.Comment

	var hasSearch bool
	var hasRecursive bool

	if err = in.addColumnsEnumType(table); err != nil {
		return
	}

	for _, fn := range in.schema.GetFunctions() {
		ty := in.addArgsType(table, fn)
		in.addType(ty)
	}

	for _, c := range table.Columns {
		if c.Blocked {
			continue
		}
		if c.FullText {
			hasSearch = true
		}
		if c.FKRecursive {
			hasRecursive = true
		}
		var f1 FieldObject
		f1, err = in.getColumnField(c)
		if err != nil {
			return
		}
		ft.Fields = append(ft.Fields, f1)
	}

	for _, fn := range in.schema.GetFunctions() {
		f1 := in.getFunctionField(table, fn)
		ft.Fields = append(ft.Fields, f1)
	}

	relNodes1, err := in.schema.GetFirstDegree(table)
	if err != nil {
		return
	}

	relNodes2, err := in.schema.GetSecondDegree(table)
	if err != nil {
		return
	}

	for _, relNode := range append(relNodes1, relNodes2...) {
		var f FieldObject
		var skip bool
		f, skip, err = in.getTableField(relNode)
		if err != nil {
			return
		}
		if !skip {
			ft.Fields = append(ft.Fields, f)
		}
	}

	ft.addArg("id", newTypeRef("", "ID", nil))
	ft.addArg("limit", newTypeRef("", "Int", nil))
	ft.addArg("offset", newTypeRef("", "Int", nil))
	ft.addArg("distinctOn", newTypeRef("LIST", "", newTypeRef("", "String", nil)))
	ft.addArg("first", newTypeRef("", "Int", nil))
	ft.addArg("last", newTypeRef("", "Int", nil))
	ft.addArg("after", newTypeRef("", "Cursor", nil))
	ft.addArg("before", newTypeRef("", "Cursor", nil))

	in.addOrderByType(table, &ft)
	in.addWhereType(table, &ft)
	in.addTableArgsType(table, &ft)

	if hasSearch {
		ft.addArg("search", newTypeRef("", "String", nil))
	}

	if depth > 1 {
		return
	}
	if depth > 0 {
		ft.addArg("find", newTypeRef("", "FindSearchInput", nil))
	}

	in.addType(ft)

	if hasRecursive {
		_, err = in.addTableTypeWithDepth(table,
			(name + "Recursive"),
			(depth + 1))
	}
	return
}

// addColumnsEnumType adds an enum type for the columns of the table
func (in *introspection) addColumnsEnumType(t sdata.DBTable) (err error) {
	tableName := in.getName(t.Name)
	ft := FullType{
		Kind:        KIND_ENUM,
		Name:        (t.Name + "Columns" + SUFFIX_ENUM),
		Description: fmt.Sprintf("Table columns for '%s'", tableName),
	}
	for _, c := range t.Columns {
		if c.Blocked {
			continue
		}
		ft.EnumValues = append(ft.EnumValues, EnumValue{
			Name:        in.getName(c.Name),
			Description: c.Comment,
		})
	}
	in.addType(ft)
	return
}

// addTablesEnumType adds an enum type for the tables
func (in *introspection) addTablesEnumType() {
	ft := FullType{
		Kind:        KIND_ENUM,
		Name:        ("tables" + SUFFIX_ENUM),
		Description: "All available tables",
	}
	for _, t := range in.schema.GetTables() {
		if t.Blocked {
			continue
		}
		ft.EnumValues = append(ft.EnumValues, EnumValue{
			Name:        in.getName(t.Name),
			Description: t.Comment,
		})
	}
	in.addType(ft)
}

// addRolesEnumType adds an enum type for the roles
func (in *introspection) addRolesEnumType(roles map[string]*Role) {
	ft := FullType{
		Kind:        KIND_ENUM,
		Name:        ("roles" + SUFFIX_ENUM),
		Description: "All available roles",
	}
	for name, ro := range roles {
		cmt := ro.Comment
		if ro.Match != "" {
			cmt = fmt.Sprintf("%s (Match: %s)", cmt, ro.Match)
		}
		ft.EnumValues = append(ft.EnumValues, EnumValue{
			Name:        name,
			Description: cmt,
		})
	}
	in.addType(ft)
}

// addOrderByType adds an order by type to the introspection schema
func (in *introspection) addOrderByType(t sdata.DBTable, ft *FullType) {
	ty := FullType{
		Kind: KIND_INPUT_OBJ,
		Name: (t.Name + SUFFIX_ORDER_BY),
	}
	for _, c := range t.Columns {
		if c.Blocked {
			continue
		}
		ty.InputFields = append(ty.InputFields, InputValue{
			Name:        in.getName(c.Name),
			Description: c.Comment,
			Type:        newTypeRef("", "OrderDirection", nil),
		})
	}
	in.addType(ty)
	ft.addArg("orderBy", newTypeRef("", (t.Name+SUFFIX_ORDER_BY), nil))
}

// addWhereType adds a where type to the introspection schema
func (in *introspection) addWhereType(table sdata.DBTable, ft *FullType) {
	tablename := (table.Name + SUFFIX_WHERE)
	ty := FullType{
		Kind: "INPUT_OBJECT",
		Name: tablename,
		InputFields: []InputValue{
			{Name: "and", Type: newTypeRef("", tablename, nil)},
			{Name: "or", Type: newTypeRef("", tablename, nil)},
			{Name: "not", Type: newTypeRef("", tablename, nil)},
		},
	}
	for _, c := range table.Columns {
		if c.Blocked {
			continue
		}
		ft := getTypeFromColumn(c)
		if c.Array {
			ft += SUFFIX_LISTEXP
		} else {
			ft += SUFFIX_EXP
		}
		ty.InputFields = append(ty.InputFields, InputValue{
			Name:        in.getName(c.Name),
			Description: c.Comment,
			Type:        newTypeRef("", ft, nil),
		})
	}
	in.addType(ty)
	ft.addArg("where", newTypeRef("", ty.Name, nil))
}

func (in *introspection) addInputType(table sdata.DBTable, ft FullType) (retFT FullType, err error) {
	// upsert
	ty := FullType{
		Kind:        "INPUT_OBJECT",
		Name:        ("upsert" + table.Name + SUFFIX_INPUT),
		InputFields: []InputValue{},
	}
	for _, c := range table.Columns {
		if c.Blocked {
			continue
		}
		ft1 := getTypeFromColumn(c)
		ty.InputFields = append(ty.InputFields, InputValue{
			Name:        in.getName(c.Name),
			Description: c.Comment,
			Type:        newTypeRef("", ft1, nil),
		})
	}
	in.addType(ty)
	ft.addArg("upsert", newTypeRef("", ty.Name, nil))

	// insert
	relNodes1, err := in.schema.GetFirstDegree(table)
	if err != nil {
		return
	}
	relNodes2, err := in.schema.GetSecondDegree(table)
	if err != nil {
		return
	}
	allNodes := append(relNodes1, relNodes2...)
	fieldLen := len(ty.InputFields)

	ty.Name = ("insert" + table.Name + SUFFIX_INPUT)
	for _, relNode := range allNodes {
		t1 := relNode.Table
		if relNode.Type == sdata.RelRemote ||
			relNode.Type == sdata.RelPolymorphic ||
			relNode.Type == sdata.RelEmbedded {
			continue
		}
		ty.InputFields = append(ty.InputFields, InputValue{
			Name:        in.getName(t1.Name),
			Description: t1.Comment,
			Type:        newTypeRef("", ("insert" + t1.Name + SUFFIX_INPUT), nil),
		})
	}
	in.addType(ty)
	ft.addArg("insert", newTypeRef("", ty.Name, nil))

	// update
	ty.Name = ("update" + table.Name + SUFFIX_INPUT)
	i := 0
	for _, relNode := range allNodes {
		t1 := relNode.Table
		if relNode.Type == sdata.RelRemote ||
			relNode.Type == sdata.RelPolymorphic ||
			relNode.Type == sdata.RelEmbedded {
			continue
		}
		ty.InputFields[(fieldLen + i)] = InputValue{
			Name:        in.getName(t1.Name),
			Description: t1.Comment,
			Type:        newTypeRef("", ("update" + t1.Name + SUFFIX_INPUT), nil),
		}
		i++
	}
	description1 := fmt.Sprintf("Connect to rows in table '%s' that match the expression", in.getName(table.Name))
	ty.InputFields = append(ty.InputFields, InputValue{
		Name:        "connect",
		Description: description1,
		Type:        newTypeRef("", (table.Name + SUFFIX_WHERE), nil),
	})
	description2 := fmt.Sprintf("Disconnect from rows in table '%s' that match the expression", in.getName(table.Name))
	ty.InputFields = append(ty.InputFields, InputValue{
		Name:        "disconnect",
		Description: description2,
		Type:        newTypeRef("", (table.Name + SUFFIX_WHERE), nil),
	})
	desciption3 := fmt.Sprintf("Update rows in table '%s' that match the expression", in.getName(table.Name))
	ty.InputFields = append(ty.InputFields, InputValue{
		Name:        "where",
		Description: desciption3,
		Type:        newTypeRef("", (table.Name + SUFFIX_WHERE), nil),
	})
	in.addType(ty)
	ft.addArg("update", newTypeRef("", ty.Name, nil))

	// delete
	ft.addArg("delete", newTypeRef("", TYPE_BOOLEAN, nil))
	retFT = ft
	return
}

// addTableArgsType adds the table arguments type to the introspection schema
func (in *introspection) addTableArgsType(table sdata.DBTable, ft *FullType) {
	if table.Type != "function" {
		return
	}
	ty := in.addArgsType(table, table.Func)
	in.addType(ty)
	ft.addArg("args", newTypeRef("", ty.Name, nil))
}

// addArgsType adds the arguments type to the introspection schema
func (in *introspection) addArgsType(table sdata.DBTable, fn sdata.DBFunction) (ft FullType) {
	ft = FullType{
		Kind: "INPUT_OBJECT",
		Name: (table.Name + fn.Name + SUFFIX_ARGS),
	}
	for _, fi := range fn.Inputs {
		var tr *TypeRef
		if fn.Agg {
			tr = newTypeRef("", (table.Name + "Columns" + SUFFIX_ENUM), nil)
		} else {
			tn, list := getType(fi.Type)
			if tn == "" {
				tn = "String"
			}
			tr = newTypeRef("", tn, nil)
			if list {
				tr = newTypeRef(KIND_LIST, "", tr)
			}
		}

		fname := in.getName(fi.Name)
		if fname == "" {
			fname = "_" + strconv.Itoa(fi.ID)
		}
		ft.InputFields = append(ft.InputFields, InputValue{
			Name: fname,
			Type: tr,
		})
	}
	return
}

// getColumnField returns the field object for the given column
func (in *introspection) getColumnField(column sdata.DBColumn) (field FieldObject, err error) {
	field.Args = []InputValue{}
	field.Name = in.getName(column.Name)
	typeValue := newTypeRef("", "String", nil)

	if v, ok := in.types[getTypeFromColumn(column)]; ok {
		typeValue.Name = &v.Name
		typeValue.Kind = v.Kind
	}

	if column.Array {
		typeValue = newTypeRef(KIND_LIST, "", typeValue)
	}

	if column.NotNull {
		typeValue = newTypeRef(KIND_NONNULL, "", typeValue)
	}

	field.Type = typeValue

	field.Args = append(field.Args, InputValue{
		Name: "includeIf", Type: newTypeRef("", (column.Table + SUFFIX_WHERE), nil),
	})

	field.Args = append(field.Args, InputValue{
		Name: "skipIf", Type: newTypeRef("", (column.Table + SUFFIX_WHERE), nil),
	})

	return
}

// getFunctionField returns the field object for the given function
func (in *introspection) getFunctionField(t sdata.DBTable, fn sdata.DBFunction) (f FieldObject) {
	f.Name = in.getName(fn.Name)
	f.Args = []InputValue{}
	ty, list := getType(fn.Type)
	f.Type = newTypeRef("", ty, nil)
	if list {
		f.Type = newTypeRef(KIND_LIST, "", f.Type)
	}

	if len(fn.Inputs) != 0 {
		typeName := (t.Name + fn.Name + SUFFIX_ARGS)
		argsArg := InputValue{Name: "args", Type: newTypeRef("", typeName, nil)}
		f.Args = append(f.Args, argsArg)
	}

	f.Args = append(f.Args, InputValue{
		Name: "includeIf", Type: newTypeRef("", (t.Name + SUFFIX_WHERE), nil),
	})

	f.Args = append(f.Args, InputValue{
		Name: "skipIf", Type: newTypeRef("", (t.Name + SUFFIX_WHERE), nil),
	})
	return
}

// getTableField returns the field object for the given table
func (in *introspection) getTableField(relNode sdata.RelNode) (
	f FieldObject, skip bool, err error,
) {
	f.Args = []InputValue{}
	f.Name = in.getName(relNode.Name)

	tn := in.getName(relNode.Table.Name)
	if _, ok := in.types[tn]; !ok && relNode.Type != sdata.RelRecursive {
		skip = true
		return
	}

	switch relNode.Type {
	case sdata.RelOneToOne:
		f.Type = newTypeRef(KIND_LIST, "", newTypeRef("", tn, nil))
	case sdata.RelRecursive:
		tn += "Recursive"
		f.Type = newTypeRef(KIND_LIST, "", newTypeRef("", tn, nil))
	default:
		f.Type = newTypeRef("", tn, nil)
	}
	return
}

// addDirType adds a directive type to the introspection schema
func (in *introspection) addDirType(dt dir) {
	d := DirectiveType{
		Name:         dt.name,
		Description:  dt.desc,
		Locations:    dt.locs,
		IsRepeatable: dt.repeat,
	}
	for _, a := range dt.args {
		d.Args = append(d.Args, InputValue{
			Name:        a.name,
			Description: a.desc,
			Type:        newTypeRef("", a.atype, nil),
		})
	}
	if len(dt.args) == 0 {
		d.Args = []InputValue{}
	}
	in.result.Schema.Directives = append(in.result.Schema.Directives, d)
}

// addDirValidateType adds a validate directive type to the introspection schema
func (in *introspection) addDirValidateType() {
	ft := FullType{
		Kind:        KIND_ENUM,
		Name:        ("validateFormat" + SUFFIX_ENUM),
		Description: "Various formats supported by @validate",
	}
	for k := range valid.Formats {
		ft.EnumValues = append(ft.EnumValues, EnumValue{
			Name: k,
		})
	}
	in.addType(ft)

	d := DirectiveType{
		Name:         "validate",
		Description:  "Add a validation for input variables",
		Locations:    []string{LOC_QUERY, LOC_MUTATION, LOC_SUBSCRIPTION},
		IsRepeatable: true,
	}
	d.Args = append(d.Args, InputValue{
		Name:        "variable",
		Description: "Variable to add the validation on",
		Type:        newTypeRef(KIND_NONNULL, "", newTypeRef("", "String", nil)),
	})
	for k, v := range valid.Validators {
		if v.Type == "" {
			continue
		}
		var ty *TypeRef
		if v.List {
			ty = newTypeRef(KIND_LIST, "", newTypeRef("", v.Type, nil))
		} else {
			ty = newTypeRef("", v.Type, nil)
		}
		d.Args = append(d.Args, InputValue{
			Name:        k,
			Description: v.Description,
			Type:        ty,
		})
	}
	in.result.Schema.Directives = append(in.result.Schema.Directives, d)
}

// addArg adds an argument to the full type
func (ft *FullType) addArg(name string, tr *TypeRef) {
	ft.InputFields = append(ft.InputFields, InputValue{
		Name: name,
		Type: tr,
	})
}

// addOrReplaceArg adds or replaces an argument to the full type
func (ft *FullType) addOrReplaceArg(name string, tr *TypeRef) {
	for i, a := range ft.InputFields {
		if a.Name == name {
			ft.InputFields[i].Type = tr
			return
		}
	}
	ft.InputFields = append(ft.InputFields, InputValue{
		Name: name,
		Type: tr,
	})
}

// addType adds a type to the introspection schema
func (in *introspection) addType(ft FullType) {
	in.types[ft.Name] = ft
}

func newTypeRef(kind, name string, tr *TypeRef) *TypeRef {
	if name == "" {
		return &TypeRef{Kind: kind, Name: nil, OfType: tr}
	}
	return &TypeRef{Kind: kind, Name: &name, OfType: tr}
}

// Returns the type of the given column. Returns ID if column is the primary key
func getTypeFromColumn(col sdata.DBColumn) (gqlType string) {
	if col.PrimaryKey {
		gqlType = "ID"
		return
	}
	gqlType, _ = getType(col.Type)
	return
}

// Returns the GraphQL type for the given column type
func getType(t string) (gqlType string, list bool) {
	if i := strings.IndexRune(t, '('); i != -1 {
		t = t[:i]
	}
	if i := strings.IndexRune(t, '['); i != -1 {
		list = true
		t = t[:i]
	}
	if v, ok := dbTypes[t]; ok {
		gqlType = v
	} else if t == "json" || t == "jsonb" {
		gqlType = "JSON"
	} else {
		gqlType = "String"
	}
	return
}
