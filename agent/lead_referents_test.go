package agent

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// Conversations and delegated questions from the lead-corpus runs of
// 2026-11-06/07 (flash-lite): the user's follow-up, the earlier exchange, and
// what the lead's code actually sent to the data agent.
var (
	renewalHistory = []Turn{
		{Role: "user", Content: "Which account has the soonest upcoming renewal?"},
		{Role: "assistant", Content: "The account with the soonest upcoming renewal is Meridian Robotics, with a renewal date of November 16, 2026.", Status: StatusAnswered},
	}
	churnHistory = []Turn{
		{Role: "user", Content: "Which accounts are at churn risk right now?"},
		{Role: "assistant", Content: "Two accounts are at churn risk: Meridian Robotics and Quartzline Analytics.", Status: StatusAnswered},
	}
	planHistory = []Turn{
		{Role: "user", Content: "Which plan tier brings in the most total monthly recurring revenue?"},
		{Role: "assistant", Content: "The enterprise plan brings in the most, $16,100.00 a month across 3 accounts.", Status: StatusAnswered},
	}
)

func TestLeadReferentCheckHoldsBackUnresolvedFollowUps(t *testing.T) {
	cases := []struct {
		name        string
		instruction string
		history     []Turn
		question    string
		want        []string
	}{
		{"the user's question forwarded as-is", "What is the total amount of their failed invoices?", renewalHistory,
			"What is the total amount of their failed invoices?", []string{"their"}},
		{"a paraphrase that keeps the pronoun", "What is the total amount of their failed invoices?", renewalHistory,
			"Show me all failed invoices and their amounts.", []string{"their"}},
		{"a determiner phrase", "What is the average monthly recurring revenue of the accounts on that plan?", planHistory,
			"What is the average monthly recurring revenue of the accounts on that plan?", []string{"that plan"}},
		{"an acronym is not a name", "What is the average MRR of the accounts on that plan?", planHistory,
			"What is the average MRR of the accounts on that plan?", []string{"that plan"}},
		{"a contraction is not a quoted value", "What's the total of their failed invoices?", renewalHistory,
			"What's the total of their failed invoices?", []string{"their"}},
		{"the reference dropped, widening to every account", "What is the total amount of their failed invoices?", renewalHistory,
			"What is the total amount of all failed invoices across all accounts?", []string{"their"}},
		{"the reference dropped, one row per account", "What is their combined monthly recurring revenue?", churnHistory,
			"What is the monthly recurring revenue for each account?", []string{"their"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := newLeadReferentCheck(tc.instruction, tc.history).unresolved(tc.question)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unresolved(%q) = %v, want %v", tc.question, got, tc.want)
			}
		})
	}
}

func TestLeadReferentCheckLetsStandaloneQuestionsThrough(t *testing.T) {
	cases := []struct {
		name        string
		instruction string
		history     []Turn
		question    string
	}{
		{"resolved to the name", "What is the total amount of their failed invoices?", renewalHistory,
			"What is the total amount of failed invoices for Meridian Robotics?"},
		{"resolved with a possessive", "What is the total amount of their failed invoices?", renewalHistory,
			"What is the total of Meridian Robotics' failed invoices and their amounts?"},
		{"resolved to a plan word from the conversation", "What is the average monthly recurring revenue of the accounts on that plan?", planHistory,
			"What is the average monthly recurring revenue of the accounts on the enterprise plan?"},
		{"resolved to both names", "What is their combined monthly recurring revenue?", churnHistory,
			"What is the combined monthly recurring revenue of Meridian Robotics and Quartzline Analytics?"},
		{"resolved by the conversation's own words", "What is their combined monthly recurring revenue?", churnHistory,
			"What is the combined monthly recurring revenue of the accounts at churn risk?"},
		{"resolved to an id", "What is the total amount of their failed invoices?", renewalHistory,
			"What is the total amount of failed invoices for account 1?"},
		{"the earlier question asked again carries no reference", "What is the average monthly recurring revenue of the accounts on that plan?", planHistory,
			"Which plan tier brings in the most total monthly recurring revenue?"},
		{"a pronoun the user never used", "What is the total amount of Meridian Robotics' failed invoices?", renewalHistory,
			"List the failed invoices with their amounts."},
		{"no conversation", "What is the total amount of their failed invoices?", nil,
			"What is the total amount of their failed invoices?"},
		{"a follow-up that refers to nothing", "How many open tickets are there?", renewalHistory,
			"How many open tickets are there?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := newLeadReferentCheck(tc.instruction, tc.history).unresolved(tc.question); got != nil {
				t.Fatalf("unresolved(%q) = %v, want none", tc.question, got)
			}
		})
	}
}

// A question carrying the user's reference is held back with an error the
// lead's code sees; restated, it reaches the data agent. Asking the same
// question again also goes through: the check runs once per turn.
func TestLeadHoldsBackAnUnresolvedQuestionOnce(t *testing.T) {
	dangling := "What is the total amount of their failed invoices?"
	restated := "What is the total amount of failed invoices for Meridian Robotics?"
	run := func(retry string) ([]string, string) {
		client := &scriptedLeadClient{responses: []ax.Value{
			leadCode(`final({ plan: "total their failed invoices" })`),
			leadCode(`const r = await ask_graphjin({ questions: ["` + dangling + `"] }); console.log(JSON.stringify(r))`),
			leadCode(`const r2 = await ask_graphjin({ questions: ["` + retry + `"] }); console.log(JSON.stringify(r2))`),
			leadCode(`final("answer with the total")`),
			leadReply(StatusAnswered, "Meridian Robotics has $9,600.00 in failed invoices."),
		}}
		var mu sync.Mutex
		var asked []string
		lead := newTestLead(t, Config{}, client, func(_ context.Context, d DelegateRequest) (Response, error) {
			mu.Lock()
			asked = append(asked, d.Question)
			mu.Unlock()
			return Response{Status: StatusAnswered, Answer: "Meridian Robotics has $9,600.00 in failed invoices."}, nil
		})
		if _, err := lead.Run(context.Background(), Request{Instruction: dangling, History: renewalHistory}); err != nil {
			t.Fatal(err)
		}
		return asked, strings.Join(client.requests, "\n")
	}

	asked, prompts := run(restated)
	if !reflect.DeepEqual(asked, []string{restated}) {
		t.Fatalf("data agent was asked %q, want only the restated question", asked)
	}
	if !strings.Contains(prompts, "sees none of this conversation") {
		t.Fatal("the lead's code never saw why the question was held back")
	}

	asked, _ = run(dangling)
	if !reflect.DeepEqual(asked, []string{dangling}) {
		t.Fatalf("data agent was asked %q, want the repeated question to go through", asked)
	}
}

// The refusal names who the conversation was about, so the lead can restate
// the question instead of dropping the reference.
func TestLeadReferentRefusalNamesTheConversationsSubjects(t *testing.T) {
	history := append([]Turn{{Role: "user", Content: "Compare Solvex Dynamics with The Harborlight Systems."}}, renewalHistory...)
	c := newLeadReferentCheck("What is the total amount of their failed invoices?", history)
	err := c.refusal("What is the total amount of their failed invoices?", []string{"their"}, true)
	if !strings.Contains(err.Error(), "The conversation names Meridian Robotics, Solvex Dynamics, Harborlight Systems.") {
		t.Fatalf("refusal = %v", err)
	}
	if got := conversationNames(planHistory); got != nil {
		t.Fatalf("names = %v, want none from a conversation without proper names", got)
	}
}
