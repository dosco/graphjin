package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type pr633Transport struct{}

func (pr633Transport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"amount":9007199254740993}}`))}, nil
}
func TestNumberFromStringNegativeCurrency(t *testing.T) {
	n, ok := numberFromString("-$100.50")
	if !ok || n != -100.50 {
		t.Fatalf("got %v, %v", n, ok)
	}
}
func TestPostGraphQLPreservesLargeIntegers(t *testing.T) {
	data, err := postGraphQL(&http.Client{Transport: pr633Transport{}}, endpointProfile{URL: "http://example.test/api/v1/agent"}, "query { amount }", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := valueString(data.(map[string]any)["amount"]); got != "9007199254740993" {
		t.Fatalf("got %s", got)
	}
}
