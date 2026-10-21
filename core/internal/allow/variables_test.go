package allow

import (
	"encoding/json"
	"testing"
)

func TestSetReplacesSavedVariableMetadata(t *testing.T) {
	for _, namespace := range []string{"", "shop"} {
		t.Run(namespace, func(t *testing.T) {
			fs := &testFS{}
			al, err := New(nil, fs, false)
			if err != nil {
				t.Fatal(err)
			}
			query := []byte(`query getProducts { products { id } }`)
			item := Item{Namespace: namespace, Name: "getProducts", Query: query, ActionJSON: map[string]json.RawMessage{"data": json.RawMessage(`{"name":""}`)}}
			if err := al.Set(item); err != nil {
				t.Fatal(err)
			}
			name := "getProducts"
			if namespace != "" {
				name = namespace + "." + name
			}
			loaded, err := al.GetByName(name, true)
			if err != nil || len(loaded.ActionJSON) != 1 {
				t.Fatalf("initial metadata: %v, %v", loaded.ActionJSON, err)
			}
			item.ActionJSON = nil
			if err := al.Set(item); err != nil {
				t.Fatal(err)
			}
			// Check the cached path and a fresh allow list, so clearing only the cache
			// cannot hide metadata left on disk.
			fresh, err := New(nil, fs, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, reader := range []*List{al, fresh} {
				got, err := reader.GetByName(name, true)
				if err != nil {
					t.Fatal(err)
				}
				if len(got.ActionJSON) != 0 {
					t.Fatalf("replacement resurrected removed metadata: %s", got.ActionJSON["data"])
				}
			}
		})
	}
}

func TestSetWithoutMetadataDoesNotCreateSidecar(t *testing.T) {
	fs := &testFS{}
	al, err := New(nil, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := al.Set(Item{Name: "getProducts", Query: []byte("query getProducts { products { id } }")}); err != nil {
		t.Fatal(err)
	}
	if exists, err := fs.Exists("/queries/getProducts.json"); err != nil || exists {
		t.Fatalf("unexpected metadata sidecar: exists=%v err=%v", exists, err)
	}
}
