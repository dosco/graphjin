package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
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

// Cerebras reports its token bucket without the literal phrase "rate limit".
// Missing this wording turned 236 throttled benchmark episodes into permanent
// agent failures, so the harness scored provider weather as model quality.
func TestProviderRateLimitRecognizesCerebrasTokenBucket(t *testing.T) {
	for _, message := range []string{
		"Tokens per minute limit exceeded - too many tokens processed.",
		"Requests per minute limit exceeded.",
	} {
		classification := ClassifyProviderError(errors.New(message))
		if classification.Code != ErrorCodeProviderRateLimit || !classification.Retryable {
			t.Fatalf("ClassifyProviderError(%q) = %+v, want retryable %s", message, classification, ErrorCodeProviderRateLimit)
		}
	}
}

// axStatusError is the shape ax hands GraphJin once it has mapped a provider's
// HTTP response: the status the provider returned, and a message GraphJin has
// no control over.
func axStatusError(status int, code, message string) error {
	return ax.AIServiceError{AxError: ax.AxError{
		Category: "ai", Type: "AxAIServiceStatusError",
		Message: message, Status: status, Code: code, Retryable: status == 429,
	}}
}

// The phrase list only ever covers wording someone has already been burned by.
// Reading the status ax already carried means the next provider to invent its
// own throttle sentence is classified correctly the first time.
func TestProviderClassificationPrefersTypedStatusOverWording(t *testing.T) {
	throttled := axStatusError(429, "", "Throughput cap reached for this deployment.")
	for name, err := range map[string]error{
		"direct":  throttled,
		"wrapped": fmt.Errorf("actor step 3: %w", throttled),
	} {
		classification := ClassifyProviderError(err)
		if classification.Code != ErrorCodeProviderRateLimit || !classification.Retryable {
			t.Fatalf("%s: classification = %+v, want retryable %s", name, classification, ErrorCodeProviderRateLimit)
		}
	}
}

// Providers bill an exhausted account as 429 as well. Quota is terminal, so the
// message and the provider's own code still decide that one; treating it as a
// rate limit would retry an account that cannot recover.
func TestProviderClassificationKeepsExhaustedQuotaTerminalAt429(t *testing.T) {
	for name, err := range map[string]error{
		"code only":    axStatusError(429, "insufficient_quota", "Too many requests."),
		"message only": axStatusError(429, "", "You exceeded your current quota, please check your plan and billing details."),
	} {
		classification := ClassifyProviderError(err)
		if classification.Code != ErrorCodeProviderQuota || classification.Retryable {
			t.Fatalf("%s: classification = %+v, want terminal %s", name, classification, ErrorCodeProviderQuota)
		}
	}
}

func TestProviderClassificationMapsTypedStatuses(t *testing.T) {
	cases := map[int]ProviderErrorClassification{
		401: authClassification,
		403: authClassification,
		402: quotaClassification,
		408: timeoutClassification,
		500: serverClassification,
		503: serverClassification,
		529: serverClassification,
	}
	for status, want := range cases {
		classification := ClassifyProviderError(axStatusError(status, "", "provider said something unfamiliar"))
		if classification != want {
			t.Fatalf("status %d classified %+v, want %+v", status, classification, want)
		}
	}
}

// A status with more than one honest reading is left to the message rather than
// guessed at: 404 is a missing model as often as a bad route.
func TestProviderClassificationFallsBackToMessageForUnclaimedStatus(t *testing.T) {
	classification := ClassifyProviderError(axStatusError(404, "", "The model `gemma-4-31b` does not exist or you do not have access to it."))
	if classification.Code != ErrorCodeProviderModelUnavailable {
		t.Fatalf("classification = %+v, want %s", classification, ErrorCodeProviderModelUnavailable)
	}
}

func TestProviderClassificationReadsTransportsWithoutStatus(t *testing.T) {
	classification := ClassifyProviderError(fmt.Errorf("chat: %w", error(ax.AxError{Category: "network", Message: "socket hang up"})))
	if classification.Code != ErrorCodeProviderTransport || !classification.Retryable {
		t.Fatalf("classification = %+v, want retryable %s", classification, ErrorCodeProviderTransport)
	}
}
