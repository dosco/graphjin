package agent

import (
	"encoding/json"
	"errors"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

const maxWrittenReply = 65536

// writtenReply turns replyAsWritten's argument into the final answer: the
// reply text, then one neko_cards fence for each card the host renders.
func writtenReply(value ax.Value) (string, error) {
	var text string
	var cards []ax.Value
	switch v := value.(type) {
	case string:
		text = v
	case map[string]ax.Value:
		for _, key := range []string{"text", "answer", "reply"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				text = s
				break
			}
		}
		switch c := v["cards"].(type) {
		case map[string]ax.Value:
			cards = []ax.Value{c}
		case []ax.Value:
			cards = c
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("replyAsWritten needs {text} with the reply")
	}
	var reply strings.Builder
	reply.WriteString(strings.TrimSpace(text))
	for _, card := range cards {
		if _, ok := card.(map[string]ax.Value); !ok {
			return "", errors.New("each card must be an object of render_cards arguments")
		}
		data, err := json.Marshal(plainValue(card))
		if err != nil {
			return "", errors.New("a card is not valid JSON")
		}
		reply.WriteString("\n\n```neko_cards\n")
		reply.Write(data)
		reply.WriteString("\n```")
	}
	if reply.Len() > maxWrittenReply {
		return "", errors.New("the reply is too long; shorten it")
	}
	return reply.String(), nil
}

// plainValue drops the key-order entry that ax objects carry.
func plainValue(value ax.Value) ax.Value {
	switch v := value.(type) {
	case map[string]ax.Value:
		out := make(map[string]ax.Value, len(v))
		for key, item := range v {
			if key != "__order" {
				out[key] = plainValue(item)
			}
		}
		return out
	case []ax.Value:
		out := make([]ax.Value, len(v))
		for i, item := range v {
			out[i] = plainValue(item)
		}
		return out
	}
	return value
}
