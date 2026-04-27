package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// When auth_login is disabled, the device endpoint isn't registered. Some
// servers serve the SPA index.html via a catch-all fallback, returning
// HTTP 200 with text/html. startDeviceFlow must treat that as "no built-in
// login" (hasAuth=false, err=nil), not as a JSON decode error.
func TestStartDeviceFlow_SPAFallbackHTMLAsNoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>SPA</body></html>"))
	}))
	defer srv.Close()

	ds, hasAuth, err := startDeviceFlow(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if hasAuth {
		t.Fatalf("expected hasAuth=false for HTML fallback, got true")
	}
	if ds != nil {
		t.Fatalf("expected nil deviceStartResp, got %+v", ds)
	}
}

func TestStartDeviceFlow_404AsNoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ds, hasAuth, err := startDeviceFlow(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if hasAuth {
		t.Fatalf("expected hasAuth=false for 404, got true")
	}
	if ds != nil {
		t.Fatalf("expected nil deviceStartResp, got %+v", ds)
	}
}

func TestStartDeviceFlow_JSONOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_uri":"http://x/v","interval":5,"expires_in":600}`))
	}))
	defer srv.Close()

	ds, hasAuth, err := startDeviceFlow(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if !hasAuth {
		t.Fatalf("expected hasAuth=true for JSON 200, got false")
	}
	if ds == nil || ds.DeviceCode != "dc" || ds.UserCode != "UC" {
		t.Fatalf("unexpected deviceStartResp: %+v", ds)
	}
}

func TestIsJSONResponse(t *testing.T) {
	cases := []struct {
		name string
		ct   string
		body string
		want bool
	}{
		{"json content-type", "application/json", "{}", true},
		{"json with charset", "application/json; charset=utf-8", "{}", true},
		{"html content-type", "text/html; charset=utf-8", "<html></html>", false},
		{"empty content-type, json body", "", "  {\"a\":1}", true},
		{"empty content-type, array body", "", "[1,2]", true},
		{"empty content-type, html body", "", "<!DOCTYPE html>", false},
		{"empty content-type, empty body", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tc.ct != "" {
				resp.Header.Set("Content-Type", tc.ct)
			}
			if got := isJSONResponse(resp, []byte(tc.body)); got != tc.want {
				t.Fatalf("isJSONResponse(%q, %q) = %v, want %v", tc.ct, tc.body, got, tc.want)
			}
		})
	}
}
