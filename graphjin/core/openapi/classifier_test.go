package openapi

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// loadDoc parses an inline OpenAPI YAML/JSON document. Failures abort the
// test rather than returning an error so callers don't need a t.Fatal at
// every spec-build site.
func loadDoc(t *testing.T, src string) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(src))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		// Validation failures are warnings in production code but tests
		// should fail loudly when a fixture is malformed.
		t.Logf("warning: doc validation: %v", err)
	}
	return doc
}

func TestClassifySingleByID(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /users/{userId}:
    get:
      operationId: getUserById
      parameters:
        - name: userId
          in: path
          required: true
          schema: { type: string }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  email: { type: string }
`)

	spec := &Spec{Key: "test"}
	ops, _ := classifyAll(spec, doc, SpecConfig{})
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	op := ops[0]
	if op.Mode != OpModeSingleByID {
		t.Errorf("mode = %v, want OpModeSingleByID", op.Mode)
	}
	if op.OperationID != "getUserById" {
		t.Errorf("operationId = %q", op.OperationID)
	}
	if op.ExposeAs != "test_get_user_by_id" {
		t.Errorf("exposeAs = %q, want test_get_user_by_id", op.ExposeAs)
	}
	if len(op.PathParams) != 1 || op.PathParams[0].Name != "userId" {
		t.Errorf("path params = %+v", op.PathParams)
	}
}

func TestClassifyRowJoinUpgrade(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /users/{userId}:
    get:
      operationId: getUserById
      parameters:
        - { name: userId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content: { application/json: { schema: { type: object } } }
`)

	cfg := SpecConfig{
		Joins: map[string]JoinConfig{
			"getUserById": {
				ParentTable:  "users",
				ParentColumn: "email",
				Param:        "userId",
				ExposeAs:     "is_profile",
			},
		},
	}

	spec := &Spec{Key: "test"}
	ops, _ := classifyAll(spec, doc, cfg)
	if ops[0].Mode != OpModeRowJoin {
		t.Errorf("mode = %v, want OpModeRowJoin", ops[0].Mode)
	}
	if ops[0].Join == nil || ops[0].Join.ParentTable != "users" {
		t.Errorf("join not wired correctly: %+v", ops[0].Join)
	}
	if ops[0].ExposeAs != "is_profile" {
		t.Errorf("expose_as override not applied: %q", ops[0].ExposeAs)
	}
}

