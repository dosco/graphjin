package agent

import (
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestContextAnchorKeepsOneSystemMessage(t *testing.T) {
	client := &contextAnchorClient{request: []string{"org rules", "which block is full?"}}
	request := ax.Object("chat_prompt", []ax.Value{
		ax.Object("role", "system", "content", "ax prompt", "cache", true),
		ax.Object("role", "user", "content", "Instructions: org ru"),
	})
	messages := client.anchored(request)["chat_prompt"].([]ax.Value)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	system := messages[0].(map[string]ax.Value)
	content := system["content"].(string)
	if !strings.HasPrefix(content, "Accepted user request and constraints (authoritative): org rules\n\nwhich block is full?") ||
		!strings.HasSuffix(content, "\n\nax prompt") || system["cache"] != true {
		t.Fatalf("system message = %#v", system)
	}
	if original := request["chat_prompt"].([]ax.Value)[0].(map[string]ax.Value)["content"]; original != "ax prompt" {
		t.Fatalf("original request changed: %v", original)
	}
}

func TestContextAnchorRestoresOnlyTheCutParts(t *testing.T) {
	client := &contextAnchorClient{request: []string{"org rules", "which block is full?"}}
	messages := client.anchored(ax.Object("chat_prompt", []ax.Value{
		ax.Object("role", "system", "content", "ax prompt"),
		ax.Object("role", "user", "content", "Instructions: org ru\n\nQuestion: which block is full?"),
	}))["chat_prompt"].([]ax.Value)
	want := "Accepted user request and constraints (authoritative): org rules\n\nax prompt"
	if got := messages[0].(map[string]ax.Value)["content"]; got != want {
		t.Fatalf("system = %q, want %q", got, want)
	}
}

func TestContextAnchorAddsSystemMessageWhenNoneLeads(t *testing.T) {
	client := &contextAnchorClient{request: []string{"org rules"}}
	messages := client.anchored(ax.Object("chat_prompt", []ax.Value{
		ax.Object("role", "user", "content", "hi"),
	}))["chat_prompt"].([]ax.Value)
	if len(messages) != 2 || messages[0].(map[string]ax.Value)["role"] != "system" {
		t.Fatalf("messages = %#v", messages)
	}
}
