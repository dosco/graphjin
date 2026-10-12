package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

type preparedRecordingClient struct{ recordingClient }

func (c *preparedRecordingClient) Chat(ctx context.Context, values, options map[string]ax.Value) (ax.Value, error) {
	out, err := c.recordingClient.Chat(ctx, values, options)
	if err == nil {
		out.(map[string]ax.Value)["modelUsage"] = map[string]ax.Value{"tokens": map[string]ax.Value{"promptTokens": 10, "completionTokens": 2, "totalTokens": 12}}
	}
	return out, err
}

func TestPreparedRequestExecutesOnFirstCoreModelTurn(t *testing.T) {
	rt := selectionFixture()
	// Keep a distractor in the seed, with one relevant executable root.
	rt.snapshot.Cards = append(rt.snapshot.Cards[:1:1], rt.snapshot.Cards[3])
	rt.snapshot.Nodes, rt.snapshot.Edges = nil, nil
	client := &preparedRecordingClient{recordingClient{responses: []string{
		`{"javascriptCode":"const preferred = inputs.context._graphjin_prepared.preferred; await query_catalog({id:preferred.id}); const execution = await execute_graphql({query:'query { invoices { id } }'}); await final('Answer from the returned invoice data.', {execution});"}`,
		`{"status":"answered","answer":"Found invoice 1."}`,
	}}}
	a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithSelectionProgramFactory(selectionFactory(selectRelevant)))
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered || rt.executions != 1 || !response.Preparation.Consumed || !response.Preparation.PreparedHandoff {
		t.Fatalf("first-turn execution: %+v", response)
	}
	if len(client.calls) != 2 || response.Preparation.DetailCacheHits != 1 {
		t.Fatalf("want executor/responder only with a local schema read, calls=%d preparation=%+v", len(client.calls), response.Preparation)
	}
	usage := SummarizeUsage(response.Usage)
	if usage.LLMCalls-response.Preparation.Usage.LLMCalls != 2 || usage.TotalTokens-response.Preparation.Usage.TotalTokens != 24 {
		t.Fatalf("local handoff counted as paid inference: usage=%+v selection=%+v", usage, response.Preparation.Usage)
	}
	firstPrompt := dumpAXValue(client.calls[0].values)
	if !strings.Contains(firstPrompt, "The model plans, the database computes.") || !strings.Contains(firstPrompt, "table:db:public.invoices") {
		t.Fatal("first execution prompt lacks selected guides or root")
	}
	for _, call := range client.calls {
		if strings.Contains(dumpAXValue(call.values), "invoice amounts") {
			t.Fatal("full prepared schema leaked into staged prompt")
		}
	}
}

func TestPreparedHandoffDoesNotGrantDiscoveryOrExecutionEvidence(t *testing.T) {
	rt := selectionFixture()
	client := &recordingClient{responses: []string{
		`{"javascriptCode":"await execute_graphql({query:'query { invoices { id } }'}); await final('Answer from execution.', {});"}`,
	}}
	a := newAgent(Config{MaxSteps: 1, Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithSelectionProgramFactory(selectionFactory(selectRelevant)))
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if rt.executions != 0 || response.Status == StatusAnswered || response.Preparation.Consumed || !response.Preparation.PreparedHandoff {
		t.Fatalf("unconsumed prepared facts authorized execution: %+v executions=%d", response, rt.executions)
	}
}

func TestPreparedSavedQueryExecutesOnFirstCoreModelTurn(t *testing.T) {
	rt := selectionFixture()
	client := &recordingClient{responses: []string{
		`{"javascriptCode":"const preferred = inputs.context._graphjin_prepared.preferred; await query_catalog({id:preferred.id}); const execution = await execute_saved_query({name:preferred.name}); await final('Answer from the returned invoice data.', {execution});"}`,
		`{"status":"answered","answer":"Found invoice 1."}`,
	}}
	a := newAgent(Config{Selection: SelectionConfig{Enabled: true, Provider: "typesafe"}}, rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithSelectionProgramFactory(selectionFactory(selectRelevant)))
	response, err := a.Run(context.Background(), Request{Instruction: "show invoices"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != StatusAnswered || rt.executions != 1 || len(client.calls) != 2 || response.Preparation.DetailCacheHits != 1 {
		t.Fatalf("saved-query first-turn execution: %+v calls=%d executions=%d", response, len(client.calls), rt.executions)
	}
}

func TestPreparedHandoffEligibility(t *testing.T) {
	for _, test := range []struct {
		name       string
		req        Request
		invalidate bool
		empty      bool
	}{
		{name: "history", req: Request{History: []Turn{{Role: "user", Content: "previous request"}}}},
		{name: "caller context", req: Request{Context: map[string]any{"report": "interpret me"}}},
		{name: "changed revision", invalidate: true},
		{name: "no prepared roots", empty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := selectionFixture()
			prepared, _ := prepareForTest(t, newSelectionAgent(rt, selectionFactory(selectRelevant)), Request{Instruction: "show invoices"})
			if test.invalidate {
				rt.snapshot.Revision = "revision-2"
			}
			if test.empty {
				prepared.reads = nil
			}
			program := ax.NewAgent(agentSignature, map[string]ax.Value{"runtime": map[string]ax.Value{"language": "JavaScript"}})
			installPreparedHandoff(context.Background(), program, prepared, rt, test.req)
			if prepared.stats.PreparedHandoff || program.Distiller.Options["cachingFunction"] != nil {
				t.Fatal("ineligible request skipped context interpretation")
			}
		})
	}
}
