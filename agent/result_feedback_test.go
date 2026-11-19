package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// feedbackRuntime counts the look-ups and queries a run makes, and remembers
// how many model calls had been made when each query ran.
type feedbackRuntime struct {
	fakeRuntime
	client       *recordingClient
	lookups      int
	queries      int
	queryAtCalls []int
	queryErr     error
}

func (r *feedbackRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if len(detailIDsFromArgs(args)) != 0 {
		r.lookups++
	}
	return r.fakeRuntime.QueryCatalog(ctx, args)
}

func (r *feedbackRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.queries++
	r.queryAtCalls = append(r.queryAtCalls, len(r.client.calls))
	if r.queryErr != nil {
		err := r.queryErr
		r.queryErr = nil
		return nil, err
	}
	return map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"id": 1, "name": "Meridian Robotics"}}}}, nil
}

const (
	// The program models write today: look up, query and finish in one go.
	oneProgram     = `{"javascriptCode":"const d = await query_catalog({id:'table:app:main.accounts'}); const r = await execute_graphql({query:'query { accounts { id name } }'}); await final('Answer from the rows.', {rows: r.data});"}`
	queryAndFinish = `{"javascriptCode":"const r = await execute_graphql({query:'query { accounts { id name } }'}); await final('Answer from the rows.', {rows: r.data});"}`
	finishOnly     = `{"javascriptCode":"await final('Answer from the rows.', {rows: r.data});"}`
	reply          = `{"status":"answered","answer":"Meridian Robotics"}`
)

func runWithFeedback(t *testing.T, mode string, rt *feedbackRuntime, responses ...string) *recordingClient {
	t.Helper()
	rec := &recordingClient{responses: responses}
	rt.client = rec
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 6},
		rt,
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
		WithResultFeedback(mode),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "Which accounts do we have?", Capabilities: profileWithRoleAndRoots("user")}); err != nil {
		t.Fatalf("%q: %v", mode, err)
	}
	return rec
}

// Off, the whole program runs: the query is sent in the same model call that
// asked for the table's detail, so it was written before that detail was read.
// With result feedback the look-up ends the block: the query runs only after
// a model call that was shown what the look-up returned.
func TestResultFeedbackShowsALookupBeforeTheQueryRuns(t *testing.T) {
	off := &feedbackRuntime{}
	rec := runWithFeedback(t, "", off, oneProgram, reply)
	if off.lookups != 1 || off.queries != 1 || off.queryAtCalls[0] != 1 {
		t.Fatalf("off: lookups=%d queries=%d ran after %v model calls; want one program doing both", off.lookups, off.queries, off.queryAtCalls)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("off: %d model calls, want 2", len(rec.calls))
	}

	for mode, script := range map[string][]string{
		"lookups": {oneProgram, queryAndFinish, reply},
		"all":     {oneProgram, queryAndFinish, finishOnly, reply},
	} {
		rt := &feedbackRuntime{}
		rec := runWithFeedback(t, mode, rt, script...)
		if rt.lookups != 1 || rt.queries != 1 {
			t.Fatalf("%s: lookups=%d queries=%d, want one of each", mode, rt.lookups, rt.queries)
		}
		if rt.queryAtCalls[0] != 2 {
			t.Fatalf("%s: the query ran after %d model calls, want it held until the second", mode, rt.queryAtCalls[0])
		}
		if len(rec.calls) != len(script) {
			t.Fatalf("%s: %d model calls, want %d", mode, len(rec.calls), len(script))
		}
		// The second model call is shown what the look-up returned, without
		// the program having printed it.
		second := dumpAXValue(rec.calls[1].values)
		for _, want := range []string{"table:app:main.accounts", "Execution paused before"} {
			if !strings.Contains(second, want) {
				t.Errorf("%s: the model call after the look-up does not carry %q", mode, want)
			}
		}
		if mode == "all" {
			third := dumpAXValue(rec.calls[2].values)
			if !strings.Contains(third, "Meridian Robotics") {
				t.Errorf("all: the model call after the query was not shown its rows")
			}
		}
	}
}

// A function's error is a message for the model. Run through Ax's own
// registration it used to end the run; handed over as a plain record it is
// returned to the model's code, shown on the next turn, and the run goes on,
// whichever name the code called the function by.
func TestResultFeedbackReturnsAFunctionErrorToTheModel(t *testing.T) {
	const hint = "this call did NOT return data: field 'amount' is not a column; available columns: id, name, mrr_cents"
	for _, name := range []string{"execute_graphql", axFlatFunctionNamespace + ".execute_graphql"} {
		broken := `{"javascriptCode":"const r = await ` + name + `({query:'query { accounts { amount } }'}); await final('Answer from the rows.', {rows: r.data});"}`
		rt := &feedbackRuntime{queryErr: errors.New(hint)}
		rt.catalogOverride = nil
		rec := runWithFeedback(t, "lookups", rt, `{"javascriptCode":"const d = await query_catalog({id:'table:app:main.accounts'});"}`, broken, queryAndFinish, reply)
		if rt.queries != 2 {
			t.Fatalf("%s: %d queries ran, want the failed one and the repaired one", name, rt.queries)
		}
		if len(rec.calls) != 4 {
			t.Fatalf("%s: %d model calls, want 4 (the error must not end the run)", name, len(rec.calls))
		}
		if after := dumpAXValue(rec.calls[2].values); !strings.Contains(after, "available columns: id, name, mrr_cents") {
			t.Errorf("%s: the model call after the failed query was not shown the error", name)
		}
	}
}

// The stages that write code are still given GraphJin's instructions, now
// through the runtime Ax is built with, and the sentences that told the model
// to look up and query in one program are gone from them.
func TestResultFeedbackRewritesTheOneProgramInstructions(t *testing.T) {
	sources := map[string]string{
		"agentSignature":                agentSignature,
		"runtimeSeedUsageInstructions":  runtimeSeedUsageInstructions,
		"preparedDiscoveryInstructions": preparedDiscoveryInstructions,
	}
	for _, sentence := range feedbackRewrites {
		if !strings.Contains(sources[sentence.in], sentence.old) {
			t.Errorf("%s no longer holds the sentence a rewrite replaces: %q", sentence.in, sentence.old)
		}
	}
	for _, mode := range []resultFeedback{feedbackLookups, feedbackAll} {
		for name, text := range sources {
			if got := mode.rewrite(text); strings.Contains(got, "same code block") {
				t.Errorf("%s, %s: still tells the model to act in the same code block", mode, name)
			}
		}
	}
	if got := feedbackOff.rewrite(agentSignature); got != agentSignature {
		t.Error("off must leave the instructions as they are")
	}

	rt := &feedbackRuntime{}
	rec := runWithFeedback(t, "lookups", rt, oneProgram, queryAndFinish, reply)
	first := dumpAXValue(rec.calls[0].values)
	for _, want := range []string{"goja runtime profile", "end the block", "Tool results use"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first code-stage prompt does not carry %q", want)
		}
	}
	if strings.Contains(first, "same code block") {
		t.Error("the first code-stage prompt still says to act in the same code block")
	}
}

func TestResultFeedbackPolicies(t *testing.T) {
	for _, test := range []struct {
		mode           resultFeedback
		function, want string
	}{
		{feedbackLookups, "query_catalog", "inspect"},
		{feedbackLookups, "graphql_help", "inspect"},
		{feedbackLookups, "execute_graphql", "data"},
		{feedbackAll, "execute_graphql", "inspect"},
	} {
		if got := test.mode.policy(test.function); got != test.want {
			t.Errorf("%s, %s: policy %q, want %q", test.mode, test.function, got, test.want)
		}
	}
}
