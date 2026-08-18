package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSanitizeValueRemovesProviderCredentials(t *testing.T) {
	secret := "AIza-canary-secret-value-1234567890"
	value := map[string]any{
		"error": "Post https://provider.test/generate?key=" + secret + ": context deadline exceeded",
		"trace": []any{"Authorization: Bearer bearer-canary", map[string]any{"token": secret}},
	}
	got := SanitizeValue(value, secret)
	text := strings.ToLower(strings.TrimSpace(stringify(got)))
	for _, leaked := range []string{strings.ToLower(secret), "bearer-canary"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("sanitized value leaked %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, "[redacted]") {
		t.Fatalf("sanitized value has no redaction marker: %s", text)
	}
}

func TestSanitizeTextRedactsRecognizableProviderPatternsAndJSONAuthorization(t *testing.T) {
	openAI := "sk-proj-abcdefghijklmnopqrstuvwxyz0123456789"
	anthropic := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	input := `{"Authorization":"Bearer abc.def.ghi","openai":"` + openAI + `","anthropic":"` + anthropic + `"}`
	got := SanitizeText(input)
	for _, secret := range []string{"abc.def.ghi", openAI, anthropic} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized text retained secret %q: %s", secret, got)
		}
	}
}

func TestPublicErrorInfoClassifiesProviderTimeout(t *testing.T) {
	info := PublicErrorInfo(context.DeadlineExceeded, "google-gemini", "gemini-test")
	if info.Extensions["code"] != ErrorCodeProviderTimeout || info.Extensions["retryable"] != true {
		t.Fatalf("classification = %+v", info)
	}
	if strings.Contains(strings.ToLower(info.Message), "context deadline") {
		t.Fatalf("public message retained raw error: %+v", info)
	}
}

func TestProviderQuotaWinsOverGoogleResourceExhaustedFallback(t *testing.T) {
	classification := ClassifyProviderError(errors.New("RESOURCE_EXHAUSTED: quota exceeded for this project"))
	if classification.Code != ErrorCodeProviderQuota || classification.Retryable {
		t.Fatalf("quota classification = %+v", classification)
	}
}

// A DeepSeek benchmark run drained its account mid-suite. "Insufficient
// Balance" matched none of the quota phrasings, so it classified as a generic
// agent error and the harness scored 204 remaining episodes as model failures
// instead of halting the run on an environment problem.
func TestProviderQuotaRecognizesBalanceExhaustion(t *testing.T) {
	for _, message := range []string{
		"Insufficient Balance",
		"error, status code: 402, message: Insufficient Balance",
		"insufficient funds for this request",
		"Payment Required",
	} {
		classification := ClassifyProviderError(errors.New(message))
		if classification.Code != ErrorCodeProviderQuota {
			t.Fatalf("ClassifyProviderError(%q) = %+v, want %s", message, classification, ErrorCodeProviderQuota)
		}
		if classification.Retryable {
			t.Fatalf("ClassifyProviderError(%q) must not be retryable; a drained account does not refill on backoff", message)
		}
	}
}

func TestProviderAuthRecognizesGoogleUnauthenticatedResponse(t *testing.T) {
	classification := ClassifyProviderError(errors.New("status: UNAUTHENTICATED: Request had invalid authentication credentials"))
	if classification.Code != ErrorCodeProviderAuth || classification.Retryable {
		t.Fatalf("auth classification = %+v", classification)
	}
}

func TestProviderRateLimitRecognizesGoogleQueueExhaustion(t *testing.T) {
	classification := ClassifyProviderError(errors.New("code:429 message:The request queue is full. status:RESOURCE_EXHAUSTED"))
	if classification.Code != ErrorCodeProviderRateLimit || !classification.Retryable {
		t.Fatalf("rate-limit classification = %+v", classification)
	}
}
