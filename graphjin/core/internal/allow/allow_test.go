package allow

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type testFS struct {
	files map[string][]byte
}

func (f *testFS) Get(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return append([]byte(nil), data...), nil
}

func (f *testFS) Put(path string, data []byte) error {
	if f.files == nil {
		f.files = make(map[string][]byte)
	}
	f.files[path] = append([]byte(nil), data...)
	return nil
}

func (f *testFS) Exists(path string) (bool, error) {
	_, ok := f.files[path]
	return ok, nil
}

func (f *testFS) List(path string) ([]string, error) {
	return nil, nil
}

func TestGetByNameRejectsTraversalNames(t *testing.T) {
	al, err := New(nil, &testFS{}, true)
	if err != nil {
		t.Fatalf("new allow list: %v", err)
	}

	names := []string{"../db", `..\db`, "a/../../b", "", "shop..user"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, err := al.GetByName(name, false)
			if !errors.Is(err, ErrInvalidLookupName) {
				t.Fatalf("expected invalid lookup name error, got %v", err)
			}
		})
	}
}

func TestGetFragmentRejectsTraversalNames(t *testing.T) {
	al, err := New(nil, &testFS{}, true)
	if err != nil {
		t.Fatalf("new allow list: %v", err)
	}

	_, err = al.GetFragment("../x")
	if !errors.Is(err, ErrInvalidLookupName) {
		t.Fatalf("expected invalid lookup name error, got %v", err)
	}
}

func TestGetByNameAllowsNamespacedQueries(t *testing.T) {
	fs := &testFS{files: map[string][]byte{
		"/queries/getProducts.gql":                []byte("query getProducts { products { id } }"),
		"/queries/shop.user_fields.gql":           []byte("query user_fields { users { id } }"),
		"/queries/fragments/shop.user_fields.gql": []byte("fragment UserFields on users { id }"),
	}}

	al, err := New(nil, fs, true)
	if err != nil {
		t.Fatalf("new allow list: %v", err)
	}

	if _, err := al.GetByName("getProducts", false); err != nil {
		t.Fatalf("expected root query name to load, got %v", err)
	}

	if _, err := al.GetByName("shop.user_fields", false); err != nil {
		t.Fatalf("expected namespaced query name to load, got %v", err)
	}

	if _, err := al.GetFragment("shop.user_fields"); err != nil {
		t.Fatalf("expected namespaced fragment name to load, got %v", err)
	}
}

func TestGetByNameRejectsEscapingImports(t *testing.T) {
	fs := &testFS{files: map[string][]byte{
		"/queries/getProducts.gql": []byte(strings.Join([]string{
			`#import "../secret"`,
			`query getProducts { products { id } }`,
		}, "\n")),
		"/secret.gql": []byte(`fragment Secret on users { id }`),
	}}

	al, err := New(nil, fs, true)
	if err != nil {
		t.Fatalf("new allow list: %v", err)
	}

	_, err = al.GetByName("getProducts", false)
	if !errors.Is(err, ErrInvalidImportPath) {
		t.Fatalf("expected invalid import path error, got %v", err)
	}
}
