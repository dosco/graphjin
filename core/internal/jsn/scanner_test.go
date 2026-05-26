package jsn

import "testing"

func TestJSONNumberHelpers(t *testing.T) {
	if !isJSONNumberStart('-') || !isJSONNumberStart('0') || isJSONNumberStart('.') {
		t.Fatal("unexpected number start classification")
	}
	if !isJSONNumberPart('-') || !isJSONNumberPart('E') || !isJSONNumberPart('.') || isJSONNumberPart('x') {
		t.Fatal("unexpected number part classification")
	}
}

func TestKeyHashLookupVerifiesExactKey(t *testing.T) {
	byteKeys := [][]byte{[]byte("id"), []byte("name"), []byte("id")}
	byteCollisions := map[uint64][]int{42: []int{0, 1, 2}}
	if !lookupBytesKeyHash(byteKeys, byteCollisions, 42, -1, []byte("id")) {
		t.Fatal("expected byte key collision lookup to find exact key")
	}
	if lookupBytesKeyHash(byteKeys, byteCollisions, 42, -1, []byte("other")) {
		t.Fatal("matched byte key by hash without exact key equality")
	}

	stringKeys := []string{"id", "name", "id"}
	stringCollisions := map[uint64][]int{42: []int{0, 1, 2}}
	if !lookupStringKeyHash(stringKeys, stringCollisions, 42, -1, []byte("id")) {
		t.Fatal("expected string key collision lookup to find exact key")
	}
	if lookupStringKeyHash(stringKeys, stringCollisions, 42, -1, []byte("other")) {
		t.Fatal("matched string key by hash without exact key equality")
	}
}

func TestFieldIndexVerifiesKeyAndValueAfterHashMatch(t *testing.T) {
	fields := []Field{
		{Key: []byte("id"), Value: []byte("1")},
	}

	if _, ok := lookupFieldHash(fields, nil, 42, 0, []byte("id"), []byte("2")); ok {
		t.Fatal("matched same key with different value")
	}
	if _, ok := lookupFieldHash(fields, nil, 42, 0, []byte("other"), []byte("1")); ok {
		t.Fatal("matched same value with different key")
	}
	if n, ok := lookupFieldHash(fields, nil, 42, 0, []byte("id"), []byte("1")); !ok || n != 0 {
		t.Fatalf("expected exact match at 0, got %d %v", n, ok)
	}
}

func TestFieldIndexKeepsLastDuplicateMatch(t *testing.T) {
	fields := []Field{
		{Key: []byte("id"), Value: []byte("1")},
		{Key: []byte("id"), Value: []byte("2")},
		{Key: []byte("id"), Value: []byte("1")},
	}
	collisions := map[uint64][]int{42: []int{0, 1, 2}}

	if n, ok := lookupFieldHash(fields, collisions, 42, -1, []byte("id"), []byte("1")); !ok || n != 2 {
		t.Fatalf("expected last duplicate match at 2, got %d %v", n, ok)
	}
}
