// Command embedding-fixture serves deterministic OpenAI-compatible embeddings
// for the coffee-roastery semantic discovery smoke test. It also exposes one
// scripted chat-completions response so the optional agent smoke can exercise
// the real Ax/Goja runtime and GraphJin protocol without a provider API key. It
// is intentionally domain-specific and does not claim to benchmark a production
// embedding or chat model.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultDimensions = 128

const (
	conceptCustomers = iota
	conceptOrders
	conceptInventory
	conceptQuality
	conceptRoasting
	conceptTime
	conceptSensors
	conceptEquipment
	conceptPermissions
	conceptPlanning
)

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions"`
}

type fixtureStats struct {
	Requests     int   `json:"requests"`
	Inputs       int   `json:"inputs"`
	MaxBatchSize int   `json:"max_batch_size"`
	BatchSizes   []int `json:"batch_sizes"`
	ChatRequests int   `json:"chat_requests"`
}

type fixtureServer struct {
	mu    sync.Mutex
	stats fixtureStats
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18081", "HTTP listen address")
	flag.Parse()

	fixture := &fixtureServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", fixture.health)
	mux.HandleFunc("/stats", fixture.statistics)
	mux.HandleFunc("/v1/embeddings", fixture.embeddings)
	mux.HandleFunc("/v1/chat/completions", fixture.chatCompletions)
	server := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("coffee semantic embedding fixture listening on %s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (s *fixtureServer) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

func (s *fixtureServer) statistics(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	stats := s.stats
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, stats)
}

func (s *fixtureServer) embeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close() //nolint:errcheck
	var request embeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("invalid embedding request: %v", err), http.StatusBadRequest)
		return
	}
	if request.Dimensions == 0 {
		request.Dimensions = defaultDimensions
	}
	if request.Dimensions < 10 || request.Dimensions > 4096 {
		http.Error(w, "dimensions must be between 10 and 4096", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.stats.Requests++
	s.stats.Inputs += len(request.Input)
	s.stats.BatchSizes = append(s.stats.BatchSizes, len(request.Input))
	if len(request.Input) > s.stats.MaxBatchSize {
		s.stats.MaxBatchSize = len(request.Input)
	}
	s.mu.Unlock()

	data := make([]map[string]any, len(request.Input))
	for index, text := range request.Input {
		data[index] = map[string]any{
			"object":    "embedding",
			"index":     index,
			"embedding": semanticFixtureEmbedding(text, request.Dimensions),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"model":  request.Model,
		"data":   data,
		"usage": map[string]any{
			"prompt_tokens": len(request.Input),
			"total_tokens":  len(request.Input),
		},
	})
}

func (s *fixtureServer) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close() //nolint:errcheck
	var request map[string]any
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("invalid chat request: %v", err), http.StatusBadRequest)
		return
	}

	// The code deliberately uses business terminology, performs one diversified
	// coverage batch, derives detail ids from returned cards, and inspects those
	// ids before answering. GraphJin still owns validation, visibility, path
	// expansion, action logging, and the one-batch protocol guard.
	code := `
const coverage = query_catalog({
  searches: [
    "clients and purchases",
    "clients buying roasted coffee",
    "purchases awaiting production"
  ],
  where: {kind: {in: ["table", "relationship"]}},
  limit: 20
});
const inspectIds = coverage.next && coverage.next.args
  ? (coverage.next.args.ids || [])
  : [];
const inspected = inspectIds.length > 0
  ? query_catalog({ids: inspectIds})
  : {cards: [], details: []};
final({
  status: "answered",
  answer: "The adaptive semantic coverage search found the customer and production-order endpoints and used only catalog relationship evidence.",
  data: {coverage, inspected},
  evidence: {catalog_ids: inspectIds},
  actions: ["semantic coverage batch", "catalog detail inspection"]
});`
	content, err := json.Marshal(map[string]any{"javascriptCode": code})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.stats.ChatRequests++
	chatIndex := s.stats.ChatRequests
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-coffee-fixture-%d", chatIndex),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   request["model"],
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": string(content),
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     1,
			"completion_tokens": 1,
			"total_tokens":      2,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func semanticFixtureEmbedding(text string, dimensions int) []float64 {
	vector := make([]float64, dimensions)
	normalized := normalizeFixtureText(text)
	switch {
	case strings.HasPrefix(normalized, "table identity"):
		fixtureTableConcepts(vector, normalized)
	case strings.HasPrefix(normalized, "relationship neighborhood"):
		fixtureTableConcepts(vector, normalized)
	case strings.HasPrefix(normalized, "table column facet"):
		fixtureColumnConcepts(vector, normalized)
	case strings.HasPrefix(normalized, "catalog concept"):
		fixtureConceptCardConcepts(vector, normalized)
	default:
		fixtureQueryConcepts(vector, normalized)
	}
	normalizeFixtureVector(vector)
	return vector
}

