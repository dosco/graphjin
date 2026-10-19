package cassandradriver

import (
	"bytes"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	ps := []byte{0x00, 0x01, 0xff, 0x42, 0x7e}
	c := EncodeCursor(ps)
	if c == "" {
		t.Fatalf("expected non-empty cursor")
	}
	back, err := DecodeCursor(c)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(back, ps) {
		t.Fatalf("round-trip: got %x want %x", back, ps)
	}
}

func TestCursorEmpty(t *testing.T) {
	if c := EncodeCursor(nil); c != "" {
		t.Fatalf("empty page-state should encode to empty cursor, got %q", c)
	}
	b, err := DecodeCursor("")
	if err != nil || b != nil {
		t.Fatalf("empty cursor should decode to nil, got %v %v", b, err)
	}
}

func TestCursorInvalid(t *testing.T) {
	if _, err := DecodeCursor("not-a-cql-cursor"); err == nil {
		t.Fatalf("expected error for non-Cassandra cursor")
	}
	if _, err := DecodeCursor("cql:@@@not-base64@@@"); err == nil {
		t.Fatalf("expected error for malformed base64")
	}
}
