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
		schema: schema,
		conf:   conf,
		roles:  make(map[string]*Role),
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
		schema: schema,
		conf:   conf,
		roles:  make(map[string]*Role),
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
