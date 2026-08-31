package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

type AuthorProposal struct {
	Status         string       `json:"status"`
	Clarification  string       `json:"clarification,omitempty"`
	Interpretation string       `json:"interpretation,omitempty"`
	Category       Category     `json:"category,omitempty"`
	Difficulty     Difficulty   `json:"difficulty,omitempty"`
	Oracle         *OracleSpec  `json:"oracle,omitempty"`
	Answer         AnswerRule   `json:"answer,omitempty"`
	Method         MethodRule   `json:"method,omitempty"`
	Behavior       BehaviorRule `json:"behavior,omitempty"`
}

type Author struct {
	Client   HTTPDoer
	Verifier Verifier
}

func (a Author) Propose(ctx context.Context, baseURL string, headers map[string]string, question string) (Task, OracleResult, AuthorProposal, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Task{}, OracleResult{}, AuthorProposal{}, fmt.Errorf("question is empty")
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	instruction := `You are authoring one hidden GraphJin evaluation task, not answering the business question for the user. Use catalog discovery to resolve real table and field names. Never invent a business definition. If the question is ambiguous, do not guess: return status needs_clarification and one concise clarification question. Otherwise propose a read-only GraphQL oracle that computes the answer in the database. Return only one JSON object with this exact shape: {"status":"ready","interpretation":"plain language interpretation","category":"aggregate|ranking|window|traversal|saved-metric|discovery","difficulty":"T1|T2|T3|T4","oracle":{"query":"query ...","variables":{},"extract":"root.0.field","dimension_extract":"optional"},"answer":{"kind":"number|string|date","tolerance_pct":0,"accept_scales":[]},"method":{"require_query_match":["regex"],"forbid_finalize_from_list_only":true}}. The oracle is hidden from the evaluated agent and must be read-only. Business question: ` + question
	response, _, _, err := postAgent(ctx, client, baseURL, headers, instruction, nil)
	if err != nil {
		return Task{}, OracleResult{}, AuthorProposal{}, err
	}
	proposal, err := decodeAuthorProposal(response)
	if err != nil {
		return Task{}, OracleResult{}, AuthorProposal{}, err
	}
	if proposal.Status == "needs_clarification" {
		return Task{}, OracleResult{}, proposal, nil
	}
	if proposal.Status != "ready" || proposal.Oracle == nil {
		return Task{}, OracleResult{}, proposal, fmt.Errorf("authoring model returned incomplete proposal status %q", proposal.Status)
	}
	task := Task{
		Slug:              "user-" + question,
		Category:          proposal.Category,
		Difficulty:        proposal.Difficulty,
		Prompt:            question,
		Provenance:        Provenance{Source: "user-added"},
		CapabilityProfile: CapabilityProfile{RoleClass: "user", ReadOnly: true},
		ExpectedStatus:    gjagent.StatusAnswered,
		Oracle:            proposal.Oracle,
		Answer:            proposal.Answer,
		Method:            proposal.Method,
		Behavior:          proposal.Behavior,
	}
	if len(task.Behavior.RequiredActions) == 0 {
		task.Behavior.RequiredActions = []string{"query_catalog", "execute_graphql"}
	}
	if len(task.Behavior.ForbiddenActions) == 0 {
		task.Behavior.ForbiddenActions = []string{"execute_graphql:mutation"}
	}
	if err := task.Normalize(); err != nil {
		return Task{}, OracleResult{}, proposal, err
	}
	verifier := a.Verifier
	verifier.Client = client
	verifier.BaseURL = baseURL
	verifier.Headers = headers
	result, err := verifier.Resolve(ctx, *task.Oracle)
	if err != nil {
		return Task{}, OracleResult{}, proposal, fmt.Errorf("proposed oracle did not compile and execute: %w", err)
	}
	return task, result, proposal, nil
}

func decodeAuthorProposal(response gjagent.Response) (AuthorProposal, error) {
	var proposal AuthorProposal
	if mapped := toMap(response.Data); len(mapped) != 0 {
		data, _ := json.Marshal(mapped)
		if json.Unmarshal(data, &proposal) == nil && proposal.Status != "" {
			return proposal, nil
		}
	}
	if err := decodeFencedJSON(response.Answer, &proposal); err != nil {
		return proposal, err
	}
	return proposal, nil
}

// DecodeFencedJSON pulls one JSON value out of a model reply, for callers
// outside this package that ask a model for JSON and get prose around it.
func DecodeFencedJSON(text string, out any) error { return decodeFencedJSON(text, out) }

// decodeFencedJSON pulls one JSON value out of a model reply.
//
// Models wrap JSON in code fences, and often say something either side of it.
// Rather than demand clean output, this finds the outermost JSON value in the
// text: the first opening bracket through the last matching close. Both object
// and array replies are accepted, because a call that asks for several picks
// answers with an array.
func decodeFencedJSON(text string, out any) error {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")

	object := jsonSpan(text, '{', '}')
	array := jsonSpan(text, '[', ']')
	span := object
	// Whichever value starts first is the reply; an object containing an array
	// must not be mistaken for the array it contains, and vice versa.
	if array.ok && (!object.ok || array.start < object.start) {
		span = array
	}
	if !span.ok {
		return fmt.Errorf("model reply did not contain a JSON value")
	}
	if err := json.Unmarshal([]byte(text[span.start:span.end+1]), out); err != nil {
		return fmt.Errorf("decode model reply: %w", err)
	}
	return nil
}

type jsonBounds struct {
	start, end int
	ok         bool
}

func jsonSpan(text string, open, close byte) jsonBounds {
	start, end := strings.IndexByte(text, open), strings.LastIndexByte(text, close)
	if start < 0 || end < start {
		return jsonBounds{}
	}
	return jsonBounds{start: start, end: end, ok: true}
}
