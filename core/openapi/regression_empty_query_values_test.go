package openapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallerPreservesExplicitEmptyQueryValues(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				values, exists := query["filter"]
				if !exists || len(values) != 1 || values[0] != "" {
					http.Error(w, "explicit empty filter missing", http.StatusBadRequest)
					return
				}
				if _, exists := query["omitted"]; exists {
					t.Error("omitted parameter was sent")
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()
			op := &OpDescriptor{OperationID: "search", Method: method, PathTemplate: "/search", QueryParams: []ParamSpec{{Name: "filter", In: ParamInQuery, Type: "string"}, {Name: "omitted", In: ParamInQuery, Type: "string"}}, Defaults: map[string]string{"filter": "default"}}
			var params CallParams
			var err error
			if method == http.MethodPost {
				op.Mode = OpModeMutation
				op.SuccessStatuses = []int{200}
				params, err = op.ResolveMutationCall(map[string]interface{}{"query": map[string]interface{}{"filter": ""}})
			} else {
				params, err = op.ResolveCallParams(map[string]string{"filter": ""})
			}
			if err != nil {
				t.Fatal(err)
			}
			caller := buildCaller(t, op, srv.URL, nil)
			if method == http.MethodPost {
				_, err = caller.CallMutation(context.Background(), params)
			} else {
				_, err = caller.Call(context.Background(), params)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
