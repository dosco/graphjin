package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// textSpec builds a one-operation spec whose 200 response is declared under
// the given media types, each with the given schema (YAML flow syntax).
func textSpec(path string, mediaTypes []string, schema string) string {
	var content strings.Builder
	for _, mt := range mediaTypes {
		fmt.Fprintf(&content, "            %s:\n              schema: %s\n", mt, schema)
	}
	return fmt.Sprintf(`openapi: 3.0.3
info: {title: Text, version: '1'}
paths:
  %s:
    get:
      operationId: getText
      parameters: []
      responses:
        '200':
          description: text
          content:
%s`, path, content.String())
}

func classifyText(t *testing.T, src string, cfg SpecConfig) OpDescriptor {
	t.Helper()
	ops, _ := classifyAll(&Spec{Key: "shop"}, loadDoc(t, src), cfg)
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	return ops[0]
}

func TestClassifyTextScalarResponsesAreActive(t *testing.T) {
	for _, mt := range []string{"text/plain", "text/html", "text/html; charset=utf-8"} {
		t.Run(mt, func(t *testing.T) {
			op := classifyText(t, textSpec("/magento_version", []string{mt}, "{type: string}"), SpecConfig{})
			if op.Mode != OpModeList {
				t.Fatalf("mode = %v (skip: %q), want List", op.Mode, op.SkipReason)
			}
			if !op.TextResponse {
				t.Fatal("TextResponse not set")
			}
			if len(op.ResponseMediaTypes) != 1 || op.ResponseMediaTypes[0] != mt {
				t.Fatalf("ResponseMediaTypes = %v", op.ResponseMediaTypes)
			}
			if op.IsArrayResponse || len(op.ResultPath) != 0 {
				t.Fatalf("text response must not be a list or carry a result path: %+v", op)
			}
			if op.ExposeAs != "shop_get_text" {
				t.Fatalf("ExposeAs = %q", op.ExposeAs)
			}
			cols := SynthesiseColumns("public", op.ExposeAs, op.ResponseSchema, op.ResultPath)
			if len(cols) != 1 || cols[0].Name != "value" || cols[0].Type != "text" || !cols[0].NotNull {
				t.Fatalf("columns = %+v, want one non-null text column named value", cols)
			}
		})
	}
}

func TestClassifyTextScalarAcceptsBothDeclaredTypesAndPathParams(t *testing.T) {
	op := classifyText(t, `openapi: 3.0.3
info: {title: Text, version: '1'}
paths:
  /notes/{noteId}:
    get:
      operationId: getNote
      parameters:
        - {name: noteId, in: path, required: true, schema: {type: string}}
      responses:
        '200':
          description: note
          content:
            text/plain: {}
            text/html: {schema: {type: string}}
`, SpecConfig{})
	if op.Mode != OpModeSingleByID || !op.TextResponse {
		t.Fatalf("mode = %v text=%v skip=%q", op.Mode, op.TextResponse, op.SkipReason)
	}
	if strings.Join(op.ResponseMediaTypes, ",") != "text/html,text/plain" {
		t.Fatalf("ResponseMediaTypes = %v", op.ResponseMediaTypes)
	}
}

func TestClassifyTextResponsesThatAreNotScalarsStaySkipped(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		cfg      SpecConfig
		wantSkip string
	}{
		{"object schema", textSpec("/v", []string{"text/plain"}, "{type: object}"), SpecConfig{}, "must be type: string"},
		{"binary string", textSpec("/v", []string{"text/plain"}, "{type: string, format: binary}"), SpecConfig{}, "must be type: string"},
		{"undeclared text type", textSpec("/v", []string{"text/csv"}, "{type: string}"), SpecConfig{}, "non-JSON response (text/csv)"},
		{"binary media type", textSpec("/v", []string{"application/octet-stream"}, "{type: string}"), SpecConfig{}, "non-JSON response"},
		{
			"result_path override",
			textSpec("/v", []string{"text/plain"}, "{type: string}"),
			SpecConfig{Operations: map[string]OperationOverride{"getText": {ResultPath: "data"}}},
			"result_path cannot apply to a text response",
		},
		{
			"disabled",
			textSpec("/v", []string{"text/plain"}, "{type: string}"),
			SpecConfig{Operations: map[string]OperationOverride{"getText": {Disabled: true}}},
			"disabled by config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := classifyText(t, tc.src, tc.cfg)
			if op.Mode != OpModeSkipped || op.TextResponse {
				t.Fatalf("mode = %v text=%v, want skipped", op.Mode, op.TextResponse)
			}
			if !strings.Contains(op.SkipReason, tc.wantSkip) {
				t.Fatalf("SkipReason = %q, want %q", op.SkipReason, tc.wantSkip)
			}
		})
	}
}

