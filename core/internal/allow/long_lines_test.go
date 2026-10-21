package allow

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadGQLPreservesLongLines(t *testing.T) {
	query := `query getProducts { products(where: { name: { eq: "` + strings.Repeat("a", 70*1024) + `" } }) { id } }`
	for _, newline := range []string{"", "\n", "\r\n"} {
		fs := &testFS{files: map[string][]byte{"/queries/getProducts.gql": []byte(query + newline)}}
		got, err := ReadGQL(fs, "/queries/getProducts.gql")
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte(query + "\n"); !bytes.Equal(got, want) {
			t.Fatalf("newline %q: got %d bytes, want %d", newline, len(got), len(want))
		}
	}
}

func TestSavedQueryLongLineRoundTrip(t *testing.T) {
	fs := &testFS{}
	al, err := New(nil, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	query := `query getProducts { products(where: { name: { eq: "` + strings.Repeat("a", 70*1024) + `" } }) { id } }`
	if err := al.Set(Item{Name: "getProducts", Query: []byte(query)}); err != nil {
		t.Fatal(err)
	}
	item, err := al.GetByName("getProducts", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(item.Query), []byte(query)) {
		t.Fatalf("got %d query bytes, want %d", len(item.Query), len(query))
	}
}

func TestReadGQLPreservesLongImportedFragment(t *testing.T) {
	fragment := `fragment ProductFields on products { name(args: "` + strings.Repeat("a", 70*1024) + `") }`
	query := "#import \"./fragments/ProductFields\"\nquery getProducts { products { ...ProductFields } }"
	fs := &testFS{files: map[string][]byte{
		"/queries/getProducts.gql":             []byte(query),
		"/queries/fragments/ProductFields.gql": []byte(fragment),
	}}
	got, err := ReadGQL(fs, "/queries/getProducts.gql")
	if err != nil {
		t.Fatal(err)
	}
	want := fragment + "\nquery getProducts { products { ...ProductFields } }\n"
	if string(got) != want {
		t.Fatalf("got %d bytes, want %d", len(got), len(want))
	}
}
