package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestSemanticCatalogGuidanceAndToolSchemaAreConditional(t *testing.T) {
	features := CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}
	if got := catalogSearchInstruction(runtimeUsageInstructions, CatalogSearchFeatures{}); got != runtimeUsageInstructions {
		t.Fatal("lexical-only prompt changed")
	}
	semanticPrompt := catalogSearchInstruction(runtimeUsageInstructions, features)
	for _, phrase := range []string{"short noun-and-intent phrases", "combined relationship intent", "recall candidates", "at most one query_catalog({searches:", "same JavaScript block", "coverage.next.args.ids", "never call searches again", "never derive or pass an empty ids array", "without another catalog call"} {
		if !strings.Contains(semanticPrompt, phrase) {
			t.Fatalf("semantic prompt missing %q", phrase)
		}
	}

	queryCatalog := func(agent *Agent) map[string]struct{} {
		fields := map[string]struct{}{}
		for _, tool := range agent.tools(context.Background(), Request{}, agent.runtime) {
			if tool.Name != "query_catalog" {
				continue
			}
			for name := range tool.Args {
				fields[name] = struct{}{}
			}
			return fields
		}
		t.Fatal("query_catalog tool missing")
		return nil
	}
	lexical := newAgent(Config{}, &fakeRuntime{})
	if _, exists := queryCatalog(lexical)["searches"]; exists {
		t.Fatal("lexical-only internal tool exposed searches")
	}
	semantic := newAgent(Config{}, &fakeRuntime{}, WithCatalogSearchFeatures(features))
	if _, exists := queryCatalog(semantic)["searches"]; !exists {
		t.Fatal("semantic internal tool did not expose searches")
	}

	capturePrompt := func(features CatalogSearchFeatures) string {
		t.Helper()
		program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
		options := []Option{
			WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
			WithProgramFactory(func(_ string, values map[string]ax.Value) Program {
				program.options = values
				program.onForward = func(p *fakeProgram) {
					callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "help:discovery"})
				}
				return program
			}),
		}
		if features.enabled() {
			options = append(options, WithCatalogSearchFeatures(features))
		}
		runner := newAgent(Config{TimeoutSeconds: 5}, &fakeRuntime{}, options...)
		if _, err := runner.Run(context.Background(), Request{Instruction: "find customers"}); err != nil {
			t.Fatal(err)
		}
		runtime, ok := program.options["runtime"].(map[string]ax.Value)
		if !ok {
			t.Fatalf("runtime option = %T", program.options["runtime"])
		}
		prompt, _ := runtime["usageInstructions"].(string)
		return prompt
	}
	marker := "Semantic catalog recall is available"
	if strings.Contains(capturePrompt(CatalogSearchFeatures{}), marker) {
		t.Fatal("lexical-only runtime prompt contained semantic guidance")
	}
	if !strings.Contains(capturePrompt(features), marker) {
		t.Fatal("semantic guidance did not reach runtime.usageInstructions")
	}
}

func TestRuntimeSeedUsageInstructionsNameExactCodePathsAndShapes(t *testing.T) {
	for _, phrase := range []string{
		"inputs.context._graphjin_discovery",
		"do not require one saved query per requested entity",
		"const card = saved[0]",
		"never put them in Promise.then callbacks",
		"inputs.distilledContext does not exist",
		"result.cards[0]",
		"execute_saved_query({name: card.title})",
		"execution.data",
		"never make another catalog call",
		"identical repeated query_catalog request is rejected before execution",
		"returns recovery.execution instead",
		"globalThis.graphjinLastExecution",
		"graphjinLastExecution.result.data",
		"Never infer that business data is absent from catalog-card prose",
	} {
		if !strings.Contains(runtimeSeedUsageInstructions, phrase) {
			t.Fatalf("runtime seed guidance missing %q", phrase)
		}
	}
}

func TestExecutorHandoffInstructionsRequireExplicitDiscovery(t *testing.T) {
	for _, phrase := range []string{
		"initial catalog seed is orientation only",
		"never satisfies the required model-driven discovery action",
		`query_catalog({kind:"saved_query", limit:10})`,
		"globalThis.graphjinDistilledContext",
		"globalThis.graphjinHistory",
		"most recent entries",
		"rejected until its JavaScript references graphjinHistory",
		"globalThis.graphjinExecutorRequest",
		"normalized user request",
		"runtime-only seed fallback",
		"Never treat a draft answer from the distiller as final evidence",
		"globalThis.graphjinLastExecution",
		"graphjinLastExecution.result.data",
	} {
		if !strings.Contains(executorHandoffInstructions, phrase) {
			t.Fatalf("executor handoff guidance missing %q", phrase)
		}
	}
}

