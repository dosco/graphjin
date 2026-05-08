package serv

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// buildMultipart constructs a graphql-multipart-request-spec request
// with `operations`, `map`, and one or more file parts.
func buildMultipart(t *testing.T, operations, fileMap string, files map[string]struct {
	Filename string
	CType    string
	Body     []byte
}) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("operations", operations); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("map", fileMap); err != nil {
		t.Fatal(err)
	}
	for name, f := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="`+name+`"; filename="`+f.Filename+`"`)
		if f.CType != "" {
			h.Set("Content-Type", f.CType)
		}
		fw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(f.Body); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()

	r, err := http.NewRequest("POST", "/api/v1/graphql", &buf)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestParseMultipart_SingleFile(t *testing.T) {
	pngBytes := []byte("\x89PNG\x0d\x0a\x1a\x0a fake png data")

	r := buildMultipart(t,
		`{"query":"mutation($f: JSON!){ uploadAvatar(file: $f) { id } }","variables":{"f":null}}`,
		`{"0":["variables.f"]}`,
		map[string]struct {
			Filename string
			CType    string
			Body     []byte
		}{"0": {Filename: "logo.png", CType: "image/png", Body: pngBytes}},
	)

	req, err := parseMultipartGraphQL(r, UploadsConfig{Enabled: true})
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
	if f["filename"] != "logo.png" {
		t.Errorf("filename = %v, want logo.png", f["filename"])
	}
	if f["content_type"] != "image/png" {
		t.Errorf("content_type = %v, want image/png", f["content_type"])
	}
	if f["size"] != float64(len(pngBytes)) {
		t.Errorf("size = %v, want %d", f["size"], len(pngBytes))
	}
	dec, err := base64.StdEncoding.DecodeString(f["data"].(string))
	if err != nil {
		t.Fatalf("data not base64: %v", err)
	}
	if !bytes.Equal(dec, pngBytes) {
		t.Errorf("decoded data does not match original")
	}
}

func TestParseMultipart_NestedPath(t *testing.T) {
	r := buildMultipart(t,
		`{"query":"mutation($input: JSON!){ insertProduct(input: $input) { id } }","variables":{"input":{"name":"x","avatar":null}}}`,
		`{"0":["variables.input.avatar"]}`,
		map[string]struct {
			Filename string
			CType    string
			Body     []byte
		}{"0": {Filename: "a.bin", CType: "application/octet-stream", Body: []byte("hi")}},
	)
	req, err := parseMultipartGraphQL(r, UploadsConfig{Enabled: true})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var vars map[string]any
	_ = json.Unmarshal(req.Vars, &vars)
	input := vars["input"].(map[string]any)
	avatar, ok := input["avatar"].(map[string]any)
	if !ok {
		t.Fatalf("expected variables.input.avatar to be an object, got %T", input["avatar"])
	}
	if avatar["filename"] != "a.bin" {
		t.Errorf("filename = %v, want a.bin", avatar["filename"])
	}
}

func TestParseMultipart_MissingMap(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("operations", `{"query":"mutation { x }"}`)
	mw.Close()

	r, _ := http.NewRequest("POST", "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())

	_, err := parseMultipartGraphQL(r, UploadsConfig{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "missing 'map'") {
		t.Errorf("expected missing-map error, got %v", err)
	}
}

func TestParseMultipart_MissingFile(t *testing.T) {
	r := buildMultipart(t,
		`{"query":"mutation($f: JSON!){ x(f: $f) }","variables":{"f":null}}`,
		`{"0":["variables.f"]}`,
		map[string]struct {
			Filename string
			CType    string
			Body     []byte
		}{}, // no file part for "0"
	)
	_, err := parseMultipartGraphQL(r, UploadsConfig{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "missing from the request") {
		t.Errorf("expected missing-file error, got %v", err)
	}
}

func TestParseMultipart_MIMEAllowlist(t *testing.T) {
	r := buildMultipart(t,
		`{"query":"mutation($f: JSON!){ x(f: $f) }","variables":{"f":null}}`,
		`{"0":["variables.f"]}`,
		map[string]struct {
			Filename string
			CType    string
			Body     []byte
		}{"0": {Filename: "evil.exe", CType: "application/x-msdownload", Body: []byte("\x00\x00")}},
	)
	conf := UploadsConfig{Enabled: true, AllowedMIME: []string{"image/*", "application/pdf"}}
	_, err := parseMultipartGraphQL(r, conf)
	if err == nil || !strings.Contains(err.Error(), "disallowed content-type") {
		t.Errorf("expected MIME rejection, got %v", err)
	}
}

func TestMIMEAllowed_GlobAndExact(t *testing.T) {
	allow := buildMIMEAllowlist([]string{"image/*", "application/pdf"})
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"application/pdf", true},
		{"application/json", false},
		{"text/plain", false},
		{"IMAGE/PNG; charset=binary", true},
	}
	for _, c := range cases {
		got := mimeAllowed(c.ct, allow)
		if got != c.want {
			t.Errorf("mimeAllowed(%q)=%v want %v", c.ct, got, c.want)
		}
	}
}

func TestSetAtPath_ArraySlot(t *testing.T) {
	root := map[string]any{
		"variables": map[string]any{
			"files": []any{nil, nil, nil},
		},
	}
	if err := setAtPath(root, "variables.files.1", "X"); err != nil {
		t.Fatal(err)
	}
	got := root["variables"].(map[string]any)["files"].([]any)
	if got[1] != "X" {
		t.Errorf("got %v, want index 1 = X", got)
	}
}

func TestIsMultipartRequest(t *testing.T) {
	r1, _ := http.NewRequest("POST", "/", nil)
	r1.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	if !isMultipartRequest(r1) {
		t.Errorf("expected true for multipart")
	}
	r2, _ := http.NewRequest("POST", "/", nil)
	r2.Header.Set("Content-Type", "application/json")
	if isMultipartRequest(r2) {
		t.Errorf("expected false for json")
	}
}
