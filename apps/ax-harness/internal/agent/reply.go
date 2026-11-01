package agent

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

const maxWrittenReply = 65536

// writtenAnswer is a finished reply from a tool: its text and the neko_cards
// fences for its cards. A reply passed to replyAsWritten is explicit and goes
// to the reader unchanged; a reply printed in a tool result keeps the cards
// and lets the model write the text.
type writtenAnswer struct {
	text, cards string
	explicit    bool
}

// answer is the reply the reader gets. A printed reply takes the model's own
// text when it has one, so the reply can answer the question that was asked.
func (w writtenAnswer) answer(modelText string) string {
	text := w.text
	if own := strings.TrimSpace(cardFence.ReplaceAllString(modelText, "")); !w.explicit && own != "" {
		text = own
	}
	return text + w.cards
}

var cardFence = regexp.MustCompile("(?s)```neko_cards\\s.*?```")

// writtenReply turns replyAsWritten's argument into the final answer: the
// reply text, then one neko_cards fence for each card the host renders.
func writtenReply(value ax.Value) (writtenAnswer, error) {
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
		return writtenAnswer{}, errors.New("replyAsWritten needs {text} with the reply")
	}
	var fences strings.Builder
	for _, card := range cards {
		if _, ok := card.(map[string]ax.Value); !ok {
			return writtenAnswer{}, errors.New("each card must be an object of render_cards arguments")
		}
		data, err := json.Marshal(plainValue(card))
		if err != nil {
			return writtenAnswer{}, errors.New("a card is not valid JSON")
		}
		fences.WriteString("\n\n```neko_cards\n")
		fences.Write(data)
		fences.WriteString("\n```")
	}
	reply := writtenAnswer{text: strings.TrimSpace(text), cards: fences.String()}
	if len(reply.text)+len(reply.cards) > maxWrittenReply {
		return writtenAnswer{}, errors.New("the reply is too long; shorten it")
	}
	return reply, nil
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

// replyInResult finds a finished reply that a tool printed as a JSON object
// with a reply_as_written field, such as a skill script's output.
func replyInResult(raw json.RawMessage) (writtenAnswer, bool) {
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return writtenAnswer{}, false
	}
	if reply, ok := result["reply_as_written"]; ok {
		written, err := writtenReply(reply)
		return written, err == nil
	}
	output, _ := result["output"].(string)
	for i := 0; i < len(output); i++ {
		if output[i] != '{' || (i > 0 && output[i-1] != '\n') {
			continue
		}
		var object map[string]any
		if json.NewDecoder(strings.NewReader(output[i:])).Decode(&object) != nil {
			continue
		}
		if reply, ok := object["reply_as_written"]; ok {
			written, err := writtenReply(reply)
			return written, err == nil
		}
	}
	return writtenAnswer{}, false
}
