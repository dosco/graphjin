package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCacheKeyBuilder_APQKey(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUser { user { id } }`)

	// APQ key should take precedence over operation name
	key1 := builder.Build(ctx, "MyQuery", "apq123", query, nil, "user")
	key2 := builder.Build(ctx, "DifferentQuery", "apq123", query, nil, "user")

	if key1 != key2 {
		t.Errorf("expected same key when APQ key matches, got %s vs %s", key1, key2)
	}

	// Different APQ keys should produce different keys
	key3 := builder.Build(ctx, "MyQuery", "apq456", query, nil, "user")
	if key1 == key3 {
		t.Errorf("expected different keys for different APQ keys")
	}
}

func TestCacheKeyBuilder_OpName(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUsers { users { id } }`)

	// Falls back to operation name when no APQ key
	key1 := builder.Build(ctx, "GetUsers", "", query, nil, "user")
	key2 := builder.Build(ctx, "GetUsers", "", query, nil, "user")

	if key1 != key2 {
		t.Errorf("expected same key for same operation, got %s vs %s", key1, key2)
	}

	if key1 == "" {
		t.Errorf("expected non-empty key for named operation")
	}

	// Different operations should produce different keys
	key3 := builder.Build(ctx, "GetPosts", "", query, nil, "user")
	if key1 == key3 {
		t.Errorf("expected different keys for different operations")
	}
}

func TestCacheKeyBuilder_Anonymous(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`{ user { id } }`)

	// Anonymous queries (no opName, no APQ) should return empty string
	key := builder.Build(ctx, "", "", query, nil, "user")

	if key != "" {
		t.Errorf("expected empty key for anonymous query, got %s", key)
	}
}

func TestCacheKeyBuilder_Variables(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUser($id: Int!) { user(id: $id) { id } }`)

	vars1 := json.RawMessage(`{"id": 1}`)
	vars2 := json.RawMessage(`{"id": 2}`)

	key1 := builder.Build(ctx, "GetUser", "", query, vars1, "user")
	key2 := builder.Build(ctx, "GetUser", "", query, vars2, "user")

	if key1 == key2 {
		t.Errorf("expected different keys for different variables")
	}

	// Same variables should produce same key
	key3 := builder.Build(ctx, "GetUser", "", query, vars1, "user")
	if key1 != key3 {
		t.Errorf("expected same key for same variables, got %s vs %s", key1, key3)
	}
}

func TestCacheKeyBuilder_RoleIsolation(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetOrders { orders { id } }`)

	// Different roles should produce different keys
	keyAdmin := builder.Build(ctx, "GetOrders", "", query, nil, "admin")
	keyUser := builder.Build(ctx, "GetOrders", "", query, nil, "user")

	if keyAdmin == keyUser {
		t.Errorf("expected different keys for different roles")
	}
}

func TestCacheKeyBuilder_UserIsolation(t *testing.T) {
	builder := NewCacheKeyBuilder()
	query := []byte(`query GetProfile { profile { id } }`)

	// Different user IDs should produce different keys
	ctx1 := context.WithValue(context.Background(), UserIDKey, "user1")
	ctx2 := context.WithValue(context.Background(), UserIDKey, "user2")

	key1 := builder.Build(ctx1, "GetProfile", "", query, nil, "user")
	key2 := builder.Build(ctx2, "GetProfile", "", query, nil, "user")

	if key1 == key2 {
		t.Errorf("expected different keys for different user IDs")
	}

	// Same user ID should produce same key
	ctx3 := context.WithValue(context.Background(), UserIDKey, "user1")
	key3 := builder.Build(ctx3, "GetProfile", "", query, nil, "user")

	if key1 != key3 {
		t.Errorf("expected same key for same user ID, got %s vs %s", key1, key3)
	}
}

func TestCacheKeyBuilder_Determinism(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.WithValue(context.Background(), UserIDKey, "testuser")
	query := []byte(`query ListItems { items { id } }`)
	vars := json.RawMessage(`{"limit": 10, "offset": 0}`)

	// Same inputs should always produce same key
	key1 := builder.Build(ctx, "ListItems", "", query, vars, "member")
	key2 := builder.Build(ctx, "ListItems", "", query, vars, "member")
	key3 := builder.Build(ctx, "ListItems", "", query, vars, "member")

	if key1 != key2 || key2 != key3 {
		t.Errorf("expected deterministic keys, got %s, %s, %s", key1, key2, key3)
	}

	// Key should be hex-encoded SHA256 (64 chars)
	if len(key1) != 64 {
		t.Errorf("expected 64-char hex key, got %d chars", len(key1))
	}
}

