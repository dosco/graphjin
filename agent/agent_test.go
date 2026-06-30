package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
)

type fakeClient struct{}

func (fakeClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (fakeClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (fakeClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

type fakeProgram struct {
	output         ax.Value
	err            error
	actionLog      ax.Value
	options        map[string]ax.Value
	forwardValues  map[string]ax.Value
	forwardOptions map[string]ax.Value
	onForward      func(*fakeProgram)
}

func (p *fakeProgram) Forward(_ context.Context, _ ax.AIClient, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	p.forwardValues = values
	p.forwardOptions = options
	if p.onForward != nil {
		p.onForward(p)
	}
	return p.output, p.err
}

func (p *fakeProgram) GetActionLog() ax.Value {
	if p.actionLog != nil {
		return p.actionLog
	}
	return []ax.Value{map[string]ax.Value{"tool": "query_catalog"}}
}

func (p *fakeProgram) GetUsage() ax.Value {
	return map[string]ax.Value{"total_tokens": 12}
}

func (p *fakeProgram) ExportTrace() ax.Value {
	return map[string]ax.Value{"ok": true}
}

type fakeRuntime struct {
	calls           []string
	args            map[string]any
	catalogOverride func(args map[string]any) any
}

func (r *fakeRuntime) GraphQLHelp(_ context.Context, args map[string]any) (any, error) {
	return r.record("graphql_help", args), nil
}

func (r *fakeRuntime) QueryCatalog(_ context.Context, args map[string]any) (any, error) {
	r.record("query_catalog", args)
	if r.catalogOverride != nil {
		return r.catalogOverride(args), nil
	}
	return fakeCatalogResult(args), nil
}

func (r *fakeRuntime) ValidateWhereClause(_ context.Context, args map[string]any) (any, error) {
	return r.record("validate_where_clause", args), nil
}

func (r *fakeRuntime) ExecuteSavedQuery(_ context.Context, args map[string]any) (any, error) {
	r.record("execute_saved_query", args)
	return map[string]any{
		"data": map[string]any{
			"production_orders": []any{map[string]any{"product_name": "Northstar House Blend 340g"}},
		},
	}, nil
}

func (r *fakeRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	return r.record("execute_graphql", args), nil
}

func (r *fakeRuntime) record(name string, args map[string]any) any {
	r.calls = append(r.calls, name)
	r.args = args
	return map[string]any{"tool": name, "ok": true}
}

func fakeCatalogResult(args map[string]any) any {
	id := stringArg(args, "id")
	if id != "" {
		kind := "help"
		if strings.HasPrefix(id, "saved_query:") {
			kind = "saved_query"
		}
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id":      id,
				"kind":    kind,
				"name":    strings.TrimPrefix(id, "saved_query:"),
				"title":   id,
				"summary": "detail row",
			}},
			"details": []any{map[string]any{"card_id": id, "section": "details", "content": "detail"}},
			"next":    map[string]any{"recommended_tool": "query_catalog"},
		}
	}
	if stringArg(args, "kind") == "saved_query" {
		return map[string]any{
			"count": 1,
			"cards": []any{map[string]any{
				"id":      "saved_query:daily_roast_context",
				"kind":    "saved_query",
				"name":    "daily_roast_context",
				"summary": "daily roast context",
			}},
			"next": map[string]any{"recommended_tool": "query_catalog"},
		}
	}
	return map[string]any{
		"count": 1,
		"cards": []any{map[string]any{
			"id":      "help:discovery",
			"kind":    "help",
			"name":    "discovery",
			"summary": "discovery help",
		}},
		"next": map[string]any{"recommended_tool": "query_catalog"},
	}
}

func TestNewRequiresGraphJin(t *testing.T) {
	if _, err := New(nil, Config{}); !errors.Is(err, ErrMissingGraphJin) {
		t.Fatalf("New(nil) err = %v, want ErrMissingGraphJin", err)
	}
}

