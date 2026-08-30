package eval

import "testing"

// Models wrap JSON in fences and talk either side of it. The decoder has to
// survive that without being so loose it accepts something that is not the
// reply — a wrong parse here silently produces authored tasks nobody asked for.
func TestDecodeFencedJSONAcceptsRealModelShapes(t *testing.T) {
	type reply struct {
		Status string `json:"status"`
	}
	cases := map[string]string{
		"bare object":        `{"status":"ready"}`,
		"json fence":         "```json\n{\"status\":\"ready\"}\n```",
		"plain fence":        "```\n{\"status\":\"ready\"}\n```",
		"prose either side":  "Here is the task:\n```json\n{\"status\":\"ready\"}\n```\nLet me know if you want changes.",
		"leading whitespace": "   \n{\"status\":\"ready\"}",
	}
	for name, text := range cases {
		var out reply
		if err := decodeFencedJSON(text, &out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.Status != "ready" {
			t.Fatalf("%s: decoded %+v", name, out)
		}
	}
}

// Authoring asks for several picks at once, so a top-level array has to decode
// as readily as an object.
func TestDecodeFencedJSONAcceptsArrays(t *testing.T) {
	var picks []struct {
		Table string `json:"table"`
	}
	if err := decodeFencedJSON("```json\n[{\"table\":\"invoices\"},{\"table\":\"accounts\"}]\n```", &picks); err != nil {
		t.Fatal(err)
	}
	if len(picks) != 2 || picks[0].Table != "invoices" {
		t.Fatalf("decoded %+v", picks)
	}
}

// An object whose fields contain an array must decode as the object. Picking
// the innermost array instead would quietly drop everything around it.
func TestDecodeFencedJSONPrefersWhicheverValueStartsFirst(t *testing.T) {
	var out struct {
		Kind  string   `json:"kind"`
		Names []string `json:"names"`
	}
	if err := decodeFencedJSON(`{"kind":"watch","names":["a","b"]}`, &out); err != nil {
		t.Fatal(err)
	}
	if out.Kind != "watch" || len(out.Names) != 2 {
		t.Fatalf("decoded %+v", out)
	}

	var list []map[string]any
	if err := decodeFencedJSON(`[{"kind":"watch"},{"kind":"history"}]`, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("decoded %+v", list)
	}
}

func TestDecodeFencedJSONRefusesWhatIsNotJSON(t *testing.T) {
	var out map[string]any
	for name, text := range map[string]string{
		"prose only": "I could not find a suitable table for this.",
		"empty":      "",
		"truncated":  `{"status":"rea`,
	} {
		if err := decodeFencedJSON(text, &out); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
}
