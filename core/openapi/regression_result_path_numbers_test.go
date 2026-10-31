package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallerResultPathPreservesExactNumbers(t *testing.T) {
	for _, number := range []string{"9007199254740993", "18446744073709551615", "0.12345678901234567890123456789", "1e1000"} {
		t.Run(number, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"id":` + number + `}}`))
			}))
			defer srv.Close()
			op := &OpDescriptor{OperationID: "getRecord", Method: "GET", PathTemplate: "/record", ResultPath: []string{"data"}}
			caller := buildCaller(t, op, srv.URL, nil)
			body, err := caller.Call(context.Background(), CallParams{})
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			if string(got["id"]) != number {
				t.Fatalf("upstream number changed: got %s, want %s", got["id"], number)
			}
		})
	}
}

func TestCallerResultPathRejectsMalformedJSON(t *testing.T) {
	for _, body := range []string{`{"data":{"id":1}} {"extra":2}`, `{"data":{"id":1}`, `{"data":null}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			op := &OpDescriptor{OperationID: "getRecord", Method: "GET", PathTemplate: "/record", ResultPath: []string{"data", "id"}}
			caller := buildCaller(t, op, srv.URL, nil)
			if _, err := caller.Call(context.Background(), CallParams{}); err == nil {
				t.Fatal("expected malformed or non-object result path to be rejected")
			}
		})
	}
}
