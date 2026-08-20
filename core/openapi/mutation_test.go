package openapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const mutationContractSpec = `
openapi: 3.0.0
info: { title: Mutation contract, version: '1' }
paths:
  /widgets/{id}:
    parameters:
      - { name: id, in: path, required: true, schema: { type: integer } }
    post:
      operationId: createWidget
      parameters:
        - { name: dry_run, in: query, required: false, schema: { type: boolean } }
        - { name: mode, in: query, required: false, schema: { type: string, enum: [safe, force] } }
        - { name: X-Trace, in: header, required: true, schema: { type: string } }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              additionalProperties: false
              properties:
                name: { type: string }
                enabled: { type: boolean }
      responses:
        '201':
          description: created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: integer }
                  name: { type: string }
        '202':
          description: accepted
          content:
            application/json:
              schema: { type: object }
`

func mutationContractOperation(t *testing.T, override OperationOverride) OpDescriptor {
	t.Helper()
	spec := &Spec{Key: "contract", SourceName: "external_api", MaxRequestBytes: 128, MaxResponseBytes: 256}
	ops, _ := classifyAll(spec, loadDoc(t, mutationContractSpec), SpecConfig{Operations: map[string]OperationOverride{"createWidget": override}})
	if len(ops) != 1 {
		t.Fatalf("operations = %d, want 1", len(ops))
	}
	return ops[0]
}

func TestClassifyMutationRequiresExplicitExposure(t *testing.T) {
	spec := &Spec{Key: "contract", SourceName: "external_api"}
	doc := loadDoc(t, mutationContractSpec)
	absent, _ := classifyAll(spec, doc, SpecConfig{})
	if len(absent) != 1 || absent[0].Mode != OpModeSkipped || !strings.Contains(absent[0].SkipReason, "expose_mutation") {
		t.Fatalf("absent override unexpectedly exposed mutation: %+v", absent)
	}

	skipped := mutationContractOperation(t, OperationOverride{})
	if skipped.Mode != OpModeSkipped || !strings.Contains(skipped.SkipReason, "expose_mutation") {
		t.Fatalf("unexpected skipped operation: %+v", skipped)
	}

	active := mutationContractOperation(t, OperationOverride{
		ExposeMutation: true,
		ExposeAs:       "create_widget",
		AllowedRoles:   []string{"operator", "OPERATOR", "admin"},
	})
	if active.Mode != OpModeMutation || active.SourceName != "external_api" || active.ExposeAs != "create_widget" {
		t.Fatalf("unexpected active operation: %+v", active)
	}
	if active.RequestMediaType != "application/json" || !active.RequestBodyRequired || len(active.SuccessStatuses) != 2 {
		t.Fatalf("mutation contract not retained: %+v", active)
	}
	if got := strings.Join(active.AllowedRoles, ","); got != "admin,operator" {
		t.Fatalf("allowed roles = %q", got)
	}
	if active.RetryOnAuthFailure {
		t.Fatal("mutation auth retry must default off")
	}

	disabled := mutationContractOperation(t, OperationOverride{ExposeMutation: true, Disabled: true, AllowedRoles: []string{"operator"}})
	if disabled.Mode != OpModeSkipped || !strings.Contains(disabled.SkipReason, "disabled") {
		t.Fatalf("disabled mutation = %+v", disabled)
	}
}

func TestClassifyMutationRejectsUnsupportedBody(t *testing.T) {
	const specYAML = `
openapi: 3.0.0
info: { title: Unsupported mutation, version: '1' }
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema: { type: object }
      responses:
        '201':
          description: created
          content:
            application/json:
              schema: { type: object }
`
	spec := &Spec{Key: "unsupported", SourceName: "external_api"}
	ops, _ := classifyAll(spec, loadDoc(t, specYAML), SpecConfig{Operations: map[string]OperationOverride{
		"upload": {ExposeMutation: true, AllowedRoles: []string{"operator"}},
	}})
	if len(ops) != 1 || ops[0].Mode != OpModeSkipped || !strings.Contains(ops[0].SkipReason, "unsupported request body") {
		t.Fatalf("unsupported mutation body = %+v", ops)
	}
}

