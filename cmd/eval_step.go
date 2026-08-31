package main

import (
	"context"
	"errors"
	"fmt"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Letting a trainer supply the model's side of an episode.
//
// The usual arrangement points GraphJin at an inference endpoint and lets it
// call out. A training loop often cannot work that way: the weights it is
// updating live inside its own process, and standing up an HTTP server in front
// of them just to be called back is a lot of machinery to get a completion
// across a function boundary.
//
// So this inverts it. The episode runs exactly as it always does — same reset,
// same setup, same grading — but when the agent needs a completion the call is
// parked and handed out as an observation. The trainer sends the completion
// back and the episode resumes. Nothing about how it is scored changes, which
// is the point: a policy trained this way is measured by the same contract as
// one measured over the network.

// mailboxRequest is one parked model call.
type mailboxRequest struct {
	Stage   string
	Values  map[string]ax.Value
	Options map[string]ax.Value
	Reply   chan mailboxReply
}

// mailboxReply is the trainer's answer. The token counts are optional and are
// reported rather than trusted: efficiency is a term in the reward, and a
// trainer that omitted them would otherwise silently score as free.
type mailboxReply struct {
	Completion       string
	PromptTokens     int64
	CompletionTokens int64
}

// stepMailbox hands parked calls to whoever is driving the episode.
//
// One mailbox belongs to one world, and a world serves one episode at a time,
// so a parked call belongs unambiguously to the episode holding that world. No
// correlation identifiers, and no way for two episodes to answer each other's
// calls.
type stepMailbox struct {
	requests chan *mailboxRequest
	// support optionally answers the stages the trainer is not teaching, so a
	// loop can drive only the executor and let a fixed model condense and phrase.
	support ax.AIClient
}

func newStepMailbox(support ax.AIClient) *stepMailbox {
	return &stepMailbox{requests: make(chan *mailboxRequest), support: support}
}

// reset drops anything left parked from an episode that ended badly, so the
// next episode does not receive the last one's question.
func (m *stepMailbox) reset() {
	for {
		select {
		case parked := <-m.requests:
			close(parked.Reply)
		default:
			return
		}
	}
}

var errStepEpisodeGone = errors.New("the episode this call belonged to has ended")

func (m *stepMailbox) Chat(ctx context.Context, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	stage := gjagent.StageOfChatRequest(values)
	if m.support != nil && (stage == gjagent.StageDistiller || stage == gjagent.StageResponder) {
		return m.support.Chat(ctx, values, options)
	}
	request := &mailboxRequest{Stage: stage, Values: values, Options: options, Reply: make(chan mailboxReply, 1)}
	select {
	case m.requests <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case reply, ok := <-request.Reply:
		if !ok {
			return nil, errStepEpisodeGone
		}
		return completionValue(reply), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *stepMailbox) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, fmt.Errorf("embedding is not available while an episode is driven step by step")
}

func (m *stepMailbox) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, fmt.Errorf("streaming is not available while an episode is driven step by step")
}

// completionValue wraps a trainer's completion in the shape a provider would
// have returned, so nothing downstream can tell the difference.
func completionValue(reply mailboxReply) ax.Value {
	return map[string]ax.Value{
		"results": []ax.Value{map[string]ax.Value{"content": reply.Completion}},
		"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{
			"prompt": reply.PromptTokens, "completion": reply.CompletionTokens,
		}},
	}
}

// stepMessage is one rendered turn as the trainer sees it.
type stepMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// stepMessagesFromRequest renders the parked call as chat messages.
func stepMessagesFromRequest(values map[string]ax.Value) []stepMessage {
	prompt, ok := values["chat_prompt"].([]ax.Value)
	if !ok {
		return nil
	}
	messages := make([]stepMessage, 0, len(prompt))
	for _, entry := range prompt {
		message, ok := entry.(map[string]ax.Value)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		content, _ := message["content"].(string)
		messages = append(messages, stepMessage{Role: role, Content: content})
	}
	return messages
}
