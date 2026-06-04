package cassandradriver

import "testing"

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery(`{"operation":"query","root":{"table":"users","columns":["id"],"partition_keys":["id"]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Operation != OpQuery || q.Root == nil || q.Root.Table != "users" {
		t.Fatalf("unexpected parse result: %#v", q)
	}
}

func TestParseQuery_Errors(t *testing.T) {
	if _, err := ParseQuery(`{`); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	if _, err := ParseQuery(`{"root":{}}`); err == nil {
		t.Fatalf("expected error for missing operation")
	}
}

func TestSubstituteParams_Filter(t *testing.T) {
	q, err := ParseQuery(`{"operation":"query","root":{"table":"users","columns":["id"],"partition_keys":["id"],"filters":[{"col":"id","op":"eq","param":"$1"}]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := q.SubstituteParams([]any{"u1"}); err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if got := q.Root.Filters[0].Value; got != "u1" {
		t.Fatalf("param not inlined: got %#v", got)
	}
}

func TestSubstituteParams_Cursor(t *testing.T) {
	q, err := ParseQuery(`{"operation":"query","root":{"table":"users","columns":["id"],"partition_keys":["id"],"cursor_param":"$1","filters":[{"col":"id","op":"eq","value":"u1"}]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := q.SubstituteParams([]any{"cql:AAEC"}); err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if q.Root.resolvedCursor != "cql:AAEC" {
		t.Fatalf("cursor not inlined: got %q", q.Root.resolvedCursor)
	}
	if q.Root.CursorParam != "" {
		t.Fatalf("cursor param should be consumed")
	}
}
