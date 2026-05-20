package core

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestIntrospectionIncludesUnderscoreOperators(t *testing.T) {
	// Create a simple in-memory schema for testing
	di := sdata.GetTestDBInfo()
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a minimal config
	conf := &Config{
		DBType: "postgres",
	}

	// Create a GraphJin engine directly
	gj := &graphjinEngine{
		conf:      conf,
		roles:     make(map[string]*Role),
		defaultDB: "default",
		databases: map[string]*dbContext{
			"default": {name: "default", schema: schema},
		},
	}

	result, err := gj.introQuery()
	if err != nil {
		t.Fatal(err)
	}

	var introResult IntroResult
	if err := json.Unmarshal(result, &introResult); err != nil {
		t.Fatal(err)
	}

	// Check if IntExpression type exists and has _eq field
	var intExpressionType *FullType
	for _, typ := range introResult.Schema.Types {
		if typ.Name == "IntExpression" {
			intExpressionType = &typ
			break
		}
	}

	if intExpressionType == nil {
		t.Fatal("IntExpression type not found in schema")
	}

	// Check for _eq field
	hasEq := false
	for _, field := range intExpressionType.InputFields {
		if field.Name == "_eq" {
			hasEq = true
			break
		}
	}

	if !hasEq {
		t.Error("IntExpression type does not have _eq field")
	}

	// Check if any WhereInput type exists and has _or field
	var whereInputType *FullType
	for _, typ := range introResult.Schema.Types {
		if len(typ.Name) > 10 && typ.Name[len(typ.Name)-10:] == "WhereInput" {
			whereInputType = &typ
			break
		}
	}

	if whereInputType == nil {
		t.Fatal("No WhereInput type found in schema")
	}

	// Check for _or field
	hasOr := false
	for _, field := range whereInputType.InputFields {
		if field.Name == "_or" {
			hasOr = true
			break
		}
	}

	if !hasOr {
		t.Errorf("WhereInput type %s does not have _or field", whereInputType.Name)
	}
}

func TestIntrospectionIncludesBothOperatorFormats(t *testing.T) {
	// Create a simple in-memory schema for testing
	di := sdata.GetTestDBInfo()
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a minimal config
	conf := &Config{
		DBType: "postgres",
	}

	// Create a GraphJin engine directly
	gj := &graphjinEngine{
		conf:      conf,
		roles:     make(map[string]*Role),
		defaultDB: "default",
		databases: map[string]*dbContext{
			"default": {name: "default", schema: schema},
		},
	}

	result, err := gj.introQuery()
	if err != nil {
		t.Fatal(err)
	}

	var introResult IntroResult
	if err := json.Unmarshal(result, &introResult); err != nil {
		t.Fatal(err)
	}

	// Find IntExpression type
	var intExpressionType *FullType
	for _, typ := range introResult.Schema.Types {
		if typ.Name == "IntExpression" {
			intExpressionType = &typ
			break
		}
	}

	if intExpressionType == nil {
		t.Fatal("IntExpression type not found in schema")
	}

	// Check that we have both formats of operators
	operatorPairs := []struct {
		camelCase  string
		underscore string
	}{
		{"equals", "_eq"},
		{"notEquals", "_neq"},
		{"greaterThan", "_gt"},
		{"lesserThan", "_lt"},
		{"greaterOrEquals", "_gte"},
		{"lesserOrEquals", "_lte"},
	}

	for _, pair := range operatorPairs {
		hasCamelCase := false
		hasUnderscore := false

		for _, field := range intExpressionType.InputFields {
			if field.Name == pair.camelCase {
				hasCamelCase = true
			}
			if field.Name == pair.underscore {
				hasUnderscore = true
			}
		}

		if !hasCamelCase {
			t.Errorf("IntExpression type missing camelCase operator: %s", pair.camelCase)
		}
		if !hasUnderscore {
			t.Errorf("IntExpression type missing underscore operator: %s", pair.underscore)
		}
	}

	// Check WhereInput boolean operators
	var whereInputType *FullType
	for _, typ := range introResult.Schema.Types {
		if len(typ.Name) > 10 && typ.Name[len(typ.Name)-10:] == "WhereInput" {
			whereInputType = &typ
			break
		}
	}

	if whereInputType == nil {
		t.Fatal("No WhereInput type found in schema")
	}

	boolOperatorPairs := []struct {
		camelCase  string
		underscore string
	}{
		{"and", "_and"},
		{"or", "_or"},
		{"not", "_not"},
	}

	for _, pair := range boolOperatorPairs {
		hasCamelCase := false
		hasUnderscore := false

		for _, field := range whereInputType.InputFields {
			if field.Name == pair.camelCase {
				hasCamelCase = true
			}
			if field.Name == pair.underscore {
				hasUnderscore = true
			}
		}

		if !hasCamelCase {
			t.Errorf("WhereInput type missing camelCase operator: %s", pair.camelCase)
		}
		if !hasUnderscore {
			t.Errorf("WhereInput type missing underscore operator: %s", pair.underscore)
		}
	}
}