func TestClassifyMutationRejectsAmbiguousJSONBody(t *testing.T) {
	const specYAML = `
openapi: 3.0.0
info: { title: Ambiguous mutation, version: '1' }
paths:
  /widgets:
    post:
      operationId: createWidget
      requestBody:
        required: true
        content:
          application/vnd.first+json:
            schema: { type: object }
          application/vnd.second+json:
            schema: { type: object }
      responses:
        '204': { description: created }
`
	spec := &Spec{Key: "ambiguous", SourceName: "external_api"}
	ops, _ := classifyAll(spec, loadDoc(t, specYAML), SpecConfig{Operations: map[string]OperationOverride{
		"createWidget": {ExposeMutation: true, AllowedRoles: []string{"operator"}},
	}})
	if len(ops) != 1 || ops[0].Mode != OpModeSkipped || !strings.Contains(ops[0].SkipReason, "ambiguous JSON request body") {
		t.Fatalf("ambiguous mutation body = %+v", ops)
	}
}

func TestClassifyMutationRejectsLooselyNamedJSONResponse(t *testing.T) {
	const specYAML = `
openapi: 3.0.0
info: { title: Streaming mutation, version: '1' }
paths:
  /widgets:
    post:
      operationId: createWidget
      responses:
        '201':
          description: stream
          content:
            application/x-json-stream:
              schema: { type: object }
`
	spec := &Spec{Key: "streaming", SourceName: "external_api"}
	ops, _ := classifyAll(spec, loadDoc(t, specYAML), SpecConfig{Operations: map[string]OperationOverride{
		"createWidget": {ExposeMutation: true, AllowedRoles: []string{"operator"}},
	}})
	if len(ops) != 1 || ops[0].Mode != OpModeSkipped || !strings.Contains(ops[0].SkipReason, "must declare a JSON schema") {
		t.Fatalf("streaming mutation response = %+v", ops)
	}
}