func TestRunSafeModeCapsStepsAndExposesSafeTools(t *testing.T) {
	rt := &fakeRuntime{}
	program := &fakeProgram{output: map[string]ax.Value{
		"status": StatusAnswered,
		"answer": "found it",
	}}
	var programOptions map[string]ax.Value
	runner := newAgent(Config{MaxSteps: 5, TimeoutSeconds: 5, AllowRawGraphQL: true}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			programOptions = options
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
			}
			return program
		}),
		WithNow(func() time.Time { return time.Unix(10, 0) }),
	)

	resp, err := runner.Run(context.Background(), Request{
		Instruction: "find customers",
		Namespace:   "tenant_a",
		MaxSteps:    99,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusAnswered || resp.Answer != "found it" || resp.TraceID != "agent-10000000000" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got := program.forwardOptions["max_actor_steps"]; got != 5 {
		t.Fatalf("max_actor_steps = %v, want 5", got)
	}
	ctxValue, ok := normalizeValue(program.forwardValues["context"]).(map[string]any)
	if !ok || ctxValue[protocolContextKey] == nil {
		t.Fatalf("forward context missing %s: %#v", protocolContextKey, program.forwardValues["context"])
	}

	tools := toolsByName(t, programOptions)
	want := []string{"execute_saved_query", "graphql_help", "query_catalog", "validate_where_clause"}
	if got := sortedKeys(tools); !reflect.DeepEqual(got, want) {
		t.Fatalf("safe tools = %v, want %v", got, want)
	}
	if _, ok := tools["execute_graphql"]; ok {
		t.Fatal("safe mode must not expose raw GraphQL")
	}
	if _, err := tools["query_catalog"].Handler(map[string]ax.Value{"search": "customers"}); err != nil {
		t.Fatalf("query_catalog handler: %v", err)
	}
	if got := rt.args["namespace"]; got != "tenant_a" {
		t.Fatalf("namespace default = %v, want tenant_a", got)
	}
}

func TestRunDiscoveryOnlyAndRawToolGates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cfg       Config
		req       Request
		wantTools []string
	}{
		{
			name: "discovery only hides execution",
			cfg:  Config{MaxSteps: 4, TimeoutSeconds: 5, AllowRawGraphQL: true},
			req:  Request{Instruction: "inspect catalog", Mode: ModeDiscoveryOnly},
			wantTools: []string{
				"graphql_help", "query_catalog", "validate_where_clause",
			},
		},
		{
			name: "raw allowed still needs config gate",
			cfg:  Config{MaxSteps: 4, TimeoutSeconds: 5, AllowRawGraphQL: false},
			req:  Request{Instruction: "run raw", Mode: ModeRawAllowed},
			wantTools: []string{
				"execute_saved_query", "graphql_help", "query_catalog", "validate_where_clause",
			},
		},
		{
			name: "raw allowed exposes raw when config permits",
			cfg:  Config{MaxSteps: 4, TimeoutSeconds: 5, AllowRawGraphQL: true},
			req:  Request{Instruction: "run raw", Mode: ModeRawAllowed},
			wantTools: []string{
				"execute_graphql", "execute_saved_query", "graphql_help", "query_catalog", "validate_where_clause",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
			var programOptions map[string]ax.Value
			runner := newAgent(tc.cfg, &fakeRuntime{},
				WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
				WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
					programOptions = options
					program.options = options
					program.onForward = func(p *fakeProgram) {
						callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
					}
					return program
				}),
			)
			if _, err := runner.Run(context.Background(), tc.req); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := sortedKeys(toolsByName(t, programOptions)); !reflect.DeepEqual(got, tc.wantTools) {
				t.Fatalf("tools = %v, want %v", got, tc.wantTools)
			}
		})
	}
}

func TestRunValidationAndClientConfigErrors(t *testing.T) {
	runner := newAgent(Config{}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
	)
	if _, err := runner.Run(context.Background(), Request{}); !errors.Is(err, ErrMissingInstruction) {
		t.Fatalf("empty instruction err = %v, want ErrMissingInstruction", err)
	}
	if _, err := runner.Run(context.Background(), Request{Instruction: "x", Mode: "unsafe"}); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("invalid mode err = %v, want ErrInvalidMode", err)
	}

	t.Setenv("GRAPHJIN_TEST_AGENT_KEY", "")
	if _, err := DefaultClientFactory(Config{APIKeyEnv: "GRAPHJIN_TEST_AGENT_KEY"}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("missing key err = %v, want ErrMissingAPIKey", err)
	}
}

func TestEffectiveTimeoutSecondsFloorsAgentRuns(t *testing.T) {
	if got := EffectiveTimeoutSeconds(0); got != 50 {
		t.Fatalf("EffectiveTimeoutSeconds(0) = %d, want 50", got)
	}
	if got := EffectiveTimeoutSeconds(12); got != 50 {
		t.Fatalf("EffectiveTimeoutSeconds(12) = %d, want 50", got)
	}
	if got := EffectiveTimeoutSeconds(150); got != 150 {
		t.Fatalf("EffectiveTimeoutSeconds(150) = %d, want 150", got)
	}
}

