package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCallerReusesShortLivedTokensAndRefreshesAfter401(t *testing.T) {
	for _, scheme := range []string{"oauth2_client_credentials", "token_exchange"} {
		t.Run(scheme, func(t *testing.T) {
			var tokens, unauthorized atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					n := tokens.Add(1)
					_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":15}`, n)
					return
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer token-") {
					t.Error("token not attached")
				}
				if unauthorized.CompareAndSwap(1, 0) {
					w.WriteHeader(401)
					return
				}
				_, _ = w.Write([]byte(`{"id":1}`))
			}))
			defer srv.Close()
			spec := &Spec{Key: "records", BaseURL: srv.URL, Auth: AuthConfig{Scheme: scheme, TokenURL: srv.URL + "/token", ClientID: "id", ClientSecret: "secret", Request: &TokenExchangeRequest{Body: map[string]interface{}{"key": "value"}}}, Operations: []OpDescriptor{{OperationID: "getRecord", Method: "GET", PathTemplate: "/record", Mode: OpModeList}}}
			runtime, err := NewSpecRuntime(spec, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			caller, _ := runtime.Caller("getRecord")
			for i := 0; i < 3; i++ {
				if _, err := caller.Call(context.Background(), CallParams{}); err != nil {
					t.Fatal(err)
				}
			}
			if got := tokens.Load(); got != 1 {
				t.Fatalf("three immediate calls fetched %d short-lived tokens, want1", got)
			}
			unauthorized.Store(1)
			if _, err := caller.Call(context.Background(), CallParams{}); err != nil {
				t.Fatal(err)
			}
			if got := tokens.Load(); got != 2 {
				t.Fatalf("401 did not refresh cached token exactly once: %d", got)
			}
		})
	}
}