func TestProtocolRejectsIdenticalCatalogRequestLoopsAndReplaysExecution(t *testing.T) {
	base := &fakeRuntime{}
	runtime := newProtocolRuntime(base, "find customers", "", 40, nil, nil, CatalogSearchFeatures{})
	args := map[string]any{"kind": "saved_query", "search": "daily planning", "limit": 10}

	if _, err := runtime.QueryCatalog(context.Background(), cloneMap(args)); err != nil {
		t.Fatalf("first query_catalog call: %v", err)
	}
	if _, err := runtime.QueryCatalog(context.Background(), cloneMap(args)); err == nil || !strings.Contains(err.Error(), "duplicate query_catalog call rejected") {
		t.Fatalf("duplicate error = %v", err)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("runtime calls = %d, want one successful catalog dispatch", got)
	}

	execution := map[string]any{"data": map[string]any{"production_orders": []any{map[string]any{"product_name": "Northstar"}}}}
	runtime.state.recordExecution("execute_saved_query", map[string]any{"name": "daily_roast_context"}, execution)
	replayed, err := runtime.QueryCatalog(context.Background(), cloneMap(args))
	if err != nil {
		t.Fatalf("post-execution duplicate recovery: %v", err)
	}
	recovery := mapValue(mapValue(replayed)["recovery"])
	replayedExecution := mapValue(recovery["execution"])
	if got := mapValue(mapValue(replayedExecution["result"])["data"]); got == nil || got["production_orders"] == nil {
		t.Fatalf("post-execution recovery did not replay live data: %+v", replayed)
	}
	if got := len(base.calls); got != 1 {
		t.Fatalf("runtime calls after recovery = %d, want one catalog dispatch", got)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestSemanticCoverageProtocolValidationAndOneBatchLimit(t *testing.T) {
	features := CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}
	newRuntime := func(enabled bool) (*protocolRuntime, *fakeRuntime) {
		base := &fakeRuntime{}
		profile := CatalogSearchFeatures{}
		if enabled {
			profile = features
		}
		return newProtocolRuntime(base, "find customers", "", 20, nil, nil, profile), base
	}
	seedRuntime, seedBase := newRuntime(true)
	if _, err := seedRuntime.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The seed's own search must stay one unexpanded lexical/semantic call;
	// coverage batching remains the model's explicit one-shot choice. No hidden
	// saved-query lookup may expand the seed.
	seedArgs := seedRuntime.state.actions[0].Args
	if stringArg(seedArgs, "search") != "find customers" || seedArgs["searches"] != nil {
		t.Fatalf("semantic seed changed or expanded automatically: args=%+v", seedArgs)
	}
	for _, args := range []map[string]any{seedBase.args} {
		if args["searches"] != nil {
			t.Fatalf("no automatic call may use coverage batching: %+v", args)
		}
	}

	for _, test := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "too few", args: map[string]any{"searches": []any{"customers"}}, want: "two or three"},
		{name: "too many", args: map[string]any{"searches": []any{"a", "b", "c", "d"}}, want: "two or three"},
		{name: "empty", args: map[string]any{"searches": []any{"customers", "  "}}, want: "cannot be empty"},
		{name: "duplicate", args: map[string]any{"searches": []any{"Customers", " customers "}}, want: "unique"},
		{name: "non string", args: map[string]any{"searches": []any{"customers", 42}}, want: "must be strings"},
		{name: "too long", args: map[string]any{"searches": []any{"customers", strings.Repeat("x", MaxCatalogCoverageSearchBytes+1)}}, want: "at most"},
		{name: "with search", args: map[string]any{"searches": []any{"customers", "products"}, "search": "orders"}, want: "mutually exclusive"},
		{name: "with id", args: map[string]any{"searches": []any{"customers", "products"}, "id": "table:customers"}, want: "mutually exclusive"},
		{name: "with ids", args: map[string]any{"searches": []any{"customers", "products"}, "ids": []any{"table:customers"}}, want: "mutually exclusive"},
		{name: "with order", args: map[string]any{"searches": []any{"customers", "products"}, "order_by": map[string]any{"title": "asc"}}, want: "mutually exclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, base := newRuntime(true)
			if _, err := runtime.QueryCatalog(context.Background(), test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if len(base.calls) != 0 {
				t.Fatalf("invalid coverage reached runtime: %v", base.calls)
			}
		})
	}

	lexical, _ := newRuntime(false)
	if _, err := lexical.QueryCatalog(context.Background(), map[string]any{"searches": []any{"customers", "products"}}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("lexical-only error = %v", err)
	}

	runtime, base := newRuntime(true)
	args := map[string]any{
		"searches": []any{" customers and products bought ", " customers ", "products purchased"},
		"where":    map[string]any{"database_name": map[string]any{"eq": "app"}},
		"limit":    20,
	}
	if _, err := runtime.QueryCatalog(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if got, ok := base.args["explain"].(bool); !ok || !got {
		t.Fatalf("explain = %#v, want true", base.args["explain"])
	}
	searches := stringSliceArg(base.args, "searches")
	if len(searches) != 3 || searches[0] != "customers and products bought" {
		t.Fatalf("normalized searches = %#v", searches)
	}
	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"searches": []any{"orders", "products"}}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("second coverage error = %v", err)
	}
	if len(base.calls) != 1 {
		t.Fatalf("runtime calls = %v, want one coverage dispatch", base.calls)
	}
}

