package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSemanticFixtureSynonymsAndRelationshipIntent(t *testing.T) {
	clients := semanticFixtureEmbedding("clients", defaultDimensions)
	customers := semanticFixtureEmbedding("table identity\nname: ops.public.customers\ntype: table", defaultDimensions)
	if similarity := fixtureDot(clients, customers); similarity < 0.99 {
		t.Fatalf("clients/customers similarity = %f", similarity)
	}

	purchases := semanticFixtureEmbedding("purchases", defaultDimensions)
	orders := semanticFixtureEmbedding("table identity\nname: ops.public.production_orders\ntype: table", defaultDimensions)
	if similarity := fixtureDot(purchases, orders); similarity < 0.99 {
		t.Fatalf("purchases/orders similarity = %f", similarity)
	}

	intent := semanticFixtureEmbedding("clients and purchases", defaultDimensions)
	neighborhood := semanticFixtureEmbedding("relationship neighborhood\ntable: ops.public.production_orders\nforeign keys:\n- ops.public.production_orders.customer_id -> ops.public.customers.id type=many_to_one", defaultDimensions)
	if similarity := fixtureDot(intent, neighborhood); similarity < 0.99 {
		t.Fatalf("relationship intent similarity = %f", similarity)
	}

	unrelated := semanticFixtureEmbedding("employee payroll tax", defaultDimensions)
	if norm := math.Sqrt(fixtureDot(unrelated, unrelated)); norm != 0 {
		t.Fatalf("unrelated fixture query should stay at background score, norm = %f", norm)
	}
}

// Capability: ANNOTATION-SEMANTIC
// The private vocabulary must match its annotation document without matching
// the underlying production_orders catalog document directly.
func TestAnnotationSemanticFixtureVocabularyIsAnnotationOnly(t *testing.T) {
	query := semanticFixtureEmbedding("chargeback escrow lineage", defaultDimensions)
	annotation := semanticFixtureEmbedding("approved organizational annotation\ntarget: table:ops.public.production_orders\nnote: chargeback escrow lineage", defaultDimensions)
	orders := semanticFixtureEmbedding("table identity\nname: ops.public.production_orders\ntype: table", defaultDimensions)
	if similarity := fixtureDot(query, annotation); similarity < 0.70 {
		t.Fatalf("annotation vocabulary similarity = %f", similarity)
	}
	if similarity := fixtureDot(query, orders); similarity != 0 {
		t.Fatalf("annotation vocabulary leaked into base table vector: %f", similarity)
	}
}

// Capability: ANNOTATION-GUARD
// The fixture must emit both mutations so the live agent protocol—not fixture
// behavior—owns enforcement of the separate-run approval boundary.
func TestAnnotationGuardFixtureAttemptsSameRunApproval(t *testing.T) {
	fixture := &fixtureServer{}
	body := bytes.NewBufferString(`{"model":"fixture","messages":[{"role":"user","content":"ANNOTATION_GUARD_FIXTURE"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	response := httptest.NewRecorder()
	fixture.chatCompletions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || len(envelope.Choices) != 1 {
		t.Fatalf("decode guard fixture response: choices=%d err=%v body=%s", len(envelope.Choices), err, response.Body.String())
	}
	var content struct {
		JavaScriptCode string `json:"javascriptCode"`
	}
	if err := json.Unmarshal([]byte(envelope.Choices[0].Message.Content), &content); err != nil {
		t.Fatalf("decode guard fixture program: %v content=%s", err, envelope.Choices[0].Message.Content)
	}
	for _, want := range []string{`query_catalog({id: "help:security"})`, "ANNOTATION-GUARD-fixture-draft", "insert", `tier: "approved"`} {
		if !strings.Contains(content.JavaScriptCode, want) {
			t.Fatalf("guard fixture program missing %q: %s", want, content.JavaScriptCode)
		}
	}
}

// Capability: ANNOTATION-GUARD
// Once the protocol has refused the tier flip, the fixture must terminate the
// actor loop so the response exposes that refusal instead of a max-step error.
func TestAnnotationGuardFixtureStopsAfterProtocolRefusal(t *testing.T) {
	fixture := &fixtureServer{}
	requestBody := `{"model":"fixture","messages":[{"role":"user","content":"ANNOTATION_GUARD_FIXTURE"}]}`
	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(requestBody))
	firstResponse := httptest.NewRecorder()
	fixture.chatCompletions(firstResponse, firstRequest)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("initial status = %d, body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(requestBody))
	response := httptest.NewRecorder()
	fixture.chatCompletions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || len(envelope.Choices) != 1 {
		t.Fatalf("decode guard refusal response: choices=%d err=%v body=%s", len(envelope.Choices), err, response.Body.String())
	}
	var content struct {
		JavaScriptCode string `json:"javascriptCode"`
	}
	if err := json.Unmarshal([]byte(envelope.Choices[0].Message.Content), &content); err != nil {
		t.Fatalf("decode guard refusal program: %v content=%s", err, envelope.Choices[0].Message.Content)
	}
	if !strings.Contains(content.JavaScriptCode, `status: "blocked"`) || strings.Contains(content.JavaScriptCode, "execute_graphql") {
		t.Fatalf("guard refusal fixture must terminate without retrying writes: %s", content.JavaScriptCode)
	}
}

func TestEmbeddingFixtureOpenAIShapeAndStats(t *testing.T) {
	fixture := &fixtureServer{}
	body, err := json.Marshal(embeddingRequest{
		Model:      "coffee-semantic-smoke",
		Input:      []string{"clients", "purchases"},
		Dimensions: defaultDimensions,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	response := httptest.NewRecorder()
	fixture.embeddings(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 2 || len(result.Data[0].Embedding) != defaultDimensions || len(result.Data[1].Embedding) != defaultDimensions {
		t.Fatalf("unexpected response shape: %+v", result)
	}
	fixture.mu.Lock()
	stats := fixture.stats
	fixture.mu.Unlock()
	if stats.Requests != 1 || stats.Inputs != 2 || stats.MaxBatchSize != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func fixtureDot(left, right []float64) float64 {
	var total float64
	for index := range left {
		total += left[index] * right[index]
	}
	return total
}
