package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
)

type selectionRuntime struct {
	mu             sync.Mutex
	snapshot       *core.CatalogSnapshot
	seedIDs        []string
	reads          [][]string
	executions     int
	detailRevision string
	missing        bool
}

func (r *selectionRuntime) CatalogDiscovery(ctx context.Context, _ map[string]any) (*CatalogDiscoveryIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return NewCatalogDiscoveryIndex(r.snapshot), nil
}
func (r *selectionRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids := detailIDsFromArgs(args)
	r.mu.Lock()
	r.reads = append(r.reads, append([]string(nil), ids...))
	r.mu.Unlock()
	if len(ids) == 0 {
		var cards []core.CatalogCard
		for _, card := range r.snapshot.Cards {
			for _, id := range r.seedIDs {
				if card.ID == id {
					cards = append(cards, card)
				}
			}
		}
		return catalogResult{Revision: r.snapshot.Revision, Cards: SummarizeCatalogCards(cards)}, nil
	}
	if r.missing {
		return catalogResult{Revision: r.snapshot.Revision}, nil
	}
	out := catalogDetailResult(r.snapshot, ids)
	if r.detailRevision != "" {
		out.Revision = r.detailRevision
	}
	return out, nil
}
func (r *selectionRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	return r.QueryCatalog(ctx, args)
}
func (r *selectionRuntime) ValidateWhereClause(context.Context, map[string]any) (any, error) {
	return map[string]any{"valid": true}, nil
}
func (r *selectionRuntime) ExecuteSavedQuery(ctx context.Context, args map[string]any) (any, error) {
	return r.ExecuteGraphQL(ctx, args)
}
func (r *selectionRuntime) ExecuteGraphQL(context.Context, map[string]any) (any, error) {
	r.mu.Lock()
	r.executions++
	r.mu.Unlock()
	return map[string]any{"data": map[string]any{"invoices": []any{map[string]any{"id": 1}}}}, nil
}

type selectionProgram struct {
	fakeProgram
	decide func(context.Context, []map[string]any) (ax.Value, error)
}

func (p *selectionProgram) Forward(ctx context.Context, _ ax.AIClient, values map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	var candidates []map[string]any
	if err := json.Unmarshal([]byte(values["candidates"].(string)), &candidates); err != nil {
		return nil, err
	}
	return p.decide(ctx, candidates)
}

func selectionFactory(decide func(context.Context, []map[string]any) (ax.Value, error)) ProgramFactory {
	return func(_ string, _ map[string]ax.Value) Program { return &selectionProgram{decide: decide} }
}

func selectRelevant(_ context.Context, candidates []map[string]any) (ax.Value, error) {
	out := map[string]ax.Value{}
	for i, candidate := range candidates {
		id := stringFromMap(candidate, "id")
		var value any = true
		if stringFromMap(candidate, "kind") != "" {
			value = float64(0)
			if strings.Contains(id, "invoices") || strings.Contains(id, "customer") {
				value = float64(3)
			}
		}
		if id == skillCodeRead || id == skillDataWrite || id == skillCodeWrite {
			value = false
		}
		out[fmt.Sprintf("slot%d", i)] = map[string]any{"value": value, "provenance": map[string]any{"source": "native", "provider": "scripted"}}
	}
	return out, nil
}

func selectionFixture() *selectionRuntime {
	return &selectionRuntime{snapshot: &core.CatalogSnapshot{
		Revision: "revision-1",
		Cards: []core.CatalogCard{
			{ID: "table:db:public.invoices", Kind: "table", Title: "invoices", TableName: "invoices", Summary: "invoice amounts"},
			{ID: "table:db:public.customers", Kind: "table", Title: "customers", TableName: "customers", Summary: "invoice customers"},
			{ID: "saved_query:invoices", Kind: "saved_query", Title: "invoices", GraphQLQuery: `query { invoices { id } }`},
			{ID: "table:db:public.unrelated", Kind: "table", Title: "unrelated", TableName: "unrelated"},
		},
		Nodes: []core.CatalogNode{{ID: "n1", CardID: "table:db:public.invoices"}, {ID: "n2", CardID: "table:db:public.customers"}},
		Edges: []core.CatalogEdge{{ID: "e1", FromID: "n1", ToID: "n2", Kind: "relationship"}},
	}, seedIDs: []string{"table:db:public.unrelated"}}
}

func newSelectionAgent(rt GraphRuntime, factory ProgramFactory) *Agent {
	return newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }), WithSelectionProgramFactory(factory))
}

