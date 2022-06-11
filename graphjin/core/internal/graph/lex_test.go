package graph

import (
	"encoding/json"
	"reflect"
	"testing"
)

func Test_lex_JSON(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		json     string
		jsonItem int
	}{
		{
			name:     "json",
			input:    []byte("{\"id\":1,\"json\":\"{\\\"field1\\\":\\\"value1\\\", \\\"field2\\\":\\\"value2\\\"}\"}"),
			json:     "{\"field1\":\"value1\", \"field2\":\"value2\"}",
			jsonItem: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lex(tt.input)
			if err != nil {
				t.Fatalf("lex() error = %v", err)
			}
			if len(got.items) < tt.jsonItem {
				t.Fatal("lex() invalid items count")
			}
			j := got.items[tt.jsonItem]
			if j.String() != "string" {
				t.Fatal("lex() invalid type")
			}
			mapStructure := map[string]interface{}{}
			err = json.Unmarshal(tt.input, &mapStructure)
			if err != nil {
				t.Fatalf("json.Unmarshal error = %v", err)
			}
			if !reflect.DeepEqual(mapStructure["json"], tt.json) {
				t.Fatalf("lex() JSON = %v, want %v", got, tt.json)
			}
		})
	}
}
