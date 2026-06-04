package cassandradriver

import (
	"reflect"
	"testing"
	"time"
)

func TestBindValue(t *testing.T) {
	cases := []struct {
		typ string
		in  any
		out any
	}{
		{"text", "hello", "hello"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"decimal", "12345678901234567890.123", "12345678901234567890.123"},
		{"varint", "99999999999999999999", "99999999999999999999"},
		{"int", float64(42), int64(42)},
		{"bigint", float64(9000), int64(9000)},
		{"double", float64(3.5), 3.5},
		{"boolean", true, true},
		{"blob", "aGVsbG8=", []byte("hello")},
		{"list<int>", []any{float64(1), float64(2)}, []any{int64(1), int64(2)}},
	}
	for _, tc := range cases {
		got, err := BindValue(tc.typ, tc.in)
		if err != nil {
			t.Fatalf("BindValue(%s, %v): %v", tc.typ, tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.out) {
			t.Fatalf("BindValue(%s): got %#v want %#v", tc.typ, got, tc.out)
		}
	}
}

func TestBindValue_TimestampAndMap(t *testing.T) {
	ts, err := BindValue("timestamp", "2026-06-04T10:30:00.000Z")
	if err != nil {
		t.Fatalf("bind timestamp: %v", err)
	}
	tm, ok := ts.(time.Time)
	if !ok || !tm.Equal(time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("timestamp bind wrong: %#v", ts)
	}

	m, err := BindValue("map<text, int>", map[string]any{"a": float64(1)})
	if err != nil {
		t.Fatalf("bind map: %v", err)
	}
	if !reflect.DeepEqual(m, map[string]any{"a": int64(1)}) {
		t.Fatalf("map bind wrong: %#v", m)
	}
}

func TestDecodeValue(t *testing.T) {
	// timestamp -> RFC3339(ms)
	got, err := DecodeValue("timestamp", time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("decode ts: %v", err)
	}
	if got != "2026-06-04T10:30:00.000Z" {
		t.Fatalf("decode timestamp: got %v", got)
	}

	// blob -> base64
	b, err := DecodeValue("blob", []byte("hello"))
	if err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if b != "aGVsbG8=" {
		t.Fatalf("decode blob: got %v", b)
	}

	// list<text> recursion passthrough
	l, err := DecodeValue("list<text>", []any{"a", "b"})
	if err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !reflect.DeepEqual(l, []any{"a", "b"}) {
		t.Fatalf("decode list: got %#v", l)
	}
}

func TestRoundTripTimestamp(t *testing.T) {
	const s = "2026-06-04T10:30:00.000Z"
	bound, err := BindValue("timestamp", s)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	back, err := DecodeValue("timestamp", bound)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != s {
		t.Fatalf("timestamp round-trip: got %v want %v", back, s)
	}
}

func TestRoundTripBlob(t *testing.T) {
	const s = "aGVsbG8="
	bound, err := BindValue("blob", s)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	back, err := DecodeValue("blob", bound)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back != s {
		t.Fatalf("blob round-trip: got %v want %v", back, s)
	}
}