func TestCacheKeyBuilder_EmptyVariables(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUsers { users { id } }`)

	// Empty JSON object
	varsEmpty := json.RawMessage(`{}`)
	key1 := builder.Build(ctx, "GetUsers", "", query, varsEmpty, "user")
	if key1 == "" {
		t.Errorf("expected non-empty key with empty object vars")
	}

	// Nil variables
	key2 := builder.Build(ctx, "GetUsers", "", query, nil, "user")
	if key2 == "" {
		t.Errorf("expected non-empty key with nil vars")
	}

	// Empty and nil should produce different keys
	if key1 == key2 {
		t.Errorf("empty object and nil vars should produce different keys")
	}
}

func TestCacheKeyBuilder_NilContext(t *testing.T) {
	builder := NewCacheKeyBuilder()

	// Context without UserID should still work
	ctx := context.Background()
	query := []byte(`query GetUsers { users { id } }`)
	key := builder.Build(ctx, "GetUsers", "", query, nil, "user")

	if key == "" {
		t.Errorf("expected non-empty key with context missing UserID")
	}
}

func TestCacheKeyBuilder_EmptyRole(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUsers { users { id } }`)

	// Empty role should still produce valid key
	key := builder.Build(ctx, "GetUsers", "", query, nil, "")
	if key == "" {
		t.Errorf("expected non-empty key with empty role")
	}

	// Empty role and "user" role should produce different keys
	keyUser := builder.Build(ctx, "GetUsers", "", query, nil, "user")
	if key == keyUser {
		t.Errorf("expected different keys for empty vs non-empty role")
	}
}

func TestCacheKeyBuilder_SpecialCharacters(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()
	query := []byte(`query GetUsers { users { id } }`)

	// Role with special characters
	key1 := builder.Build(ctx, "GetUsers", "", query, nil, "admin/super")
	key2 := builder.Build(ctx, "GetUsers", "", query, nil, "admin:super")
	key3 := builder.Build(ctx, "GetUsers", "", query, nil, "admin super")

	// All should be different keys
	if key1 == key2 || key2 == key3 || key1 == key3 {
		t.Errorf("expected different keys for different special char roles")
	}

	// All should be valid non-empty keys
	if key1 == "" || key2 == "" || key3 == "" {
		t.Errorf("expected non-empty keys for special char roles")
	}
}

func TestCacheKeyBuilder_NumericUserID(t *testing.T) {
	builder := NewCacheKeyBuilder()
	query := []byte(`query GetProfile { profile { id } }`)

	// Integer user ID
	ctx1 := context.WithValue(context.Background(), UserIDKey, 12345)
	key1 := builder.Build(ctx1, "GetProfile", "", query, nil, "user")

	// String user ID with same value
	ctx2 := context.WithValue(context.Background(), UserIDKey, "12345")
	key2 := builder.Build(ctx2, "GetProfile", "", query, nil, "user")

	// They may be different due to type formatting, both should be valid
	if key1 == "" || key2 == "" {
		t.Errorf("expected non-empty keys for numeric and string user IDs")
	}
}

func TestCacheKeyBuilder_QueryIsolation(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()

	// Same operation name but different query text should produce different keys
	query1 := []byte(`query GetUsers { users { id } }`)
	query2 := []byte(`query GetUsers { users { id name } }`)

	key1 := builder.Build(ctx, "GetUsers", "", query1, nil, "user")
	key2 := builder.Build(ctx, "GetUsers", "", query2, nil, "user")

	if key1 == key2 {
		t.Errorf("expected different keys for different query text with same operation name")
	}
}

func TestCacheKeyBuilder_QueryWhitespace(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()

	// Different whitespace in query should produce different keys
	query1 := []byte(`query GetUsers { users { id } }`)
	query2 := []byte(`query GetUsers {
		users {
			id
		}
	}`)

	key1 := builder.Build(ctx, "GetUsers", "", query1, nil, "user")
	key2 := builder.Build(ctx, "GetUsers", "", query2, nil, "user")

	if key1 == key2 {
		t.Errorf("expected different keys for queries with different whitespace")
	}
}

func TestCacheKeyBuilder_NilQuery(t *testing.T) {
	builder := NewCacheKeyBuilder()
	ctx := context.Background()

	// Nil query should still produce valid key (for backward compatibility)
	key := builder.Build(ctx, "GetUsers", "", nil, nil, "user")
	if key == "" {
		t.Errorf("expected non-empty key with nil query")
	}

	// Nil query vs empty query should produce different keys
	keyEmpty := builder.Build(ctx, "GetUsers", "", []byte{}, nil, "user")
	if key != keyEmpty {
		t.Errorf("expected same key for nil and empty query")
	}
}

func TestShouldCache(t *testing.T) {
	tests := []struct {
		name   string
		opName string
		apqKey string
		want   bool
	}{
		{
			name:   "named query should cache",
			opName: "GetUsers",
			apqKey: "",
			want:   true,
		},
		{
			name:   "APQ query should cache",
			opName: "",
			apqKey: "abc123",
			want:   true,
		},
		{
			name:   "both opName and APQ should cache",
			opName: "GetUsers",
			apqKey: "abc123",
			want:   true,
		},
		{
			name:   "anonymous query should not cache",
			opName: "",
			apqKey: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldCacheQuery(tt.opName, tt.apqKey)
			if got != tt.want {
				t.Errorf("ShouldCacheQuery(%q, %q) = %v, want %v", tt.opName, tt.apqKey, got, tt.want)
			}
		})
	}
}