// A JSON representation always wins, so object and array operations that
// also happen to declare a text alternative classify exactly as before.
func TestClassifyJSONStillWinsOverDeclaredText(t *testing.T) {
	op := classifyText(t, `openapi: 3.0.3
info: {title: Text, version: '1'}
paths:
  /records:
    get:
      operationId: getText
      responses:
        '200':
          description: records
          content:
            text/plain: {schema: {type: string}}
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: integer}}}}
`, SpecConfig{})
	if op.TextResponse || op.Mode != OpModeList || !op.IsArrayResponse {
		t.Fatalf("JSON array operation changed: text=%v mode=%v array=%v", op.TextResponse, op.Mode, op.IsArrayResponse)
	}
	if strings.Join(op.ResponseMediaTypes, ",") != "application/json" {
		t.Fatalf("ResponseMediaTypes = %v", op.ResponseMediaTypes)
	}
	cols := SynthesiseColumns("public", op.ExposeAs, op.ResponseSchema, op.ResultPath)
	if len(cols) != 1 || cols[0].Name != "id" {
		t.Fatalf("columns = %+v", cols)
	}
}

// Mutations keep their JSON-only success contract: a write whose response
// is text is still refused, and is never marked as a text response.
func TestClassifyMutationWithTextResponseStaysSkipped(t *testing.T) {
	ops, _ := classifyAll(&Spec{Key: "shop"}, loadDoc(t, `openapi: 3.0.3
info: {title: Text, version: '1'}
paths:
  /cache/flush:
    post:
      operationId: flushCache
      responses:
        '200':
          description: flushed
          content:
            text/plain: {schema: {type: string}}
`), SpecConfig{Operations: map[string]OperationOverride{"flushCache": {ExposeMutation: true}}})
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	op := ops[0]
	if op.Mode != OpModeSkipped || op.TextResponse || !strings.Contains(op.SkipReason, "must declare a JSON schema") {
		t.Fatalf("mutation classification changed: mode=%v text=%v skip=%q", op.Mode, op.TextResponse, op.SkipReason)
	}
}

// loadTextRuntime loads the Magento-style version operation against base and
// returns its descriptor and caller, built the way the engine builds them.
func loadTextRuntime(t *testing.T, base string, client *http.Client, cfg SpecConfig) (*OpDescriptor, *Caller) {
	t.Helper()
	cfg.Document = textSpec("/magento_version", []string{"text/html"}, "{type: string}")
	cfg.BaseURL = base
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"magento": cfg}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := loaded.Registry.Get("magento")
	if !ok || len(spec.Operations) != 1 {
		t.Fatalf("spec not loaded: %+v", loaded)
	}
	runtime, err := NewSpecRuntime(spec, client)
	if err != nil {
		t.Fatal(err)
	}
	caller, ok := runtime.Caller("getText")
	if !ok {
		t.Fatalf("operation skipped: %s", spec.Operations[0].SkipReason)
	}
	return &spec.Operations[0], caller
}

func decodeTextValue(t *testing.T, body []byte) string {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if len(out) != 1 {
		t.Fatalf("body %s must carry only value", body)
	}
	var value string
	if err := json.Unmarshal(out["value"], &value); err != nil {
		t.Fatalf("value in %s: %v", body, err)
	}
	return value
}

func TestTextResponseReturnsTheExactBody(t *testing.T) {
	cases := []struct {
		name, contentType, body string
	}{
		{"magento html", "text/html; charset=UTF-8", "Magento/2.4 (Community)"},
		{"html is data", "text/html", "<script>alert(\"x\")</script>\n&amp; \u00e9 \\ trailing  "},
		{"no content type", "", "plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "text/html" {
					t.Errorf("Accept = %q, want the declared text media type", r.Header.Get("Accept"))
				}
				w.Header()["Content-Type"] = []string{tc.contentType}
				if tc.contentType == "" {
					w.Header()["Content-Type"] = nil
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, caller := loadTextRuntime(t, srv.URL, srv.Client(), SpecConfig{})
			body, err := caller.Call(context.Background(), CallParams{})
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeTextValue(t, body); got != tc.body {
				t.Fatalf("value = %q, want %q", got, tc.body)
			}
		})
	}
}

