package fstable

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLocal(t *testing.T) (*Local, string) {
	t.Helper()
	root := t.TempDir()
	b, err := NewLocal(LocalConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return b, root
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocal_ListRecursiveAndPaginate(t *testing.T) {
	b, root := newLocal(t)
	for _, k := range []string{"a/1.txt", "a/2.txt", "b/3.txt", "c/4.txt"} {
		writeFile(t, root, k, []byte("hello"))
	}

	page1, next, err := b.List(context.Background(), ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 size = %d, want 2", len(page1))
	}
	if next == "" {
		t.Fatalf("expected continuation token")
	}
	if page1[0].Key != "a/1.txt" || page1[1].Key != "a/2.txt" {
		t.Errorf("page1 keys = %v, want [a/1.txt a/2.txt]", []string{page1[0].Key, page1[1].Key})
	}

	page2, next2, err := b.List(context.Background(), ListOpts{Limit: 2, After: next})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2) != 2 || page2[0].Key != "b/3.txt" || page2[1].Key != "c/4.txt" {
		t.Errorf("page2 keys = %v, want [b/3.txt c/4.txt]", []string{page2[0].Key, page2[1].Key})
	}
	if next2 != "" {
		t.Errorf("expected empty next token at end, got %q", next2)
	}
}

func TestLocal_ListPrefix(t *testing.T) {
	b, root := newLocal(t)
	writeFile(t, root, "users/1.png", []byte("x"))
	writeFile(t, root, "users/2.png", []byte("x"))
	writeFile(t, root, "products/3.png", []byte("x"))

	entries, _, err := b.List(context.Background(), ListOpts{Prefix: "users/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries under users/, got %d", len(entries))
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Key, "users/") {
			t.Errorf("unexpected key under prefix: %q", e.Key)
		}
	}
}

func TestLocal_ListMissingPrefixReturnsEmpty(t *testing.T) {
	b, _ := newLocal(t)
	entries, _, err := b.List(context.Background(), ListOpts{Prefix: "does-not-exist/"})
	if err != nil {
		t.Fatalf("list with missing prefix should not error, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty result, got %d entries", len(entries))
	}
}

func TestLocal_StatGetPutDelete_Roundtrip(t *testing.T) {
	b, _ := newLocal(t)
	ctx := context.Background()

	body := []byte("hello world")
	entry, err := b.Put(ctx, "greetings/hello.txt", bytes.NewReader(body), PutMeta{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if entry.Size != int64(len(body)) {
		t.Errorf("entry.Size = %d, want %d", entry.Size, len(body))
	}
	if entry.ContentType != "text/plain" {
		t.Errorf("entry.ContentType = %q, want text/plain", entry.ContentType)
	}

	stat, err := b.Stat(ctx, "greetings/hello.txt")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.Size != int64(len(body)) {
		t.Errorf("stat.Size = %d, want %d", stat.Size, len(body))
	}

	r, _, err := b.Get(ctx, "greetings/hello.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, body) {
		t.Errorf("get body = %q, want %q", got, body)
	}

	if err := b.Delete(ctx, "greetings/hello.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := b.Stat(ctx, "greetings/hello.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, expected ErrNotFound, got %v", err)
	}
}

func TestLocal_DeleteMissingIsNoOp(t *testing.T) {
	b, _ := newLocal(t)
	if err := b.Delete(context.Background(), "never/existed.txt"); err != nil {
		t.Errorf("expected delete of missing key to be no-op, got %v", err)
	}
}

func TestLocal_StatMissingReturnsErrNotFound(t *testing.T) {
	b, _ := newLocal(t)
	_, err := b.Stat(context.Background(), "missing.bin")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLocal_RejectsKeyEscape(t *testing.T) {
	b, _ := newLocal(t)
	_, err := b.Stat(context.Background(), "../etc/passwd")
	if err == nil {
		t.Errorf("expected error on key escape, got nil")
	}
}

func TestLocal_PresignReturnsFileURL(t *testing.T) {
	b, _ := newLocal(t)
	u, err := b.Presign(context.Background(), "x.bin", PresignGet, 0)
	if err != nil {
		t.Fatal(err)
	}
	parsed, perr := url.Parse(u)
	if perr != nil {
		t.Fatalf("url parse: %v", perr)
	}
	if parsed.Scheme != "file" {
		t.Errorf("expected scheme=file, got %q", parsed.Scheme)
	}
}

func TestLocal_PresignPutUnsupported(t *testing.T) {
	b, _ := newLocal(t)
	_, err := b.Presign(context.Background(), "x.bin", PresignPut, 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for PUT presign, got %v", err)
	}
}

func TestNewLocal_RejectsMissingRoot(t *testing.T) {
	if _, err := NewLocal(LocalConfig{Root: ""}); err == nil {
		t.Errorf("expected error for empty root")
	}
	if _, err := NewLocal(LocalConfig{Root: filepath.Join(os.TempDir(), "definitely-does-not-exist-12345")}); err == nil {
		t.Errorf("expected error for missing root")
	}
}