func normalizeFixtureText(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func fixtureTableConcepts(vector []float64, text string) {
	setFixtureConcept(vector, conceptCustomers, containsFixturePhrase(text, "customers"))
	setFixtureConcept(vector, conceptOrders, containsFixturePhrase(text, "production orders"))
	setFixtureConcept(vector, conceptInventory, containsFixturePhrase(text, "green lots"))
	setFixtureConcept(vector, conceptQuality, containsFixturePhrase(text, "qc cupping scores"))
	setFixtureConcept(vector, conceptRoasting, containsFixturePhrase(text, "roast batches", "roast profiles", "roast schedule"))
	setFixtureConcept(vector, conceptSensors, containsFixturePhrase(text, "roast sensor samples"))
	setFixtureConcept(vector, conceptEquipment, containsFixturePhrase(text, "equipment telemetry"))
}

func fixtureColumnConcepts(vector []float64, text string) {
	setFixtureConcept(vector, conceptCustomers, containsFixturePhrase(text, "account tier", "customer id"))
	setFixtureConcept(vector, conceptOrders, containsFixturePhrase(text, "requested ship date", "quantity bags", "production order id"))
	setFixtureConcept(vector, conceptInventory, containsFixturePhrase(text, "remaining kg", "target cost per kg", "green lot id"))
	setFixtureConcept(vector, conceptQuality, containsFixturePhrase(text, "defects", "cupping score", "total score", "fragrance", "flavor", "acidity"))
	setFixtureConcept(vector, conceptTime, containsFixturePhrase(text, "scheduled for", "recorded at", "scored at", "started at", "ended at"))
	setFixtureConcept(vector, conceptSensors, containsFixturePhrase(text, "bean temp", "env temp", "ror c per min", "airflow percent", "drum rpm"))
	setFixtureConcept(vector, conceptEquipment, containsFixturePhrase(text, "vibration mm", "exhaust pa", "burner duty"))
}

func fixtureConceptCardConcepts(vector []float64, text string) {
	setFixtureConcept(vector, conceptPermissions, containsFixturePhrase(text, "permission", "access policy", "read only", "prevent users", "changing records"))
	setFixtureConcept(vector, conceptPlanning, containsFixturePhrase(text, "daily roast", "roast plan", "production priority"))
}

func fixtureQueryConcepts(vector []float64, text string) {
	setFixtureConcept(vector, conceptCustomers, containsFixturePhrase(text, "client", "clients", "customer", "customers"))
	setFixtureConcept(vector, conceptOrders, containsFixturePhrase(text, "purchase", "purchases", "order", "orders", "shipment", "shipments"))
	setFixtureConcept(vector, conceptInventory, containsFixturePhrase(text, "raw coffee", "green coffee", "inventory", "stock"))
	setFixtureConcept(vector, conceptQuality, containsFixturePhrase(text, "quality", "failure", "failures", "defect", "defects", "cupping"))
	setFixtureConcept(vector, conceptRoasting, containsFixturePhrase(text, "roast", "roasting", "batch", "batches"))
	setFixtureConcept(vector, conceptTime, containsFixturePhrase(text, "recent", "today", "month", "monthly", "history"))
	setFixtureConcept(vector, conceptSensors, containsFixturePhrase(text, "bean temperature", "sensor", "sensors", "temperature history"))
	setFixtureConcept(vector, conceptEquipment, containsFixturePhrase(text, "machine health", "equipment", "telemetry", "maintenance"))
	setFixtureConcept(vector, conceptPermissions, containsFixturePhrase(text, "prevent users", "changing records", "permission", "permissions", "read only"))
	setFixtureConcept(vector, conceptPlanning, containsFixturePhrase(text, "what should we roast", "roast today", "prioritize", "plan"))
}

func containsFixturePhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func setFixtureConcept(vector []float64, concept int, enabled bool) {
	if enabled && concept >= 0 && concept < len(vector) {
		vector[concept] = 1
	}
}

func normalizeFixtureVector(vector []float64) {
	var sum float64
	for _, value := range vector {
		sum += value * value
	}
	if sum == 0 {
		return
	}
	scale := 1 / math.Sqrt(sum)
	for index := range vector {
		vector[index] *= scale
	}
}
