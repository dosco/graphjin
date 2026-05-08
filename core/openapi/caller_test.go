package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// buildCaller wires up the minimal pieces needed to exercise a Caller
// against an httptest server. The default limiter values are used so
// concurrency ceilings don't influence test outcomes.
func buildCaller(t *testing.T, op *OpDescriptor, baseURL string, auth AuthProvider) *Caller {
	t.Helper()
	if auth == nil {
		auth = noopAuth{}
	}
	return NewCaller(op, baseURL, auth, newLimiter(ConcurrencyConfig{}), http.DefaultClient)
}

func TestCallerRowJoinPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI carries the escaped wire form; r.URL.Path is the
		// decoded version. Asserting on RequestURI ensures the client
		// actually escaped the space rather than relying on the server
		// to be lenient.
		if r.RequestURI != "/users/abc%20123" {
			t.Errorf("RequestURI = %q, want /users/abc%%20123", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc 123","email":"u@x"}`))
	}))
	defer srv.Close()

	op := &OpDescriptor{
		OperationID:  "getUserById",
		Method:       "GET",
		PathTemplate: "/users/{userId}",
		PathParams:   []ParamSpec{{Name: "userId", In: ParamInPath, Required: true, Type: "string"}},
	}
	c := buildCaller(t, op, srv.URL, nil)

	body, err := c.Call(context.Background(), CallParams{
		PathValues: map[string]string{"userId": "abc 123"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(string(body), `"abc 123"`) {
		t.Errorf("body = %s", body)
	}
}

func TestCallerListWithQueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("actorId") != "u-7" {
			t.Errorf("actorId = %q", r.URL.Query().Get("actorId"))
		}
		if r.URL.Query().Get("limit") != "50" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":1},{"id":2}]}`))
	}))
	defer srv.Close()

	op := &OpDescriptor{
		OperationID:  "listAuditLogs",
		Method:       "GET",
		PathTemplate: "/audit-logs",
		QueryParams: []ParamSpec{
			{Name: "actorId", In: ParamInQuery},
			{Name: "limit", In: ParamInQuery},
		},
		ResultPath:      []string{"data"},
		IsArrayResponse: true,
	}
	c := buildCaller(t, op, srv.URL, nil)

	body, err := c.Call(context.Background(), CallParams{
		QueryValues: map[string]string{"actorId": "u-7", "limit": "50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// ResultPath strips the {data: [...]} wrapper.
	if string(body) != `[{"id":1},{"id":2}]` {
		t.Errorf("body = %s", body)
	}
}

func TestCallerRetryOn401(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	auth := &recordingAuth{}
	op := &OpDescriptor{OperationID: "x", Method: "GET", PathTemplate: "/x"}
	c := buildCaller(t, op, srv.URL, auth)

	body, err := c.Call(context.Background(), CallParams{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s", body)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("calls = %d, want 2 (1 401 + 1 retry)", calls)
	}
	if !auth.invalidated {
		t.Error("auth provider not invalidated after 401")
	}
}

func TestCallerNonRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"oops"}`))
	}))
	defer srv.Close()

	op := &OpDescriptor{OperationID: "x", Method: "GET", PathTemplate: "/x"}
	c := buildCaller(t, op, srv.URL, nil)

	_, err := c.Call(context.Background(), CallParams{})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !contains(err.Error(), "500") {
		t.Errorf("err = %v, expected mention of status code", err)
	}
}

func TestCallerMissingRequiredPathParam(t *testing.T) {
	op := &OpDescriptor{
		OperationID:  "getUserById",
		Method:       "GET",
		PathTemplate: "/users/{userId}",
		PathParams:   []ParamSpec{{Name: "userId", In: ParamInPath, Required: true}},
	}
	c := buildCaller(t, op, "https://x", nil)

	_, err := c.Call(context.Background(), CallParams{})
	if err == nil {
		t.Fatal("expected error on missing required path param")
	}
}

func TestStripResultPath(t *testing.T) {
	cases := []struct {
		body, want string
		path       []string
	}{
		{`{"data":[1,2,3]}`, `[1,2,3]`, []string{"data"}},
		{`{"a":{"b":{"c":42}}}`, `42`, []string{"a", "b", "c"}},
		{`[1,2]`, `[1,2]`, nil},
	}
	for _, tc := range cases {
		got, err := stripResultPath([]byte(tc.body), tc.path)
		if err != nil {
			t.Errorf("stripResultPath(%q, %v): %v", tc.body, tc.path, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("stripResultPath(%q, %v) = %s, want %s", tc.body, tc.path, got, tc.want)
		}
	}
}

// recordingAuth tracks invalidate() invocations so tests can assert
// that the 401 retry path does its bookkeeping.
type recordingAuth struct {
	invalidated bool
}

func (r *recordingAuth) Apply(_ context.Context, _ *http.Request, _ http.Header) error { return nil }
func (r *recordingAuth) OnUnauthorized(_ context.Context) error {
	r.invalidated = true
	return nil
}

// pin json import — used implicitly via httptest payloads in some tests
// elsewhere in the file; kept here so future additions don't get a
// surprise unused-import error.
var _ = json.Marshal
