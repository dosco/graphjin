package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// TestBuildFederationSDL_Smoke verifies the SDL contains the federation
// link header, an entity type with @key, the _Service/_Entity built-ins,
// and the extended Query.
func TestBuildFederationSDL_Smoke(t *testing.T) {
	schema, err := sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}

	sdl, err := BuildFederationSDL(schema, FederationConfig{Enabled: true})
	if err != nil {
		t.Fatalf("BuildFederationSDL failed: %v", err)
	}

	wantContains := []string{
		`@link(url: "https://specs.apollo.dev/federation/v2.5"`,
		`"@key"`,
		`"@shareable"`,
		`@key(fields: "id")`,
		`scalar _Any`,
		`type _Service {`,
		`union _Entity =`,
		`extend type Query {`,
		`_service: _Service!`,
		`_entities(representations: [_Any!]!): [_Entity]!`,
	}
	for _, want := range wantContains {
		if !strings.Contains(sdl, want) {
			t.Errorf("SDL missing expected fragment %q\n----\n%s\n----", want, sdl)
		}
	}

	// At least one of the test schema's tables (users/products/customers)
	// should be present as a federated entity.
	expectedTypeFound := false
	for _, name := range []string{"Users", "Products", "Customers"} {
		if strings.Contains(sdl, "type "+name+" @key") {
			expectedTypeFound = true
			break
		}
	}
	if !expectedTypeFound {
		t.Errorf("expected at least one federated entity type, none found in SDL:\n%s", sdl)
	}
}

func TestBuildFederationSDL_VersionOverride(t *testing.T) {
	schema, _ := sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)

	sdl, err := BuildFederationSDL(schema, FederationConfig{Version: "v2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sdl, "federation/v2.3") {
		t.Errorf("expected version v2.3 in SDL, got:\n%s", sdl)
	}
}

func TestBuildFederationSDL_KeyOverride(t *testing.T) {
	schema, _ := sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)

	sdl, err := BuildFederationSDL(schema, FederationConfig{
		Keys: map[string][]string{
			"users": {"email"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sdl, `type Users @key(fields: "email")`) {
		t.Errorf("expected key override on Users.email, got SDL:\n%s", sdl)
	}
}

func TestBuildFederationSDL_FieldDirectives(t *testing.T) {
	schema, _ := sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)

	sdl, err := BuildFederationSDL(schema, FederationConfig{
		Shareable:    []string{"Users.email"},
		Inaccessible: []string{"Users.encrypted_password"},
		Tags:         map[string][]string{"Users.full_name": {"pii"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"email: String! @shareable",
		"encrypted_password: String! @inaccessible",
		`full_name: String! @tag(name: "pii")`,
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("SDL missing %q\n----\n%s", want, sdl)
		}
	}
}

func TestBytesContainsToken(t *testing.T) {
	tests := []struct {
		s, tok string
		want   bool
	}{
		{"query { _service { sdl } }", "_service", true},
		{"query { my_service }", "_service", false},
		{"query { _serviceX }", "_service", false},
		{"query { _entities(representations: $x) { ... } }", "_entities", true},
		{"query { entities }", "_entities", false},
		{"_service", "_service", true},
		{"", "_service", false},
	}
	for _, tt := range tests {
		got := bytesContainsToken([]byte(tt.s), []byte(tt.tok))
		if got != tt.want {
			t.Errorf("bytesContainsToken(%q, %q) = %v, want %v", tt.s, tt.tok, got, tt.want)
		}
	}
}

func TestFederationTypeName(t *testing.T) {
	cases := map[string]string{
		"users":           "Users",
		"order_items":     "OrderItems",
		"line_item_tax":   "LineItemTax",
		"already_camel":   "AlreadyCamel",
	}
	for in, want := range cases {
		if got := federationTypeName(in); got != want {
			t.Errorf("federationTypeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONMarshalServiceSDL(t *testing.T) {
	out, err := jsonMarshalServiceSDL("type X { id: ID! }\n")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"_service":{"sdl":"type X { id: ID! }\n"}}`
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
