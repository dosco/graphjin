package catalog

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func mustLookup(t *testing.T, provider, model string) Rates {
	t.Helper()
	rates, ok := Lookup(provider, model)
	if !ok || !rates.Valid() {
		t.Fatalf("%s/%s has no catalog price", provider, model)
	}
	return rates
}

func TestLookupMapsRouteProviders(t *testing.T) {
	lite := mustLookup(t, "google-gemini", "gemini-3.5-flash-lite")
	if lite.Prompt != 300_000 || lite.Completion != 2_500_000 || lite.CacheRead != 30_000 || lite.CacheWrite != 300_000 {
		t.Fatalf("gemini-3.5-flash-lite rates=%+v", lite)
	}
	if mustLookup(t, "GoogleGeminiAI", "gemini-3.8-flash") != mustLookup(t, "google-gemini", "gemini-3.8-flash") {
		t.Fatal("the Ax TypeScript Gemini name does not resolve")
	}
	mustLookup(t, "anthropic", "claude-sonnet-5-5")
	if mustLookup(t, "openai-compatible", "gpt-6-luna") != mustLookup(t, "openai", "gpt-6-luna") {
		t.Fatal("openai-compatible routes do not use OpenAI prices")
	}
	if mustLookup(t, "openai", "gpt-5.6") != mustLookup(t, "openai", "gpt-5.6-sol") {
		t.Fatal("an alias does not resolve to its model")
	}
	for _, unknown := range [][2]string{{"google-gemini", "no-such-model"}, {"groq", "llama-3.3-70b"}, {"", "gpt-6-luna"}} {
		if _, ok := Lookup(unknown[0], unknown[1]); ok {
			t.Fatalf("%v priced", unknown)
		}
	}
}

func TestCostUsesCacheAndReasoningRates(t *testing.T) {
	sonnet := mustLookup(t, "anthropic", "claude-sonnet-5-5")
	// $2 input, $0.20 cache read, $2.50 cache write, $10 output per million.
	got := sonnet.Cost(Tokens{Input: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000, Output: 1_000_000, Total: 4_000_000})
	if got != 14_700_000 {
		t.Fatalf("cache-rated cost=%d", got)
	}
	// Tokens beyond the split are reasoning and bill as output.
	if got := sonnet.Cost(Tokens{Output: 1_000, Total: 2_000}); got != 20_000 {
		t.Fatalf("reasoning cost=%d", got)
	}
	if got := sonnet.Cost(Tokens{Input: 1, Output: 1, Total: 2}); got != 12 {
		t.Fatalf("each component rounds up: %d", got)
	}
}

func TestCostUsesLongContextTier(t *testing.T) {
	haiku := mustLookup(t, "anthropic", "claude-haiku-5-5")
	if haiku.LongContextThreshold != 100_000 {
		t.Fatalf("threshold=%d", haiku.LongContextThreshold)
	}
	if got := haiku.Cost(Tokens{Input: 60_000, CacheRead: 40_000, Total: 100_000}); got != 6_400 {
		t.Fatalf("standard tier cost=%d", got)
	}
	// Cache reads count toward the threshold and take the long-context rate too.
	if got := haiku.Cost(Tokens{Input: 60_001, CacheRead: 40_000, Total: 100_001}); got != 32_001 {
		t.Fatalf("long-context cost=%d", got)
	}
	if got := haiku.Reservation(4_096); got != 10_240 {
		t.Fatalf("reservation at the top rate=%d", got)
	}
}

func TestCostMatchesAxEstimator(t *testing.T) {
	cases := []struct {
		provider, model string
		tokens          Tokens
	}{
		{"anthropic", "claude-sonnet-5-5", Tokens{Input: 12_345, CacheRead: 80_000, CacheWrite: 5_000, Output: 2_222}},
		{"google-gemini", "gemini-3.5-flash-lite", Tokens{Input: 66_972, CacheRead: 78_519, Output: 1_823}},
		{"google-gemini", "gemini-2.5-pro", Tokens{Input: 150_000, CacheRead: 60_000, Output: 9_000}},
		{"openai", "gpt-6-sol", Tokens{Input: 300_000, CacheRead: 10_000, CacheWrite: 1_000, Output: 4_000}},
	}
	client := ax.NewAI("openai", ax.Object("api_key", "synthetic", "model", "gpt-6-luna")).(ax.AxAIService)
	for _, c := range cases {
		c.tokens.Total = c.tokens.Input + c.tokens.CacheRead + c.tokens.CacheWrite + c.tokens.Output
		want := client.GetEstimatedCost(ax.Object("ai", c.provider, "model", c.model, "tokens", ax.Object(
			"prompt_tokens", c.tokens.Input, "completion_tokens", c.tokens.Output,
			"cache_read_tokens", c.tokens.CacheRead, "cache_creation_tokens", c.tokens.CacheWrite)))
		got := mustLookup(t, c.provider, c.model).Cost(c.tokens)
		if want <= 0 || math.Abs(float64(got)-want*1_000_000) > 4 {
			t.Fatalf("%s: cost=%d micros, Ax estimate=%f USD", c.model, got, want)
		}
	}
}

func pinnedAxVersion(t *testing.T) string {
	t.Helper()
	mod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == axModule {
			return fields[1]
		}
	}
	t.Fatal("go.mod does not require the Ax module")
	return ""
}

func TestHostSnapshotMatchesPinnedModule(t *testing.T) {
	want, err := Export(pinnedAxVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "llm", "src", "spend", "ax-model-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("packages/llm/src/spend/ax-model-catalog.json is stale; run `pnpm --filter @neko/llm catalog:ax`")
	}
}