func TestClarificationErrorUsesQuestionAsAnswer(t *testing.T) {
	resp := responseFromError(ax.AxError{
		Category: "clarification",
		Payload: map[string]any{
			"type": "__generated__askClarification",
			"args": []any{
				map[string]any{
					"question": "Which saved query should I run?",
				},
			},
		},
	}, "agent-test", nil, false)

	if resp.Status != StatusNeedsClarification {
		t.Fatalf("status = %s, want %s", resp.Status, StatusNeedsClarification)
	}
	if resp.Answer != "Which saved query should I run?" {
		t.Fatalf("answer = %q", resp.Answer)
	}
	if strings.Contains(resp.Answer, "askClarification") || strings.Contains(resp.Answer, "__order") {
		t.Fatalf("answer leaked clarification payload: %q", resp.Answer)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", resp.Errors)
	}
}

func TestAgentSignatureIsAcceptedByAx(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Ax rejected agent signature: %v", recovered)
		}
	}()
	_ = ax.NewAgent(agentSignature, map[string]ax.Value{})
}

func TestRunBlocksAnsweredResponseWithoutModelDiscovery(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "unsupported"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			return program
		}),
	)

	resp, err := runner.Run(context.Background(), Request{Instruction: "answer from memory"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusBlocked {
		t.Fatalf("status = %s, want blocked: %+v", resp.Status, resp)
	}
	if !responseHasProtocolError(resp, "model_discovery_required") {
		t.Fatalf("missing model_discovery_required error: %+v", resp.Errors)
	}
}

func TestRunRejectsSavedQueryExecutionBeforeDetailLookup(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				_, _ = callProgramToolError(p, "execute_saved_query", map[string]ax.Value{"name": "daily_roast_context"})
			}
			return program
		}),
	)

	resp, err := runner.Run(context.Background(), Request{Instruction: "run saved query"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusBlocked {
		t.Fatalf("status = %s, want blocked: %+v", resp.Status, resp)
	}
	if !responseHasProtocolError(resp, "saved_query_detail_required") {
		t.Fatalf("missing saved_query_detail_required error: %+v", resp.Errors)
	}
}

func TestRunAllowsSavedQueryExecutionAfterDetailLookup(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "saved_query:daily_roast_context"})
				callProgramTool(t, p, "execute_saved_query", map[string]ax.Value{"name": "daily_roast_context"})
			}
			return program
		}),
	)

	resp, err := runner.Run(context.Background(), Request{Instruction: "run daily roast context"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusAnswered {
		t.Fatalf("status = %s, want answered: %+v", resp.Status, resp)
	}
	evidence, ok := resp.Evidence.(map[string]any)
	if !ok {
		t.Fatalf("evidence type = %T", resp.Evidence)
	}
	if !stringSliceEvidenceContains(evidence["saved_queries_detailed"], "daily_roast_context") {
		t.Fatalf("saved query detail missing from evidence: %+v", evidence)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
}

func TestRunUsesFinalPayloadWhenAxReturnsExecutorError(t *testing.T) {
	program := &fakeProgram{
		err: errors.New("agent executor did not return runtime code field: javascriptCode"),
		actionLog: []ax.Value{map[string]ax.Value{
			"type": "runtime_step",
			"completion_payload": map[string]ax.Value{
				"type": "final",
				"args": []ax.Value{
					map[string]ax.Value{
						"status": "answered",
						"answer": "coffee plan ready",
						"data":   map[string]ax.Value{"source": "daily_roast_context"},
					},
				},
			},
		}},
	}
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
			}
			return program
		}),
		WithNow(func() time.Time { return time.Unix(11, 0) }),
	)

	resp, err := runner.Run(context.Background(), Request{Instruction: "answer"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusAnswered || resp.Answer != "coffee plan ready" || len(resp.Errors) != 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.TraceID != "agent-11000000000" {
		t.Fatalf("trace id = %q", resp.TraceID)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok || data["source"] != "daily_roast_context" {
		t.Fatalf("data = %#v, want source", resp.Data)
	}
}

func TestRunBlocksNoRuntimeCodeExecutorErrorWithoutFinal(t *testing.T) {
	runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, _ map[string]ax.Value) Program {
			return &fakeProgram{err: errors.New("agent executor did not return runtime code field: javascriptCode")}
		}),
		WithNow(func() time.Time { return time.Unix(12, 0) }),
	)

	resp, err := runner.Run(context.Background(), Request{Instruction: "inventory only", Mode: ModeDiscoveryOnly})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Status != StatusBlocked {
		t.Fatalf("status = %s, want blocked: %+v", resp.Status, resp)
	}
	if !responseHasProtocolError(resp, "agent_no_runtime_code") {
		t.Fatalf("missing agent_no_runtime_code error: %+v", resp.Errors)
	}
	evidence, ok := resp.Evidence.(map[string]any)
	if !ok {
		t.Fatalf("evidence type = %T", resp.Evidence)
	}
	seed, ok := evidence["seed"].(map[string]any)
	if !ok || seed["ok"] != true {
		t.Fatalf("seed evidence missing: %+v", evidence)
	}
}