func TestIntrospectionIncludesSyntheticAggregateFields(t *testing.T) {
	introResult := introspectTestDB(t, &Config{DBType: "postgres"})
	products := requireIntroType(t, introResult, "products")

	for _, name := range []string{"count_id", "count_name", "sum_price", "avg_price", "min_price", "max_price"} {
		requireIntroField(t, products, name)
	}

	requireNoIntroField(t, products, "sum_name")
	requireNoIntroField(t, products, "avg_name")

	if got := introFieldTypeName(requireIntroField(t, products, "count_id").Type); got != "Int" {
		t.Fatalf("count_id type = %q, want Int", got)
	}
	if got := introFieldTypeName(requireIntroField(t, products, "sum_price").Type); got != "Float" {
		t.Fatalf("sum_price type = %q, want Float", got)
	}
}

func TestIntrospectionSyntheticAggregateCollisionPreservesPhysicalField(t *testing.T) {
	di := sdata.NewDBInfo("postgres", 0, "public", "", []sdata.DBColumn{
		{Schema: "public", Table: "things", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "things", Name: "likes", Type: "integer"},
		{
			Schema:  "public",
			Table:   "things",
			Name:    "count_likes",
			Type:    "text",
			Comment: "physical count column",
		},
	}, nil, nil)

	introResult := introspectDBInfo(t, di, &Config{DBType: "postgres"})
	things := requireIntroType(t, introResult, "things")
	countLikes := requireIntroField(t, things, "count_likes")

	if got := introFieldTypeName(countLikes.Type); got != "String" {
		t.Fatalf("count_likes type = %q, want String from physical column", got)
	}
}

func TestIntrospectionSkipsSyntheticAggregatesWhenDisabled(t *testing.T) {
	introResult := introspectTestDB(t, &Config{DBType: "postgres", DisableAgg: true})
	products := requireIntroType(t, introResult, "products")

	requireIntroField(t, products, "id")
	requireNoIntroField(t, products, "count_id")
	requireNoIntroField(t, products, "sum_price")
}

func TestIntrospectionIncludesSyntheticCursorFields(t *testing.T) {
	introResult := introspectTestDB(t, &Config{DBType: "postgres"})

	query := requireIntroType(t, introResult, "Query")
	productsCursor := requireIntroField(t, query, "products_cursor")
	if got := introFieldTypeName(productsCursor.Type); got != "Cursor" {
		t.Fatalf("products_cursor type = %q, want Cursor", got)
	}
	requireNoIntroField(t, query, "productsByID_cursor")

	subscription := requireIntroType(t, introResult, "Subscription")
	requireIntroField(t, subscription, "products_cursor")

	mutation := requireIntroType(t, introResult, "Mutation")
	requireNoIntroField(t, mutation, "products_cursor")

	users := requireIntroType(t, introResult, "users")
	requireIntroField(t, users, "comments_cursor")
}