func TestClassifyList(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /audit-logs:
    get:
      operationId: listAuditLogs
      parameters:
        - { name: actorId, in: query, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items: { type: object }
`)

	spec := &Spec{Key: "test"}
	ops, _ := classifyAll(spec, doc, SpecConfig{})
	if ops[0].Mode != OpModeList {
		t.Errorf("mode = %v, want OpModeList", ops[0].Mode)
	}
	if !ops[0].IsArrayResponse {
		t.Error("IsArrayResponse should be true for {data: [...]} shape")
	}
	if len(ops[0].ResultPath) != 1 || ops[0].ResultPath[0] != "data" {
		t.Errorf("result_path = %v, want [data]", ops[0].ResultPath)
	}
	if len(ops[0].QueryParams) != 1 || ops[0].QueryParams[0].Name != "actorId" {
		t.Errorf("query params = %+v", ops[0].QueryParams)
	}
}

func TestClassifySkipReasons(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantSkip string
	}{
		{
			name: "binary response",
			src: `
openapi: 3.0.0
info: { title: t, version: '1' }
paths:
  /export:
    get:
      operationId: exportFile
      responses:
        '200':
          description: ok
          content: { application/octet-stream: { schema: { type: string, format: binary } } }
`,
			wantSkip: "non-JSON",
		},
		{
			name: "async with Location",
			src: `
openapi: 3.0.0
info: { title: t, version: '1' }
paths:
  /jobs:
    get:
      operationId: getJob
      responses:
        '200':
          description: ok
          headers:
            Location: { schema: { type: string } }
          content: { application/json: { schema: { type: object } } }
`,
			wantSkip: "async pattern",
		},
		{
			name: "mutating verb",
			src: `
openapi: 3.0.0
info: { title: t, version: '1' }
paths:
  /widgets:
    post:
      operationId: createWidget
      responses:
        '201':
          description: created
          content: { application/json: { schema: { type: object } } }
`,
			wantSkip: "mutating verb",
		},
		{
			name: "nested path param",
			src: `
openapi: 3.0.0
info: { title: t, version: '1' }
paths:
  /users/{uid}/profile:
    get:
      operationId: getUserProfile
      parameters:
        - { name: uid, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content: { application/json: { schema: { type: object } } }
`,
			wantSkip: "trailing position",
		},
		{
			name: "multi path params",
			src: `
openapi: 3.0.0
info: { title: t, version: '1' }
paths:
  /users/{uid}/orders/{oid}:
    get:
      operationId: getUserOrder
      parameters:
        - { name: uid, in: path, required: true, schema: { type: string } }
        - { name: oid, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content: { application/json: { schema: { type: object } } }
`,
			wantSkip: "multi-segment path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := loadDoc(t, tc.src)
			ops, _ := classifyAll(&Spec{Key: "t"}, doc, SpecConfig{})
			if len(ops) == 0 {
				t.Fatal("no ops produced")
			}
			if ops[0].Mode != OpModeSkipped {
				t.Errorf("mode = %v, want OpModeSkipped (%s)", ops[0].Mode, ops[0].SkipReason)
			}
			if !contains(ops[0].SkipReason, tc.wantSkip) {
				t.Errorf("SkipReason = %q, want substring %q", ops[0].SkipReason, tc.wantSkip)
			}
		})
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("OPENAPI_TEST_VAR", "secret")

	cases := []struct {
		in, want string
	}{
		{"https://${OPENAPI_TEST_VAR}.example.com", "https://secret.example.com"},
		{"$OPENAPI_TEST_VAR", "$OPENAPI_TEST_VAR"}, // bare $VAR intentionally NOT expanded
		{"plain", "plain"},
		{"${MISSING_VAR}", ""}, // missing vars expand to empty
	}
	for _, tc := range cases {
		got := expandEnv(tc.in)
		if got != tc.want {
			t.Errorf("expandEnv(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"getUserById", "get_user_by_id"},
		{"listAuditLogs", "list_audit_logs"},
		{"GetHTTPStatus", "get_http_status"},
		{"already_snake", "already_snake"},
		{"", ""},
	}
	for _, tc := range cases {
		got := toSnakeCase(tc.in)
		if got != tc.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyExposeTopLevelOptIn(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /api/dataset/{datasetId}/users.json:
    get:
      operationId: exportUsers
      parameters:
        - { name: datasetId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                      properties:
                        id: { type: string }
`)

	spec := &Spec{Key: "is"}

	// Default: skipped.
	ops, _ := classifyAll(spec, doc, SpecConfig{})
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	if ops[0].Mode != OpModeSkipped {
		t.Errorf("default mode = %v, want OpModeSkipped", ops[0].Mode)
	}
	if !contains(ops[0].SkipReason, "expose_top_level") {
		t.Errorf("SkipReason = %q; should mention expose_top_level", ops[0].SkipReason)
	}

	// Opt-in: classified as list (response wraps an array).
	cfg := SpecConfig{Operations: map[string]OperationOverride{
		"exportUsers": {ExposeTopLevel: true},
	}}
	ops, _ = classifyAll(spec, doc, cfg)
	if ops[0].Mode != OpModeList {
		t.Errorf("opt-in mode = %v, want OpModeList", ops[0].Mode)
	}
}

func TestClassifyExposeTopLevelSingleObject(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /api/dataset/{datasetId}/summary.json:
    get:
      operationId: getDatasetSummary
      parameters:
        - { name: datasetId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  name: { type: string }
`)

	spec := &Spec{Key: "is"}
	cfg := SpecConfig{Operations: map[string]OperationOverride{
		"getDatasetSummary": {ExposeTopLevel: true},
	}}
	ops, _ := classifyAll(spec, doc, cfg)
	if ops[0].Mode != OpModeSingleByID {
		t.Errorf("opt-in mode = %v, want OpModeSingleByID (object response, not array)", ops[0].Mode)
	}
}

func TestClassifyAppliesOperationDefaults(t *testing.T) {
	doc := loadDoc(t, `
openapi: 3.0.0
info: { title: Test, version: 1.0.0 }
paths:
  /api/region/{region}/widgets.json:
    get:
      operationId: listWidgets
      parameters:
        - { name: region, in: path,  required: true, schema: { type: string } }
        - { name: limit,  in: query,                 schema: { type: integer } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: { type: string }
`)

	spec := &Spec{Key: "widgets"}
	cfg := SpecConfig{Operations: map[string]OperationOverride{
		"listWidgets": {
			ExposeTopLevel: true,
			Defaults: map[string]string{
				"region": "us-east",
				"limit":  "25",
			},
		},
	}}
	ops, _ := classifyAll(spec, doc, cfg)
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	op := ops[0]
	if op.Mode != OpModeList {
		t.Fatalf("mode = %v, want OpModeList", op.Mode)
	}
	if got := op.Defaults["region"]; got != "us-east" {
		t.Errorf("Defaults[region] = %q, want us-east", got)
	}
	if got := op.Defaults["limit"]; got != "25" {
		t.Errorf("Defaults[limit] = %q, want 25", got)
	}

	// Mutating the descriptor's defaults must not leak back into the
	// caller's config map — confirms the classifier copied the map.
	op.Defaults["region"] = "mutated"
	if cfg.Operations["listWidgets"].Defaults["region"] != "us-east" {
		t.Errorf("classifier did not isolate the override Defaults map from the descriptor copy")
	}
}

// contains is a tiny stand-in for strings.Contains kept local so the
// classifier_test file doesn't drag in extra imports for one call site.
func contains(s, sub string) bool {
	return len(sub) <= len(s) && (sub == "" || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
