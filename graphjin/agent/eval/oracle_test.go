package eval

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (fn doerFunc) Do(request *http.Request) (*http.Response, error) { return fn(request) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestVerifierResolvesAnchorAndExtraction(t *testing.T) {
	calls := 0
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(200, `{"data":{"events":[{"max_at":"2026-08-01"}]}}`), nil
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `2026-07-25`) {
			t.Fatalf("resolved variables missing shifted anchor: %s", body)
		}
		return jsonResponse(200, `{"data":{"events":[{"count_id":42}]}}`), nil
	})
	result, err := (Verifier{Client: doer, BaseURL: "http://graphjin.test", Now: func() time.Time { return time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC) }}).Resolve(context.Background(), OracleSpec{
		AnchorQuery: "query { events { max_at } }", AnchorExtract: "events.0.max_at",
		Query:     "query Q($from: String!) { events(where: {at: {gte: $from}}) { count_id } }",
		Variables: map[string]any{"from": "{{anchor-7d}}"}, Extract: "events.0.count_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != "42" || calls != 2 {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
}

func TestVerifierInlinesAnchoredVariableWithoutNamingQuery(t *testing.T) {
	calls := 0
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(200, `{"data":{"events":[{"max_at":"2026-08-01"}]}}`), nil
		}
		body, _ := io.ReadAll(request.Body)
		payload := string(body)
		if !strings.Contains(payload, `"query":"{ events(where: {at: {gte: \"2026-07-25\"}}) { count_id } }"`) {
			t.Fatalf("anchor was not inlined into anonymous query: %s", payload)
		}
		if strings.Contains(payload, `"variables"`) || strings.Contains(payload, oracleVariableMarker("from")) {
			t.Fatalf("inlined oracle leaked a marker or unused variables: %s", payload)
		}
		return jsonResponse(200, `{"data":{"events":[{"count_id":42}]}}`), nil
	})
	result, err := (Verifier{Client: doer, BaseURL: "http://graphjin.test"}).Resolve(context.Background(), OracleSpec{
		AnchorQuery: "{ events { max_at } }", AnchorExtract: "events.0.max_at",
		Query:     `{ events(where: {at: {gte: "` + oracleVariableMarker("from") + `"}}) { count_id } }`,
		Variables: map[string]any{"from": "{{anchor-7d}}"}, Extract: "events.0.count_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != "42" || calls != 2 {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
}

func TestGroundTruthToleranceAndScales(t *testing.T) {
	task := Task{ExpectedStatus: "answered", Answer: AnswerRule{Kind: "number", TolerancePct: 0.01, AcceptScales: []float64{1, 0.01}}}
	pass, detail := evaluateGroundTruth(task, OracleResult{Value: "1980000"}, responseWithAnswer("answered", "The total is $19,800."))
	if !pass {
		t.Fatalf("scaled answer failed: %s", detail)
	}
	pass, _ = evaluateGroundTruth(task, OracleResult{Value: "1980000"}, responseWithAnswer("answered", "The total is $12."))
	if pass {
		t.Fatal("incorrect numeric answer passed")
	}
}

func TestVerifierPickMax(t *testing.T) {
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"data":{"accounts":[{"name":"A","total":4},{"name":"B","total":9}]}}`), nil
	})
	result, err := (Verifier{Client: doer, BaseURL: "http://graphjin.test"}).Resolve(context.Background(), OracleSpec{
		Query:   "query { accounts { name total } }",
		PickMax: &PickMaxRule{List: "accounts", Value: "total", Dimension: "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != "9" || result.Dimension != "B" {
		t.Fatalf("unexpected pick_max result: %+v", result)
	}
}

// An anchor that pins a row rather than a window is an identifier, not a date.
// Shifting one by days still requires a timestamp, because there is nothing a
// day before an account id.
func TestUnshiftedAnchorNeedNotBeADate(t *testing.T) {
	out, err := substituteOracleTokens(`where: {id: {eq: {{anchor}}}}`, "42", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if out != `where: {id: {eq: 42}}` {
		t.Fatalf("unexpected substitution %q", out)
	}
	if _, err := substituteOracleTokens(`{{anchor-7d}}`, "42", time.Unix(0, 0).UTC()); err == nil {
		t.Fatal("shifting a non-date anchor must fail rather than invent a value")
	}
	// Dates keep shifting as before.
	shifted, err := substituteOracleTokens(`{{anchor-7d}}`, "2026-08-08", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if shifted != "2026-08-01" {
		t.Fatalf("unexpected shifted anchor %q", shifted)
	}
}
