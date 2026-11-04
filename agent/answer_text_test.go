package agent

import "testing"

func TestUnescapeAnswerNewlines(t *testing.T) {
	for in, want := range map[string]string{
		`Tied.\n\n| Plan | Accounts |\n| --- | --- |`: "Tied.\n\n| Plan | Accounts |\n| --- | --- |",
		"Line one\nUse `\\n` for a newline.":          "Line one\nUse `\\n` for a newline.",
		"Plain answer.":                               "Plain answer.",
	} {
		if got := unescapeAnswerNewlines(in); got != want {
			t.Errorf("unescapeAnswerNewlines(%q) = %q, want %q", in, got, want)
		}
	}
}
