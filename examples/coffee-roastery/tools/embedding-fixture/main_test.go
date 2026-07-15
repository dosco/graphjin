package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
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
