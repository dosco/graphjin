package agent

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// The lead's prompt lists its callables qualified, and code written that way
// must reach them: the 2026-11-07 stage dump shows the context stage calling
// utils.ask_graphjin three times and failing with "utils is not defined".
func TestLeadCallablesAnswerUnderTheNameThePromptAdvertises(t *testing.T) {
	question := "Which account has the soonest upcoming renewal after 2026-11-07?"
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`const r = await utils.ask_graphjin({ questions: ["` + question + `"] }); console.log(JSON.stringify(r)); final("answer with the account", { r })`),
		leadCode(`final("answer with the account")`),
		leadReply(StatusAnswered, "Meridian Robotics renews soonest, on 2026-11-16."),
	}}
	var mu sync.Mutex
	var asked []string
	lead := newTestLead(t, Config{}, client, func(_ context.Context, d DelegateRequest) (Response, error) {
		mu.Lock()
		asked = append(asked, d.Question)
		mu.Unlock()
		return Response{Status: StatusAnswered, Answer: "Meridian Robotics renews soonest, on 2026-11-16."}, nil
	})
	if _, err := lead.Run(context.Background(), Request{Instruction: "Which account has the soonest upcoming renewal?"}); err != nil {
		t.Fatal(err)
	}
	prompts := strings.Join(client.requests, "\n")
	if !strings.Contains(prompts, axFlatFunctionNamespace+".ask_graphjin(") {
		t.Fatalf("the prompt no longer advertises %s.ask_graphjin; update axFlatFunctionNamespace to what it shows", axFlatFunctionNamespace)
	}
	if strings.Contains(prompts, "is not defined") {
		t.Fatal("the qualified call failed in the runtime")
	}
	if !reflect.DeepEqual(asked, []string{question}) {
		t.Fatalf("data agent was asked %q, want the qualified call's question", asked)
	}
}

// A delegation outlasting goja's default 5s code budget still returns its
// answer to the code that asked: the default interrupted the block as soon
// as the slow call returned, discarding the answer, and the lead asked again.
func TestLeadSlowDelegationReturnsItsAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out goja's default code budget")
	}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`const r = await ask_graphjin({ questions: ["Which account renews soonest?"] }); console.log(JSON.stringify(r)); final("answer", { r })`),
		leadCode(`final("answer")`),
		leadReply(StatusAnswered, "Meridian Robotics renews soonest."),
	}}
	lead := newTestLead(t, Config{}, client, func(_ context.Context, d DelegateRequest) (Response, error) {
		time.Sleep(6 * time.Second)
		return Response{Status: StatusAnswered, Answer: "Meridian Robotics renews soonest."}, nil
	})
	if _, err := lead.Run(context.Background(), Request{Instruction: "Which account renews soonest?"}); err != nil {
		t.Fatal(err)
	}
	prompts := strings.Join(client.requests, "\n")
	if strings.Contains(prompts, "timed out") {
		t.Fatal("a slow delegation was interrupted after it answered")
	}
}
