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
	if reply != want {
		t.Fatalf("reply = %q", reply)
	}
	if plain, _ := writtenReply("Done."); plain != "Done." {
		t.Fatalf("plain reply = %q", plain)
	}
	if _, err := writtenReply(ax.Object("cards", ax.Object("title", "x"))); err == nil || !strings.Contains(err.Error(), "needs {text}") {
		t.Fatalf("missing text error = %v", err)
	}
}

func TestReplyInResultReadsAScriptsPrintedReply(t *testing.T) {
	raw := []byte(`{"output":"procim.py\n{\n  \"reply_as_written\": {\"text\": \"**3 of 6 available.**\", \"cards\": [{\"title\": \"Q materials\"}]},\n  \"headline\": \"x\"\n}\n","exit_code":0}`)
	reply, ok := replyInResult(raw)
	if !ok || !strings.HasPrefix(reply, "**3 of 6 available.**\n\n```neko_cards\n{\"title\":\"Q materials\"}") {
		t.Fatalf("reply = %q, ok = %v", reply, ok)
	}
	if _, ok := replyInResult([]byte(`{"output":"{\"answer\": \"x\"}","exit_code":0}`)); ok {
		t.Fatal("a result without reply_as_written is not a reply")
	}
}
