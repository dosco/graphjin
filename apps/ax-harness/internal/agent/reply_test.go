package agent

import (
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestWrittenReplyKeepsTextAndAddsCardFences(t *testing.T) {
	reply, err := writtenReply(ax.Object("text", "**3 of 5 available.**", "cards", []ax.Value{ax.Object("title", "Materials", "table", "A | B\n1 | 2")}))
	if err != nil {
		t.Fatal(err)
	}
	want := "**3 of 5 available.**\n\n```neko_cards\n{\"table\":\"A | B\\n1 | 2\",\"title\":\"Materials\"}\n```"
	if got := reply.answer(""); got != want {
		t.Fatalf("reply = %q", got)
	}
	if plain, _ := writtenReply("Done."); plain.answer("") != "Done." {
		t.Fatalf("plain reply = %q", plain.answer(""))
	}
	if _, err := writtenReply(ax.Object("cards", ax.Object("title", "x"))); err == nil || !strings.Contains(err.Error(), "needs {text}") {
		t.Fatalf("missing text error = %v", err)
	}
}

func TestReplyInResultReadsAScriptsPrintedReply(t *testing.T) {
	raw := []byte(`{"output":"procim.py\n{\n  \"reply_as_written\": {\"text\": \"**3 of 6 available.**\", \"cards\": [{\"title\": \"Q materials\"}]},\n  \"headline\": \"x\"\n}\n","exit_code":0}`)
	reply, ok := replyInResult(raw)
	if !ok || !strings.HasPrefix(reply.answer(""), "**3 of 6 available.**\n\n```neko_cards\n{\"title\":\"Q materials\"}") {
		t.Fatalf("reply = %q, ok = %v", reply.answer(""), ok)
	}
	if _, ok := replyInResult([]byte(`{"output":"{\"answer\": \"x\"}","exit_code":0}`)); ok {
		t.Fatal("a result without reply_as_written is not a reply")
	}
}

func TestPrintedReplyTakesTheModelTextAndKeepsTheCards(t *testing.T) {
	reply, _ := replyInResult([]byte(`{"reply_as_written": {"text": "**0 of 1 available.**", "cards": [{"title": "Q materials"}]}}`))
	cards := "\n\n```neko_cards\n{\"title\":\"Q materials\"}\n```"
	own := "**0 of 1 available.** EM-882K is not in ProCIM. I used 20 from your 10-panel request."
	if got := reply.answer(own); got != own+cards {
		t.Fatalf("answer = %q", got)
	}
	retyped := own + "\n\n```neko_cards\n{\"title\":\"retyped\"}\n```"
	if got := reply.answer(retyped); got != own+cards {
		t.Fatalf("answer with retyped cards = %q", got)
	}
	if got := reply.answer("  "); got != "**0 of 1 available.**"+cards {
		t.Fatalf("answer without model text = %q", got)
	}
}

func TestExplicitReplyIgnoresTheModelText(t *testing.T) {
	reply, _ := writtenReply(ax.Object("text", "**2 of 2 available.**"))
	reply.explicit = true
	if got := reply.answer("Something else."); got != "**2 of 2 available.**" {
		t.Fatalf("answer = %q", got)
	}
}
