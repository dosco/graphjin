package eval

import (
	"context"
	"fmt"
	"strings"
)

// A task with earlier turns is a conversation. The data agent answers one
// self-contained question and holds none, so an endpoint that exposes it alone
// rejects such a request before any agent runs. Scored, that rejection read as
// the model refusing: one whole family banked zeros on every run and nothing
// stopped. The checks here turn it into a refusal to start.

// ConversationTasks returns the tasks that send earlier turns.
func ConversationTasks(tasks []Task) []Task {
	var out []Task
	for _, task := range tasks {
		if len(task.Turns) != 0 {
			out = append(out, task)
		}
	}
	return out
}

// AssertConversationsRunnable refuses a set of tasks the instance would reject
// unanswered. It asks the instance only when a task sends earlier turns, so a
// suite of single questions costs nothing.
//
// A server that does not say whether it holds a conversation is let through:
// it predates the question, and it may well answer.
func AssertConversationsRunnable(ctx context.Context, client HTTPDoer, instance Instance, tasks []Task) error {
	conversations := ConversationTasks(tasks)
	if len(conversations) == 0 {
		return nil
	}
	status, err := fetchAgentStatus(ctx, client, instance.BaseURL(), instance.Headers())
	if err != nil {
		return fmt.Errorf("%d task(s) send earlier turns, and the instance could not say whether it holds a conversation: %w",
			len(conversations), err)
	}
	if !status.RefusesConversations() {
		return nil
	}
	return conversationRefusal(conversations)
}

func conversationRefusal(conversations []Task) error {
	const shown = 5
	names := make([]string, 0, shown)
	for _, task := range conversations {
		if len(names) == shown {
			break
		}
		names = append(names, firstNonEmpty(task.Slug, task.ID))
	}
	listed := strings.Join(names, ", ")
	if more := len(conversations) - len(names); more > 0 {
		listed += fmt.Sprintf(" and %d more", more)
	}
	return fmt.Errorf(
		"%d task(s) send earlier turns (%s), but this instance answers with the data agent alone, which holds no "+
			"conversation: every one would be refused before the agent ran and scored as a failure. Remove them with "+
			"`deeporg eval rm <task-id>`, or run against a server with agent.lead.enabled",
		len(conversations), listed)
}
