package agent

import (
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestGeminiStagesUseNativeStructuredOutput(t *testing.T) {
	options := ax.Object()
	modelOptions(options, &RoutedClient{DefaultProvider: "google-gemini"}, "medium")
	for _, key := range stageKeys {
		stage, _ := options[key].(map[string]ax.Value)
		if stage["structuredOutputMode"] != "native" {
			t.Fatalf("%s structuredOutputMode = %v, want native", key, stage["structuredOutputMode"])
		}
	}
	summarizer, _ := options["summarizerOptions"].(map[string]ax.Value)
	if options["structuredOutputMode"] != "native" || summarizer["structuredOutputMode"] != "native" {
		t.Fatalf("top-level and summarizer modes = %v, %v; want native", options["structuredOutputMode"], summarizer["structuredOutputMode"])
	}
}