type semanticAgentFlowRuntime struct {
	*fakeRuntime
	catalogArgs []map[string]any
}

func (r *semanticAgentFlowRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	copyArgs, _ := normalizeValue(args).(map[string]any)
	r.catalogArgs = append(r.catalogArgs, copyArgs)
	return r.fakeRuntime.QueryCatalog(ctx, args)
}

func TestSemanticAgentInspectsCoveragePathBeforeExecution(t *testing.T) {
	base := &fakeRuntime{}
	base.catalogOverride = func(args map[string]any) any {
		if len(detailIDsFromArgs(args)) != 0 {
			return fakeCatalogResult(args)
		}
		if len(stringSliceArg(args, "searches")) != 0 {
			return map[string]any{
				"count": 3,
				"cards": []any{
					map[string]any{"id": "table:customers", "kind": "table", "table_name": "customers"},
					map[string]any{"id": "relationship:orders_products", "kind": "relationship", "summary": "real foreign-key path"},
					map[string]any{"id": "table:products", "kind": "table", "table_name": "products"},
				},
				"retrieval": map[string]any{"mode": "coverage_hybrid", "relationship_path_count": 1},
			}
		}
		return fakeCatalogResult(args)
	}
	runtime := &semanticAgentFlowRuntime{fakeRuntime: base}
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
	runner := newAgent(Config{TimeoutSeconds: 5}, runtime,
		WithCatalogSearchFeatures(CatalogSearchFeatures{SemanticRecall: true, CoverageBatch: true}),
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"searches": []any{
					"customers and products bought", "customer buyers", "products purchased",
				}})
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"ids": []any{
					"table:customers", "relationship:orders_products", "table:products",
				}})
				callProgramTool(t, p, "execute_graphql", map[string]ax.Value{
					"query": "query { customers { id products { id } } }",
				})
			}
			return program
		}),
	)
	response, err := runner.Run(context.Background(), Request{Instruction: "show customers and products they bought"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered {
		t.Fatalf("response = %+v", response)
	}
	// Seed, model coverage batch, and detail; the raw execution then proceeds
	// directly on that discovery evidence without a hidden supplement lookup.
	if len(runtime.catalogArgs) != 3 {
		t.Fatalf("catalog calls = %+v, want seed, coverage, detail", runtime.catalogArgs)
	}
	if len(stringSliceArg(runtime.catalogArgs[1], "searches")) != 3 {
		t.Fatalf("second catalog call was not coverage: %+v", runtime.catalogArgs[1])
	}
	wantDetails := []string{"table:customers", "relationship:orders_products", "table:products"}
	gotDetails := detailIDsFromArgs(runtime.catalogArgs[2])
	if strings.Join(gotDetails, "|") != strings.Join(wantDetails, "|") {
		t.Fatalf("detail inspection = %v, want returned endpoints and real path %v", gotDetails, wantDetails)
	}
	wantCalls := "query_catalog|query_catalog|query_catalog|execute_graphql"
	if got := strings.Join(base.calls, "|"); got != wantCalls {
		t.Fatalf("tool order = %s, want %s", got, wantCalls)
	}
}