func TestResolveMutationCallValidatesEnvelopeAndSchema(t *testing.T) {
	op := mutationContractOperation(t, OperationOverride{ExposeMutation: true, AllowedRoles: []string{"operator"}})
	valid := map[string]interface{}{
		"path":    map[string]interface{}{"id": int64(7)},
		"query":   map[string]interface{}{"dry_run": true, "mode": "safe"},
		"headers": map[string]interface{}{"x-trace": "trace-1"},
		"body":    map[string]interface{}{"name": "new", "enabled": true},
	}
	p, err := op.ResolveMutationCall(valid)
	if err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if p.PathValues["id"] != "7" || p.QueryValues["dry_run"] != "true" || p.QueryValues["mode"] != "safe" || p.HeaderValues["X-Trace"] != "trace-1" {
		t.Fatalf("resolved params = %+v", p)
	}
	if string(p.BodyJSON) != `{"enabled":true,"name":"new"}` {
		t.Fatalf("body = %s", p.BodyJSON)
	}

	cases := []struct {
		name string
		call map[string]interface{}
		want string
	}{
		{"unknown envelope field", map[string]interface{}{"url": "https://evil"}, "unknown call field"},
		{"unknown parameter", map[string]interface{}{"path": map[string]interface{}{"id": 1, "extra": "x"}}, "undeclared path"},
		{"missing required parameter", map[string]interface{}{"body": map[string]interface{}{"name": "x"}}, "required path"},
		{"non-object params", map[string]interface{}{"path": []interface{}{1}}, "call.path must be an object"},
		{"parameter schema constraint", map[string]interface{}{"path": map[string]interface{}{"id": 1}, "query": map[string]interface{}{"mode": "unsafe"}, "headers": map[string]interface{}{"X-Trace": "x"}, "body": map[string]interface{}{"name": "valid"}}, "schema validation failed"},
		{"invalid nested body", map[string]interface{}{"path": map[string]interface{}{"id": 1}, "headers": map[string]interface{}{"X-Trace": "x"}, "body": map[string]interface{}{"enabled": true}}, "invalid request body"},
		{"unknown body property", map[string]interface{}{"path": map[string]interface{}{"id": 1}, "headers": map[string]interface{}{"X-Trace": "x"}, "body": map[string]interface{}{"name": "x", "extra": true}}, "invalid request body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := op.ResolveMutationCall(tc.call)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveMutationCallAppliesCanonicalOperationDefaults(t *testing.T) {
	op := mutationContractOperation(t, OperationOverride{
		ExposeMutation: true,
		AllowedRoles:   []string{"operator"},
		Defaults: map[string]string{
			"ID":      "7",
			"DRY_RUN": "true",
		},
	})
	p, err := op.ResolveMutationCall(map[string]interface{}{
		"headers": map[string]interface{}{"X-Trace": "trace-1"},
		"body":    map[string]interface{}{"name": "new"},
	})
	if err != nil {
		t.Fatalf("resolve mutation defaults: %v", err)
	}
	if p.PathValues["id"] != "7" || p.QueryValues["dry_run"] != "true" {
		t.Fatalf("resolved defaults = path:%+v query:%+v", p.PathValues, p.QueryValues)
	}
	if _, ok := op.Defaults["ID"]; ok {
		t.Fatalf("mutation defaults were not canonicalized: %+v", op.Defaults)
	}
}

func TestResolveMutationCallRedactsInvalidSchemaValues(t *testing.T) {
	op := mutationContractOperation(t, OperationOverride{ExposeMutation: true, AllowedRoles: []string{"operator"}})
	_, paramErr := op.ResolveMutationCall(map[string]interface{}{
		"path":    map[string]interface{}{"id": 1},
		"query":   map[string]interface{}{"mode": "parameter-secret-must-not-leak"},
		"headers": map[string]interface{}{"X-Trace": "trace"},
		"body":    map[string]interface{}{"name": "valid"},
	})
	if paramErr == nil || !strings.Contains(paramErr.Error(), "schema validation failed") {
		t.Fatalf("expected parameter schema validation error, got %v", paramErr)
	}
	if strings.Contains(paramErr.Error(), "parameter-secret-must-not-leak") {
		t.Fatalf("parameter schema error leaked request value: %v", paramErr)
	}

	_, bodyErr := op.ResolveMutationCall(map[string]interface{}{
		"path":    map[string]interface{}{"id": 1},
		"headers": map[string]interface{}{"X-Trace": "trace"},
		"body": map[string]interface{}{
			"name": "valid", "body-secret-must-not-leak": true,
		},
	})
	if bodyErr == nil || !strings.Contains(bodyErr.Error(), "schema validation failed") {
		t.Fatalf("expected body schema validation error, got %v", bodyErr)
	}
	if strings.Contains(bodyErr.Error(), "body-secret-must-not-leak") {
		t.Fatalf("body schema error leaked request value: %v", bodyErr)
	}
}

func TestResolveMutationCallValidatesOpenAPI31Schema(t *testing.T) {
	const specYAML = `
openapi: 3.1.0
info: { title: OpenAPI 3.1 mutation, version: '1' }
paths:
  /cards:
    post:
      operationId: storeCard
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: [object, "null"]
              properties:
                credit_card: { type: string }
                billing_address: { type: string }
              dependentRequired:
                credit_card: [billing_address]
      responses:
        '204': { description: stored }
`
	doc := loadDoc(t, specYAML)
	spec := &Spec{Key: "oas31", SourceName: "external_api", Doc: doc}
	ops, _ := classifyAll(spec, doc, SpecConfig{Operations: map[string]OperationOverride{
		"storeCard": {ExposeMutation: true, AllowedRoles: []string{"operator"}},
	}})
	if len(ops) != 1 || ops[0].Mode != OpModeMutation || !ops[0].JSONSchema2020 {
		t.Fatalf("OpenAPI 3.1 operation = %+v", ops)
	}
	if _, err := ops[0].ResolveMutationCall(map[string]interface{}{
		"body": map[string]interface{}{"credit_card": "4111111111111111", "billing_address": "safe"},
	}); err != nil {
		t.Fatalf("valid OpenAPI 3.1 body: %v", err)
	}
	nullBody, err := ops[0].ResolveMutationCall(map[string]interface{}{"body": nil})
	if err != nil || string(nullBody.BodyJSON) != "null" {
		t.Fatalf("valid explicit null OpenAPI 3.1 body: params=%+v err=%v", nullBody, err)
	}
	_, err = ops[0].ResolveMutationCall(map[string]interface{}{
		"body": map[string]interface{}{"credit_card": "card-secret-must-not-leak"},
	})
	if err == nil || !strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("expected OpenAPI 3.1 schema rejection, got %v", err)
	}
	if strings.Contains(err.Error(), "card-secret-must-not-leak") {
		t.Fatalf("OpenAPI 3.1 schema error leaked request value: %v", err)
	}
}

func TestResolveMutationCallRejectsSensitiveHeadersAndOversizedBody(t *testing.T) {
	op := mutationContractOperation(t, OperationOverride{ExposeMutation: true, AllowedRoles: []string{"operator"}})
	op.HeaderParams = append(op.HeaderParams, ParamSpec{Name: "Authorization", In: ParamInHeader, Type: "string"})
	_, err := op.ResolveMutationCall(map[string]interface{}{
		"path": map[string]interface{}{"id": 1}, "headers": map[string]interface{}{"X-Trace": "x", "Authorization": "secret"},
		"body": map[string]interface{}{"name": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("sensitive header error = %v", err)
	}

	op.MaxRequestBytes = 10
	_, err = op.ResolveMutationCall(map[string]interface{}{
		"path": map[string]interface{}{"id": 1}, "headers": map[string]interface{}{"X-Trace": "x"},
		"body": map[string]interface{}{"name": "body-is-too-large"},
	})
	if err == nil || !strings.Contains(err.Error(), "max_request_bytes") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestCallerMutationSuccessStatus204AndNoImplicitRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path == "/empty" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"token":"must-not-leak"}`))
	}))
	defer srv.Close()

	empty := &OpDescriptor{OperationID: "empty", Method: "DELETE", PathTemplate: "/empty", Mode: OpModeMutation, SuccessStatuses: []int{204}}
	result, err := buildCaller(t, empty, srv.URL, nil).CallMutation(context.Background(), CallParams{})
	if err != nil || result.StatusCode != 204 || len(result.Body) != 0 || result.RetryCount != 0 {
		t.Fatalf("204 result = %+v, err=%v", result, err)
	}

	auth := &recordingAuth{}
	unauthorized := &OpDescriptor{OperationID: "unauthorized", Method: "POST", PathTemplate: "/unauthorized", Mode: OpModeMutation, SuccessStatuses: []int{201}}
	_, err = buildCaller(t, unauthorized, srv.URL, auth).CallMutation(context.Background(), CallParams{})
	if err == nil || atomic.LoadInt32(&calls) != 2 || auth.invalidated || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("retry/redaction contract failed: calls=%d invalidated=%v err=%v", calls, auth.invalidated, err)
	}
}

func TestCallerMutationRejectsEmptyNon204Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	op := &OpDescriptor{
		OperationID: "empty_created", Method: http.MethodPost, PathTemplate: "/empty",
		Mode: OpModeMutation, SuccessStatuses: []int{http.StatusCreated},
	}
	result, err := buildCaller(t, op, srv.URL, nil).CallMutation(context.Background(), CallParams{})
	if err == nil || result.StatusCode != http.StatusCreated || !strings.Contains(err.Error(), "empty JSON response") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCallerMutationMethodsAndDeclaredSuccessStatuses(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/post":
			w.WriteHeader(http.StatusCreated)
		case "/put":
			w.WriteHeader(http.StatusOK)
		case "/patch":
			w.WriteHeader(http.StatusAccepted)
		case "/delete":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
		if r.URL.Path != "/delete" {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	tests := []struct {
		method string
		status int
	}{
		{http.MethodPost, http.StatusCreated},
		{http.MethodPut, http.StatusOK},
		{http.MethodPatch, http.StatusAccepted},
		{http.MethodDelete, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			op := &OpDescriptor{
				OperationID: strings.ToLower(tc.method), Method: tc.method,
				PathTemplate: "/" + strings.ToLower(tc.method), Mode: OpModeMutation,
				SuccessStatuses: []int{tc.status}, RequestMediaType: "application/json",
			}
			params := CallParams{}
			if tc.method != http.MethodDelete {
				params.BodyJSON = []byte(`{"name":"fixture"}`)
			}
			result, err := buildCaller(t, op, srv.URL, nil).CallMutation(context.Background(), params)
			if err != nil || result.StatusCode != tc.status || result.RetryCount != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	if calls.Load() != int32(len(tests)) {
		t.Fatalf("upstream calls = %d, want %d", calls.Load(), len(tests))
	}
}

func TestCallerMutationDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()

	var initial atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		initial.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Location", target.URL+"/replayed")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = w.Write([]byte(`{"secret":"redirect-body-must-not-leak"}`))
	}))
	defer upstream.Close()

	op := &OpDescriptor{
		OperationID: "redirect", Method: http.MethodPost, PathTemplate: "/write",
		Mode: OpModeMutation, SuccessStatuses: []int{http.StatusCreated}, RequestMediaType: "application/json",
	}
	result, err := buildCaller(t, op, upstream.URL, nil).CallMutation(context.Background(), CallParams{BodyJSON: []byte(`{"name":"fixture"}`)})
	if err == nil || result.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if initial.Load() != 1 || redirected.Load() != 0 {
		t.Fatalf("redirect replayed mutation: initial=%d redirected=%d", initial.Load(), redirected.Load())
	}
	if strings.Contains(err.Error(), "redirect-body-must-not-leak") {
		t.Fatalf("redirect response body leaked in error: %v", err)
	}
}

func TestCallerMutationExplicitAuthRetryAndUpstreamFailureNoRetry(t *testing.T) {
	t.Run("explicit 401 retry", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()
		auth := &recordingAuth{}
		op := &OpDescriptor{
			OperationID: "retry", Method: http.MethodPost, PathTemplate: "/retry", Mode: OpModeMutation,
			SuccessStatuses: []int{http.StatusCreated}, RetryOnAuthFailure: true,
		}
		result, err := buildCaller(t, op, srv.URL, auth).CallMutation(context.Background(), CallParams{})
		if err != nil || result.RetryCount != 1 || !auth.invalidated || calls.Load() != 2 {
			t.Fatalf("result=%+v invalidated=%v calls=%d err=%v", result, auth.invalidated, calls.Load(), err)
		}
	})

	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"secret":"must-not-leak"}`))
			}))
			defer srv.Close()
			op := &OpDescriptor{OperationID: "failure", Method: http.MethodPost, PathTemplate: "/failure", Mode: OpModeMutation, SuccessStatuses: []int{http.StatusCreated}}
			_, err := buildCaller(t, op, srv.URL, nil).CallMutation(context.Background(), CallParams{})
			if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestCallerMutationResponseLimitCancellationAndAmbiguousTransport(t *testing.T) {
	t.Run("preflight auth error is not ambiguous", func(t *testing.T) {
		op := &OpDescriptor{OperationID: "auth", Method: "POST", PathTemplate: "/call", Mode: OpModeMutation, SuccessStatuses: []int{201}}
		_, err := buildCaller(t, op, "https://upstream.invalid", &basicAuth{}).CallMutation(context.Background(), CallParams{})
		if err == nil || !strings.Contains(err.Error(), "requires username") || strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("response limit", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"value":"larger than limit"}`))
		}))
		defer srv.Close()
		op := &OpDescriptor{OperationID: "large", Method: "POST", PathTemplate: "/large", Mode: OpModeMutation, SuccessStatuses: []int{201}, MaxResponseBytes: 8}
		_, err := buildCaller(t, op, srv.URL, nil).CallMutation(context.Background(), CallParams{})
		if err == nil || !strings.Contains(err.Error(), "max_response_bytes") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		op := &OpDescriptor{OperationID: "cancel", Method: "POST", PathTemplate: "/cancel", Mode: OpModeMutation, SuccessStatuses: []int{201}}
		_, err := buildCaller(t, op, "http://127.0.0.1", nil).CallMutation(ctx, CallParams{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			<-r.Context().Done()
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		op := &OpDescriptor{OperationID: "timeout", Method: "POST", PathTemplate: "/timeout", Mode: OpModeMutation, SuccessStatuses: []int{201}}
		_, err := buildCaller(t, op, srv.URL, nil).CallMutation(ctx, CallParams{})
		if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(fmt.Sprint(err), "ambiguous") || calls.Load() != 1 {
			t.Fatalf("calls=%d error=%v, want deadline exceeded without retry", calls.Load(), err)
		}
	})

	t.Run("ambiguous transport", func(t *testing.T) {
		op := &OpDescriptor{OperationID: "ambiguous", Method: "POST", PathTemplate: "/call", Mode: OpModeMutation, SuccessStatuses: []int{201}}
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		})}
		caller := NewCaller(op, "https://upstream.invalid", noopAuth{}, newLimiter(ConcurrencyConfig{}), client)
		_, err := caller.CallMutation(context.Background(), CallParams{})
		if err == nil || !strings.Contains(err.Error(), "outcome may be ambiguous") {
			t.Fatalf("error = %v", err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
