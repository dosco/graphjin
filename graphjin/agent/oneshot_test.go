package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// replyingClient answers the way a provider does under structured output: a
// JSON object whose keys are the signature's output fields. It records the
// options it was handed so a test can prove they reached the wire.
type replyingClient struct {
	content string
	err     error
	options []map[string]ax.Value
}

func (c *replyingClient) Chat(_ context.Context, _ map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.options = append(c.options, options)
	return map[string]ax.Value{
		"results": []ax.Value{map[string]ax.Value{"content": c.content}},
		"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{
			"prompt": 12, "completion": 6,
		}},
	}, nil
}

func (c *replyingClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *replyingClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

const testSignature = `"Answer the question."
question:string "What to answer."
-> answer:string "The answer."`

// The one-shot path is what every authoring call runs through, so it has to
// return the model's fields rather than the raw envelope.
func TestOneShotReturnsTheModelsFields(t *testing.T) {
	client := &replyingClient{content: `{"answer": "forty two"}`}
	fields, err := OneShotWithClient(context.Background(), client, Config{}.withDefaults(),
		testSignature, map[string]any{"question": "How many?"})
	if err != nil {
		t.Fatal(err)
	}
	if got := StringField(fields, "answer"); !strings.Contains(got, "forty two") {
		t.Fatalf("answer field = %q, fields = %+v", got, fields)
	}
}

// The portable controls have to reach the provider, or a service tier and an
// output mode configured for authoring would be silently ignored.
func TestOneShotCarriesThePortableControls(t *testing.T) {
	client := &replyingClient{content: `{"answer": "ok"}`}
	cfg := Config{ServiceTier: "flex", StructuredOutputMode: "json_object"}.withDefaults()
	if _, err := OneShotWithClient(context.Background(), client, cfg, testSignature,
		map[string]any{"question": "How many?"}); err != nil {
		t.Fatal(err)
	}
	if len(client.options) == 0 {
		t.Fatal("the client was never called")
	}
	options := client.options[0]
	if options["service_tier"] != "flex" {
		t.Fatalf("service tier did not reach the provider: %+v", options)
	}
	if options["structured_output_mode"] != "json_object" {
		t.Fatalf("structured output mode did not reach the provider: %+v", options)
	}
}

// A config assembled in code never passes through the service's normalization,
// so an unsupported value has to be caught here rather than becoming a puzzling
// transport error partway through an authoring run.
func TestOneShotValidatesTheConfigBeforeSpending(t *testing.T) {
	for name, cfg := range map[string]Config{
		"bad service tier":  {Model: "m", ServiceTier: "platinum"},
		"bad output mode":   {Model: "m", StructuredOutputMode: "telepathy"},
		"impossible limits": {Model: "m", RateLimit: RateLimitConfig{RequestsPerMinute: -5}},
	} {
		if _, err := OneShot(context.Background(), cfg, testSignature, nil); err == nil {
			t.Fatalf("%s should have been refused", name)
		}
	}
}

func TestOneShotRefusesAnEmptySignatureOrMissingClient(t *testing.T) {
	if _, err := OneShotWithClient(context.Background(), &replyingClient{}, Config{}.withDefaults(), "", nil); err == nil {
		t.Fatal("an empty signature must be refused")
	}
	if _, err := OneShotWithClient(context.Background(), nil, Config{}.withDefaults(), testSignature, nil); err == nil {
		t.Fatal("a missing client must be refused")
	}
}

// Authoring replies arrive as JSON inside a string field; this is the shape the
// whole authoring path depends on surviving the round trip.
func TestOneShotCarriesAJSONPayloadThrough(t *testing.T) {
	payload, err := json.Marshal([]map[string]string{{"table": "invoices"}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(map[string]string{"answer": string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	client := &replyingClient{content: string(content)}
	fields, err := OneShotWithClient(context.Background(), client, Config{}.withDefaults(),
		testSignature, map[string]any{"question": "picks"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]string
	if err := json.Unmarshal([]byte(StringField(fields, "answer")), &decoded); err != nil {
		t.Fatalf("the JSON payload did not survive: %q (%v)", StringField(fields, "answer"), err)
	}
	if len(decoded) != 1 || decoded[0]["table"] != "invoices" {
		t.Fatalf("decoded %+v", decoded)
	}
}