func prepareForTest(t *testing.T, a *Agent, req Request) (*preparationResult, *protocolRuntime) {
	t.Helper()
	protocol := newProtocolRuntime(a.runtime, req.Instruction, req.Namespace, 40, req.Capabilities, nil, CatalogSearchFeatures{})
	p := a.startPreparation(context.Background(), req, allowedSkills(false, req.Capabilities))
	seed, err := protocol.Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p.finish(protocol, seed), protocol
}

func TestPreparationDiscoversOutsideSeedAndRequiresConsumption(t *testing.T) {
	rt := selectionFixture()
	a := newSelectionAgent(rt, selectionFactory(selectRelevant))
	result, protocol := prepareForTest(t, a, Request{Instruction: "show invoice customers"})
	if !reflect.DeepEqual(result.stats.DetailIDs, []string{"saved_query:invoices", "table:db:public.customers", "table:db:public.invoices"}) {
		t.Fatalf("prefetched %v", result.stats.DetailIDs)
	}
	if protocol.state.hasCatalogDetailEvidence() || protocol.state.modelDiscoveryAction {
		t.Fatal("prefetch armed discovery before consumption")
	}
	if _, err := result.consume(protocol); err != nil {
		t.Fatal(err)
	}
	if !protocol.state.tablesDetailed["invoices"] || !protocol.state.tablesDetailed["customers"] || !protocol.state.savedQueriesDetailed["invoices"] {
		t.Fatalf("detail evidence not adopted: %+v", protocol.state.catalogDetails)
	}
	if len(protocol.state.savedQueriesDiscovered) != 0 {
		t.Fatal("prefetch chose an implicit saved-query route")
	}
	if !protocol.state.modelDiscoveryAction || !result.stats.Consumed {
		t.Fatal("consumption did not satisfy discovery")
	}
	if SummarizeUsage(result.usage).TotalTokens == 0 {
		t.Fatal("decision usage missing")
	}
	if got := skillIDs(result.skills); !reflect.DeepEqual(got, []string{skillDataDiscovery, skillDataAggregation}) {
		t.Fatalf("selected guides %v", got)
	}
}

func TestPreparationLargeCatalogUsesScopesBeyondSeed(t *testing.T) {
	rt := selectionFixture()
	for i := 0; i < 60; i++ {
		rt.snapshot.Cards = append(rt.snapshot.Cards, core.CatalogCard{ID: fmt.Sprintf("table:db:public.z%d", i), Kind: "table", Title: fmt.Sprintf("noise%d", i)})
	}
	result, _ := prepareForTest(t, newSelectionAgent(rt, selectionFactory(selectRelevant)), Request{Instruction: "show invoice customers"})
	if len(result.stats.DetailIDs) != 3 {
		t.Fatalf("scope discovery missed target: %+v", result.stats)
	}
}

func TestPreparationFailureAndDeadlineRestoreGuides(t *testing.T) {
	for _, failure := range []string{"provider", "malformed", "deadline", "refusal"} {
		t.Run(failure, func(t *testing.T) {
			rt := selectionFixture()
			factory := selectionFactory(func(ctx context.Context, candidates []map[string]any) (ax.Value, error) {
				if failure == "refusal" {
					out := map[string]ax.Value{}
					for i := range candidates {
						out[fmt.Sprintf("slot%d", i)] = map[string]any{"status": "refused", "provenance": map[string]any{"source": "native"}}
					}
					return out, nil
				}
				if failure == "deadline" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if failure == "malformed" {
					return map[string]ax.Value{}, nil
				}
				return nil, errors.New("unavailable")
			})
			a := newSelectionAgent(rt, factory)
			a.config.Selection.StartupBudgetMS = 20
			started := time.Now()
			result, protocol := prepareForTest(t, a, Request{Instruction: "show invoices"})
			if time.Since(started) > time.Second {
				t.Fatal("preparation did not cancel workers")
			}
			if len(result.stats.Fallbacks) == 0 || !reflect.DeepEqual(skillIDs(result.skills), skillIDs(allowedSkills(false, nil))) {
				t.Fatalf("fallback %+v", result.stats)
			}
			if protocol.state.hasCatalogDetailEvidence() {
				t.Fatal("failed decisions armed schema evidence")
			}
		})
	}
}