func TestAsMapAcceptsRuntimeJSONObjects(t *testing.T) {
	args := asMap(map[string]any{
		"kind":  "saved_query",
		"limit": float64(10),
		"where": map[string]any{
			"kind": map[string]any{"eq": "saved_query"},
		},
	})
	if args["kind"] != "saved_query" {
		t.Fatalf("kind = %#v, want saved_query", args["kind"])
	}
	if args["limit"] != float64(10) {
		t.Fatalf("limit = %#v, want 10", args["limit"])
	}
	where, ok := args["where"].(map[string]any)
	if !ok {
		t.Fatalf("where type = %T, want map[string]any", args["where"])
	}
	kind, ok := where["kind"].(map[string]any)
	if !ok || kind["eq"] != "saved_query" {
		t.Fatalf("where.kind = %#v, want eq saved_query", where["kind"])
	}
}

func TestCoreRuntimeRawMutationGate(t *testing.T) {
	rt := newCoreRuntime(&core.GraphJin{}, Config{AllowRawGraphQL: true})
	if _, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": "mutation { x }"}); err == nil || !strings.Contains(err.Error(), "mutations are not allowed") {
		t.Fatalf("mutation gate err = %v", err)
	}
}

func TestContainsMutationOperation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  bool
	}{
		{name: "simple mutation", query: `mutation { createUser { id } }`, want: true},
		{name: "fragment before mutation", query: `fragment userBits on users { id }
mutation CreateUser { users(insert: { id: 1 }) { id } }`, want: true},
		{name: "short query", query: `{ users { id } }`, want: false},
		{name: "query type named mutation", query: `query GetUsers($input: MutationInput) { users { id } }`, want: false},
		{name: "query operation named mutation", query: `query mutation { users { id } }`, want: false},
		{name: "mutation in string", query: `query { logs(where: { message: { eq: "mutation { unsafe }" } }) { id } }`, want: false},
		{name: "mutation in block string", query: "query { logs(where: { message: { eq: \"\"\"mutation { unsafe }\"\"\" } }) { id } }", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContainsMutationOperation(tc.query); got != tc.want {
				t.Fatalf("ContainsMutationOperation() = %v, want %v", got, tc.want)
			}
		})
	}
}

func toolsByName(t *testing.T, options map[string]ax.Value) map[string]ax.Tool {
	t.Helper()
	var items []ax.Value
	switch arr := options["functions"].(type) {
	case []ax.Value:
		items = arr
	case *ax.AxArray:
		items = arr.Items
	default:
		t.Fatalf("functions option has type %T", options["functions"])
	}
	out := map[string]ax.Tool{}
	for _, item := range items {
		tool, ok := item.(ax.Tool)
		if !ok {
			t.Fatalf("function item has type %T", item)
		}
		out[tool.Name] = tool
	}
	return out
}

func callProgramTool(t *testing.T, program *fakeProgram, name string, args map[string]ax.Value) ax.Value {
	t.Helper()
	out, err := callProgramToolError(program, name, args)
	if err != nil {
		t.Fatalf("%s handler: %v", name, err)
	}
	return out
}

func callProgramToolError(program *fakeProgram, name string, args map[string]ax.Value) (ax.Value, error) {
	tools := map[string]ax.Tool{}
	var items []ax.Value
	switch arr := program.options["functions"].(type) {
	case []ax.Value:
		items = arr
	case *ax.AxArray:
		items = arr.Items
	}
	for _, item := range items {
		if tool, ok := item.(ax.Tool); ok {
			tools[tool.Name] = tool
		}
	}
	tool, ok := tools[name]
	if !ok {
		return nil, errors.New("missing tool: " + name)
	}
	return tool.Handler(args)
}

func responseHasProtocolError(resp Response, code string) bool {
	for _, item := range resp.Errors {
		if item.Extensions != nil && item.Extensions["code"] == code {
			return true
		}
	}
	return false
}

func stringSliceEvidenceContains(value any, want string) bool {
	for _, item := range anySlice(value) {
		if item == want {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]ax.Tool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