func TestTextResponseWithPlainTextMediaType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("2.4.7-p3"))
	}))
	defer srv.Close()
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"shop": {
		Document: textSpec("/version", []string{"text/plain"}, "{type: string}"),
		BaseURL:  srv.URL,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := loaded.Registry.Get("shop")
	runtime, err := NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	caller, ok := runtime.Caller("getText")
	if !ok {
		t.Fatalf("skipped: %s", spec.Operations[0].SkipReason)
	}
	body, err := caller.Call(context.Background(), CallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"value":"2.4.7-p3"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestTextResponseFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		cfg     SpecConfig
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name:    "empty body",
			handler: func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/html") },
			wantErr: "empty text response",
		},
		{
			name: "oversized body",
			cfg:  SpecConfig{MaxResponseBytes: 8},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("Magento/2.4 (Community)"))
			},
			wantErr: "exceeds max_response_bytes",
		},
		{
			name: "redirect to a login page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<form>sign in</form>"))
					return
				}
				http.Redirect(w, r, "/login", http.StatusFound)
			},
			wantErr: "returned 302",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("maintenance"))
			},
			wantErr: "returned 503",
		},
		{
			name: "undeclared content type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"message":"not text"}`))
			},
			wantErr: `Content-Type "application/json"`,
		},
		{
			name: "invalid utf-8",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte{0xff, 0xfe, 'x'})
			},
			wantErr: "not valid UTF-8",
		},
		{
			name: "timeout",
			cfg:  SpecConfig{Timeout: 50 * time.Millisecond},
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			},
			wantErr: "timed out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			_, caller := loadTextRuntime(t, srv.URL, srv.Client(), tc.cfg)
			body, err := caller.Call(context.Background(), CallParams{})
			if err == nil {
				t.Fatalf("want error, got body %s", body)
			}
			if body != nil {
				t.Fatalf("a failed call must not return a value: %s", body)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// A 401 invalidates credentials and retries exactly once, as for JSON reads;
// a second 401 is an error, never an HTML login page returned as a value.
func TestTextResponseAuthRetry(t *testing.T) {
	for _, recovers := range []bool{true, false} {
		t.Run(fmt.Sprintf("recovers=%v", recovers), func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if atomic.AddInt32(&calls, 1) == 1 || !recovers {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte("<h1>log in</h1>"))
					return
				}
				_, _ = w.Write([]byte("Magento/2.4 (Community)"))
			}))
			defer srv.Close()
			op, _ := loadTextRuntime(t, srv.URL, srv.Client(), SpecConfig{})
			auth := &recordingAuth{}
			caller := NewCaller(op, srv.URL, auth, newLimiter(ConcurrencyConfig{}), srv.Client())
			body, err := caller.Call(context.Background(), CallParams{})
			if !auth.invalidated {
				t.Fatal("401 did not invalidate credentials")
			}
			if got := atomic.LoadInt32(&calls); got != 2 {
				t.Fatalf("upstream calls = %d, want exactly one retry", got)
			}
			if recovers {
				if err != nil {
					t.Fatal(err)
				}
				if got := decodeTextValue(t, body); got != "Magento/2.4 (Community)" {
					t.Fatalf("value = %q", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "returned 401") || body != nil {
				t.Fatalf("second 401 must fail closed: body=%s err=%v", body, err)
			}
		})
	}
}

// JSON reads do not change shape: the body comes back untouched, never
// wrapped, and redirects are still followed as they were before.
func TestJSONReadsAreNotWrappedAndStillFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/records" {
			http.Redirect(w, r, "/records/v2", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer srv.Close()
	op := &OpDescriptor{OperationID: "listRecords", Method: "GET", PathTemplate: "/records", Mode: OpModeList, IsArrayResponse: true}
	body, err := buildCaller(t, op, srv.URL, nil).Call(context.Background(), CallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `[{"id":1}]` {
		t.Fatalf("body = %s", body)
	}
}