func TestPreparationMissingAndChangedDetailsDoNotArmEvidence(t *testing.T) {
	for _, change := range []string{"missing", "revision"} {
		t.Run(change, func(t *testing.T) {
			rt := selectionFixture()
			rt.missing = change == "missing"
			if change == "revision" {
				rt.detailRevision = "revision-2"
			}
			result, protocol := prepareForTest(t, newSelectionAgent(rt, selectionFactory(selectRelevant)), Request{Instruction: "show invoices"})
			result.consume(protocol)
			if len(result.stats.DetailIDs) != 0 || protocol.state.hasCatalogDetailEvidence() {
				t.Fatal("missing/stale details became evidence")
			}
		})
	}
}

func TestPreparationConcurrencyBoundAndOverlap(t *testing.T) {
	var active, peak atomic.Int32
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	factory := selectionFactory(func(ctx context.Context, candidates []map[string]any) (ax.Value, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return selectRelevant(ctx, candidates)
	})
	a := newSelectionAgent(selectionFixture(), factory)
	a.config.Selection.Concurrency = 2
	done := make(chan *preparationResult, 1)
	go func() {
		protocol := newProtocolRuntime(a.runtime, "show invoices", "", 40, nil, nil, CatalogSearchFeatures{})
		p := a.startPreparation(context.Background(), Request{Instruction: "show invoices"}, allowedSkills(false, nil))
		seed, _ := protocol.Seed(context.Background())
		done <- p.finish(protocol, seed)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("independent decisions did not overlap")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers did not finish")
	}
	if peak.Load() > 2 {
		t.Fatalf("concurrency exceeded: %d", peak.Load())
	}
}

