package agent

import (
	"context"

	ax "github.com/ax-llm/ax/packages/go"
)

// forwardStreamingAnswer runs the lead with Ax's StreamingForward and hands
// the reply's answer text to onAnswer as it arrives. The earlier stages run
// as usual; only the reply stage streams. It returns the same output a
// Forward returns: string fields are the concatenated deltas, other fields
// the last value. A new delta version means Ax restarted the reply (a retry
// after a validation failure), so the output so far is dropped and the
// caller is told to reset what it showed.
func forwardStreamingAnswer(ctx context.Context, program *ax.AxAgent, client ax.AIClient, input, options map[string]ax.Value, onAnswer func(string, bool)) (ax.Value, error) {
	values := map[string]ax.Value{}
	version := -1
	for delta, err := range program.StreamingForward(ctx, client, input, options) {
		if err != nil {
			return nil, err
		}
		if delta.Version != version {
			if version != -1 {
				values = map[string]ax.Value{}
				onAnswer("", true)
			}
			version = delta.Version
		}
		for field, value := range delta.Delta {
			text, isText := value.(string)
			if !isText {
				values[field] = value
				continue
			}
			prior, _ := values[field].(string)
			values[field] = prior + text
			if field == "answer" && text != "" {
				onAnswer(text, false)
			}
		}
	}
	return values, nil
}