func TestIntrospectionIncludesFilesystemRemoteCursorField(t *testing.T) {
	in := Introspection{
		types: map[string]FullType{},
	}
	in.addType(FullType{Kind: KIND_OBJECT, Name: "Query"})

	err := in.addRemoteTable(sdata.DBTable{
		Schema: "public",
		Name:   "avatars",
		Type:   "remote",
		Columns: []sdata.DBColumn{
			{Schema: "public", Table: "avatars", Name: "key", Type: "text"},
			{Schema: "public", Table: "avatars", Name: "url", Type: "text"},
		},
		Args: []sdata.DBColumn{
			{Schema: "public", Table: "avatars", Name: "prefix", Type: "text"},
			{Schema: "public", Table: "avatars", Name: "inline_data", Type: "boolean"},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	query := in.types["Query"]
	requireIntroField(t, &query, "avatars")
	cursor := requireIntroField(t, &query, "avatars_cursor")
	if got := introFieldTypeName(cursor.Type); got != "Cursor" {
		t.Fatalf("avatars_cursor type = %q, want Cursor", got)
	}
}

func TestIntrospectionSyntheticFieldsRespectCamelcase(t *testing.T) {
	introResult := introspectTestDB(t, &Config{DBType: "postgres", EnableCamelcase: true})

	products := requireIntroType(t, introResult, "products")
	requireIntroField(t, products, "countId")
	requireIntroField(t, products, "sumPrice")
	requireNoIntroField(t, products, "count_id")
	requireNoIntroField(t, products, "sum_price")

	query := requireIntroType(t, introResult, "Query")
	requireIntroField(t, query, "products_cursor")
}

func introspectTestDB(t *testing.T, conf *Config) IntroResult {
	t.Helper()
	return introspectDBInfo(t, sdata.GetTestDBInfo(), conf)
}

func introspectDBInfo(t *testing.T, di *sdata.DBInfo, conf *Config) IntroResult {
	t.Helper()

	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	if conf == nil {
		conf = &Config{DBType: "postgres"}
	}

	gj := &graphjinEngine{
		conf:      conf,
		roles:     make(map[string]*Role),
		defaultDB: "default",
		databases: map[string]*dbContext{
			"default": {name: "default", schema: schema},
		},
	}

	result, err := gj.introQuery()
	if err != nil {
		t.Fatal(err)
	}

	var introResult IntroResult
	if err := json.Unmarshal(result, &introResult); err != nil {
		t.Fatal(err)
	}
	return introResult
}

func requireIntroType(t *testing.T, introResult IntroResult, name string) *FullType {
	t.Helper()
	for i := range introResult.Schema.Types {
		if introResult.Schema.Types[i].Name == name {
			return &introResult.Schema.Types[i]
		}
	}
	t.Fatalf("introspection type %q not found", name)
	return nil
}

func requireIntroField(t *testing.T, ft *FullType, name string) FieldObject {
	t.Helper()
	for _, field := range ft.Fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("field %q not found on type %q; fields: %v", name, ft.Name, introFieldNames(ft))
	return FieldObject{}
}

func requireNoIntroField(t *testing.T, ft *FullType, name string) {
	t.Helper()
	for _, field := range ft.Fields {
		if field.Name == name {
			t.Fatalf("field %q unexpectedly found on type %q", name, ft.Name)
		}
	}
}

func introFieldTypeName(tr *TypeRef) string {
	for tr != nil {
		if tr.Name != nil {
			return *tr.Name
		}
		tr = tr.OfType
	}
	return ""
}

func introFieldNames(ft *FullType) []string {
	names := make([]string, len(ft.Fields))
	for i, field := range ft.Fields {
		names[i] = field.Name
	}
	return names
}
