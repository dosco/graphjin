package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/fstable"
)

// TestParseMultipart_StreamsToBackend verifies that when a backend is
// supplied, the file body lands on disk (via the local backend) and
// the GraphQL variable becomes a storageRef instead of base64 data.
func TestParseMultipart_StreamsToBackend(t *testing.T) {
	root := t.TempDir()
	backend, err := fstable.NewLocal(fstable.LocalConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	body := []byte("\x89PNG\x0d\x0a\x1a\x0a streamed payload")
	r := buildMultipart(t,
		`{"query":"mutation($f: JSON!){ insert(file: $f){id} }","variables":{"f":null}}`,
		`{"0":["variables.f"]}`,
		map[string]struct {
			Filename string
			CType    string
			Body     []byte
		}{"0": {Filename: "logo.png", CType: "image/png", Body: body}},
	)

	conf := UploadsConfig{
		Enabled:          true,
		Storage:          "uploads",
		StorageKeyPrefix: "{date}/avatars/",
	}
	req, err := parseMultipartGraphQL(r, conf, backend)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	var vars map[string]any
	if err := json.Unmarshal(req.Vars, &vars); err != nil {
		t.Fatalf("vars not JSON: %v", err)
	}
	f, ok := vars["f"].(map[string]any)
	if !ok {
		t.Fatalf("expected vars.f to be a JSON object, got %T", vars["f"])
	}

	for _, k := range []string{"key", "content_type", "size", "url", "modified_at"} {
		if _, ok := f[k]; !ok {
			t.Errorf("storageRef missing field %q (have %v)", k, f)
		}
	}
	if _, hasData := f["data"]; hasData {
		t.Errorf("storageRef should not contain a 'data' field, got %v", f["data"])
	}

	key := f["key"].(string)
	if !strings.HasSuffix(key, ".png") {
		t.Errorf("expected key to preserve extension, got %q", key)
	}
	if !strings.Contains(key, "/avatars/") {
		t.Errorf("expected key to honour prefix template, got %q", key)
	}

	// Verify the body actually landed on disk.
	full := filepath.Join(root, filepath.FromSlash(key))
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("file not written to disk: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("on-disk body mismatch: got %q, want %q", got, body)
	}

	// Verify metadata via the backend matches what was injected.
	stat, err := backend.Stat(context.Background(), key)
	if err != nil {
		t.Fatalf("backend.Stat: %v", err)
	}
	if stat.Size != int64(len(body)) {
		t.Errorf("stat.Size = %d, want %d", stat.Size, len(body))
	}
}

// TestStreamToBackend_UnitLevel exercises the helper directly.
func TestStreamToBackend_UnitLevel(t *testing.T) {
	root := t.TempDir()
	backend, _ := fstable.NewLocal(fstable.LocalConfig{Root: root})

	ref, err := streamToBackend(
		context.Background(),
		backend,
		bytes.NewReader([]byte("hello")),
		"original.bin",
		"application/octet-stream",
		"prefix/",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.Key, "prefix/") {
		t.Errorf("key prefix not honoured: %q", ref.Key)
	}
	if !strings.HasSuffix(ref.Key, ".bin") {
		t.Errorf("extension not preserved: %q", ref.Key)
	}
	if ref.Size != 5 {
		t.Errorf("size = %d, want 5", ref.Size)
	}
	if ref.URL == "" {
		t.Errorf("expected non-empty url")
	}
}

func TestGenerateUploadKey_DateMarker(t *testing.T) {
	k := generateUploadKey("avatars/{date}/", "x.png")
	// Format: avatars/YYYY/MM/DD/<hex>.png — assert structure, not the
	// concrete date (test would flake at midnight UTC otherwise).
	if !strings.HasPrefix(k, "avatars/") {
		t.Errorf("missing prefix: %q", k)
	}
	if !strings.HasSuffix(k, ".png") {
		t.Errorf("missing extension: %q", k)
	}
	parts := strings.Split(k, "/")
	if len(parts) != 5 {
		t.Errorf("expected 5 path components after {date} expansion, got %v", parts)
	}
}

