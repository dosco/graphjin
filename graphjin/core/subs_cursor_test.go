package core

import (
	"reflect"
	"testing"
)

func TestSystemCursorCodecRoundTrip(t *testing.T) {
	values := []any{"a,b:c", float64(42), true, nil}
	raw := encodeSystemCursor(17, values)
	id, got, ok := decodeSystemCursor(raw)
	if !ok {
		t.Fatal("decodeSystemCursor returned not ok")
	}
	if id != 17 {
		t.Fatalf("selection id = %d, want 17", id)
	}
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("values = %#v, want %#v", got, values)
	}
}

func TestSystemCursorCodecTolerance(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantID int32
		want   []any
		ok     bool
	}{
		{name: "prefixed", raw: "gj-65a8b3c0:" + encodeSystemCursor(2, []any{"x"}), wantID: 2, want: []any{"x"}, ok: true},
		{name: "legacy", raw: "3,a,b", wantID: 3, want: []any{"a", "b"}, ok: true},
		{name: "junk", raw: "junk", ok: false},
		{name: "bad version payload", raw: "3,m1:not-base64", ok: false},
		{name: "bad id", raw: "x,m1:W10", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, got, ok := decodeSystemCursor(tt.raw)
			if ok != tt.ok || id != tt.wantID || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("decodeSystemCursor(%q) = (%d, %#v, %v), want (%d, %#v, %v)",
					tt.raw, id, got, ok, tt.wantID, tt.want, tt.ok)
			}
		})
	}
}
