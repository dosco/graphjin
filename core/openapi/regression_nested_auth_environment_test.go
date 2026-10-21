package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadAndCallExpandsNestedTokenExchangeCredentials(t *testing.T) {
	t.Setenv("OPENAPI_TEST_CLIENT", "example-client")
	t.Setenv("OPENAPI_TEST_SECRET", "example-secret")
	t.Setenv("OPENAPI_TEST_AUDIENCE", "records")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			var body struct {
				Credentials struct {
					Client string `json:"client_id"`
					Secret string `json:"client_secret"`
				} `json:"credentials"`
				Audiences []string `json:"audiences"`
				Options   []struct {
					Audience string `json:"audience"`
				} `json:"options"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Credentials.Client != "example-client" || body.Credentials.Secret != "example-secret" || len(body.Audiences) != 1 || body.Audiences[0] != "records" || len(body.Options) != 1 || body.Options[0].Audience != "records" {
				http.Error(w, "credentials not expanded", 401)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"example-token","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer example-token" {
			http.Error(w, "token missing", 401)
			return
		}
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()
	original := map[string]interface{}{
		"credentials": map[string]interface{}{"client_id": "${OPENAPI_TEST_CLIENT}", "client_secret": "${OPENAPI_TEST_SECRET}"},
		"audiences":   []interface{}{"${OPENAPI_TEST_AUDIENCE}"},
		"options":     []interface{}{map[string]interface{}{"audience": "${OPENAPI_TEST_AUDIENCE}"}},
	}
	document := `openapi: 3.0.3
info: {title: Records, version: '1'}
paths:
  /record:
    get:
      operationId: getRecord
      responses:
        '200':
          description: record
          content:
            application/json:
              schema: {type: object, properties: {id: {type: integer}}}
`
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"records": {Document: document, BaseURL: srv.URL, Auth: AuthConfig{Scheme: "token_exchange", TokenURL: srv.URL + "/token", Request: &TokenExchangeRequest{Body: original}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := loaded.Registry.Get("records")
	runtime, err := NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	caller, _ := runtime.Caller("getRecord")
	if _, err := caller.Call(context.Background(), CallParams{}); err != nil {
		t.Fatal(err)
	}
	if original["credentials"].(map[string]interface{})["client_secret"] != "${OPENAPI_TEST_SECRET}" || original["audiences"].([]interface{})[0] != "${OPENAPI_TEST_AUDIENCE}" {
		t.Fatal("load mutated caller-owned credential configuration")
	}
}