func TestSelectionRealAxEvidenceMapping(t *testing.T) {
	for _, kind := range []string{"skills", "rank"} {
		t.Run(kind, func(t *testing.T) {
			answer := map[string]ax.Value{"type": "noul", "noul": 0.85}
			if kind == "rank" {
				answer = map[string]ax.Value{"type": "score", "score": 1.1, "confidence": 0.7, "probabilities": map[string]ax.Value{"0": 0.2, "1": 0.3, "2": 0.5, "3": 0.0}, "legend": map[string]ax.Value{"0": "Irrelevant", "1": "Related", "2": "Useful", "3": "Essential"}}
			}
			transport := ax.NewScriptedTransport([]ax.Value{map[string]ax.Value{"model": "jev-latest", "answers": map[string]ax.Value{"slot0": answer}, "usage": map[string]ax.Value{"input_tokens": 21, "output_tokens": 3}}})
			a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, selectionFixture(), WithClientFactory(func(Config) (ax.AIClient, error) {
				return ax.NewAI("typesafe", map[string]ax.Value{"api_key": "scripted-key", "transport": transport}), nil
			}))
			p := &preparation{agent: a, ctx: context.Background(), req: Request{Instruction: "show invoices"}, config: a.config.Selection, allowed: []skillDefinition{builtinSkills[0]}}
			evidence, usage, err := p.decide(preparationJob{kind: kind, cards: []core.CatalogCard{{ID: "table:invoices", Kind: "table"}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(transport.Requests) != 1 || len(evidence) != 1 || stringFromMap(mapValue(evidence[0]["provenance"]), "source") != "native" {
				t.Fatalf("evidence mapping: %+v", evidence)
			}
			if kind == "rank" && (evidence[0]["value"] != 1.1 || evidence[0]["confidence"] != 0.7) {
				t.Fatalf("native score replaced: %+v", evidence)
			}
			if SummarizeUsage(usage).TotalTokens != 24 {
				t.Fatalf("usage: %+v", usage)
			}
		})
	}
}

func TestRunConsumesPreparedCatalogAndExecutesWithoutDiscoveryTurn(t *testing.T) {
	rt := selectionFixture()
	a := newSelectionAgent(rt, selectionFactory(selectRelevant))
	a.newProgram = func(_ string, options map[string]ax.Value) Program {
		return &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "Found invoice 1."}, onForward: func(p *fakeProgram) {
			if strings.Contains(fmt.Sprint(options["instructionAddenda"]), "data_write is permitted") {
				t.Fatal("permissions expanded by selection")
			}
			runtime := p.forwardOptions["runtime"].(*graphJinCodeRuntime)
			session, err := runtime.CreateSession(map[string]ax.Value{"inputs": p.forwardValues}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			step := session.Execute(`const prepared = await use_prepared_catalog({}); const result = await execute_graphql({query:"query { invoices { id } }"}); console.log(result);`, nil)
			if strings.Contains(stringify(step), "error") {
				t.Fatalf("execute: %+v", step)
			}
		}}
	}
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered || rt.executions != 1 || response.Preparation == nil || !response.Preparation.Consumed {
		t.Fatalf("run %+v executions=%d", response, rt.executions)
	}
	for _, item := range anySlice(response.Actions) {
		action := mapValue(item)
		if action["source"] == "model" && action["tool"] == toolQueryCatalog {
			t.Fatal("redundant model catalog call")
		}
	}
}

func TestSkillSelectionCannotAuthorizeOrHidePermittedRecovery(t *testing.T) {
	permitted := allowedSkills(false, profileWithRoleAndRoots("user", systemRootWatch))
	got := authorizedSkillValues(permitted, []ax.Value{skillWatchWrite, skillAdminWrite, "invented"})
	if ids := optionSkillIDs(t, got); !reflect.DeepEqual(ids, []string{skillWatchWrite}) {
		t.Fatalf("authorized load %v", ids)
	}
	text := capabilityCompletionInstructions(permitted, profileWithRoleAndRoots("user", systemRootWatch))
	if !strings.Contains(text, "watch_write is permitted") || strings.Contains(text, "admin_write is permitted") {
		t.Fatalf("permissions: %s", text)
	}
}

func TestDiscoveryIndexRestrictsCardsAndGraphEndpoints(t *testing.T) {
	rt := selectionFixture()
	index := NewCatalogDiscoveryIndex(rt.snapshot)
	visible := *rt.snapshot
	visible.Cards = visible.Cards[:1]
	filtered := index.Restrict(&visible)
	if len(filtered.Cards) != 1 || len(filtered.Neighbors[filtered.Cards[0].ID]) != 0 {
		t.Fatalf("hidden graph endpoint leaked: %+v", filtered)
	}
	// Composed table summaries must not retain a column hidden by a caller
	// projection, even if both projections share the source revision.
	rt.snapshot.Cards = append(rt.snapshot.Cards, core.CatalogCard{ID: "column:secret", Kind: "column", TableName: "invoices", ColumnName: "private_bank_account"})
	index = NewCatalogDiscoveryIndex(rt.snapshot)
	filtered = index.Restrict(&visible)
	if strings.Contains(filtered.Cards[0].Summary, "private_bank_account") {
		t.Fatal("hidden column leaked through table summary")
	}
}

func TestSelectionModesAndOnDemandDetails(t *testing.T) {
	for _, mode := range []string{"catalog", "skills"} {
		t.Run(mode, func(t *testing.T) {
			a := newSelectionAgent(selectionFixture(), selectionFactory(selectRelevant))
			a.config.Selection.Mode = mode
			result, _ := prepareForTest(t, a, Request{Instruction: "show invoices"})
			if mode == "catalog" && !reflect.DeepEqual(skillIDs(result.skills), skillIDs(allowedSkills(false, nil))) {
				t.Fatal("catalog mode pruned guides")
			}
			if mode == "skills" && len(result.reads) != 0 {
				t.Fatal("skill mode read catalog details")
			}
		})
	}
	// A later explicit search gets details in its own response; no extra actor
	// call is needed to fetch or adopt them.
	a := newSelectionAgent(selectionFixture(), selectionFactory(selectRelevant))
	result, protocol := prepareForTest(t, a, Request{Instruction: "show invoices"})
	result.reads = nil
	result.stats.ContextBytes = 0
	result.stats.DetailIDs = nil
	protocol.enrichCatalog = func(ctx context.Context, args map[string]any, out any) any {
		return a.enrichCatalog(ctx, Request{Instruction: "show invoices"}, protocol, result, out)
	}
	protocol.base.(*selectionRuntime).seedIDs = []string{"table:db:public.invoices"}
	out, err := protocol.QueryCatalog(context.Background(), map[string]any{"search": "invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if mapValue(out)["prefetched_catalog"] == nil || !protocol.state.tablesDetailed["invoices"] || result.stats.OnDemandMS < 0 {
		t.Fatalf("search did not enrich/adopt: %+v", out)
	}
}

func TestSelectionContextBudgetDoesNotArmDiscardedDetails(t *testing.T) {
	rt := selectionFixture()
	rt.snapshot.Cards[0].EvidenceJSON = strings.Repeat("x", 2048)
	a := newSelectionAgent(rt, selectionFactory(selectRelevant))
	a.config.Selection.ContextBytes = 1024
	result, protocol := prepareForTest(t, a, Request{Instruction: "show invoices"})
	result.consume(protocol)
	if result.stats.ContextBytes > 1024 || protocol.state.tablesDetailed["invoices"] {
		t.Fatalf("oversized evidence adopted: %+v", result.stats)
	}
}

func TestSelectionOpenAIDecisionsUsesSameAxContract(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{map[string]ax.Value{
		"model": "gpt-6-luna",
		"answers": []ax.Value{map[string]ax.Value{"name": "slot0", "type": "score", "score": 1.1, "confidence": 0.7, "probabilities": []ax.Value{
			map[string]ax.Value{"value": 0, "label": "Irrelevant", "probability": 0.2},
			map[string]ax.Value{"value": 1, "label": "Related", "probability": 0.3},
			map[string]ax.Value{"value": 2, "label": "Useful", "probability": 0.5},
			map[string]ax.Value{"value": 3, "label": "Essential", "probability": 0.0},
		}}},
		"usage": map[string]ax.Value{"input_tokens": 21, "output_tokens": 3, "total_tokens": 24, "input_tokens_details": map[string]ax.Value{"cached_tokens": 0, "cache_write_tokens": 0}, "output_tokens_details": map[string]ax.Value{"reasoning_tokens": 0}},
	}})
	a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "openai-decisions"}}, selectionFixture(), WithClientFactory(func(Config) (ax.AIClient, error) {
		return ax.NewAI("openai-decisions", map[string]ax.Value{"api_key": "scripted-key", "transport": transport}), nil
	}))
	p := &preparation{agent: a, ctx: context.Background(), req: Request{Instruction: "show invoices"}, config: a.config.Selection}
	evidence, usage, err := p.decide(preparationJob{kind: "rank", cards: []core.CatalogCard{{ID: "table:invoices", Kind: "table"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.Requests) != 1 || evidence[0]["value"] != 1.1 || evidence[0]["confidence"] != 0.7 || SummarizeUsage(usage).TotalTokens != 24 {
		t.Fatalf("contract: evidence=%+v usage=%+v", evidence, usage)
	}
}

func TestPreparedEvidenceSurvivesRealAxStageHandoff(t *testing.T) {
	rt := selectionFixture()
	client := &recordingClient{responses: []string{
		`{"javascriptCode":"const prepared = await use_prepared_catalog({}); await final('Show the invoices from live data.', {prepared});"}`,
		`{"javascriptCode":"void graphjinDistilledContext.prepared; const execution = await execute_graphql({query:'query { invoices { id } }'}); await final('Answer from the returned invoice data.', {execution});"}`,
		`{"status":"answered","answer":"Found invoice 1."}`,
	}}
	a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithSelectionProgramFactory(selectionFactory(selectRelevant)))
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered || rt.executions != 1 || !response.Preparation.Consumed {
		t.Fatalf("handoff: %+v", response)
	}
	if len(client.calls) != 3 {
		t.Fatalf("expected one distiller/executor/responder call each, got %d", len(client.calls))
	}
	for _, call := range client.calls {
		prompt := dumpAXValue(call.values)
		if strings.Contains(prompt, "invoice amounts") {
			t.Fatal("full prepared schema leaked into staged prompt")
		}
	}
}

func TestRealAxCanRecoverAnOmittedPermittedGuide(t *testing.T) {
	rt := selectionFixture()
	client := &recordingClient{responses: []string{
		`{"javascriptCode":"const prepared = await use_prepared_catalog({}); await final('Show the invoices from live data.', {prepared});"}`,
		`{"javascriptCode":"void graphjinDistilledContext.prepared; await discover({skills:['code_read']});"}`,
		`{"javascriptCode":"await used('code_read', 'Applied the loaded guide');"}`,
		`{"javascriptCode":"const execution = await execute_graphql({query:'query { invoices { id } }'}); await final('Answer from the returned invoice data.', {execution});"}`,
		`{"status":"answered","answer":"Found invoice 1."}`,
	}}
	a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt, WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }), WithSelectionProgramFactory(selectionFactory(selectRelevant)))
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered || response.Preparation.SkillReloads != 1 || len(response.Skills) != 1 || response.Skills[0].ID != skillCodeRead {
		t.Fatalf("guide recovery: status=%s reloads=%d used=%+v calls=%d", response.Status, response.Preparation.SkillReloads, response.Skills, len(client.calls))
	}
	if len(client.calls) != 5 || !strings.Contains(dumpAXValue(client.calls[2].values), "Skill: code_read") {
		t.Fatal("loaded guide absent from next executor prompt")
	}
}
