package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

const (
	ErrorCodeAgentError               = "agent_error"
	ErrorCodeProviderTimeout          = "provider_timeout"
	ErrorCodeProviderRateLimit        = "provider_rate_limit"
	ErrorCodeProviderTransport        = "provider_transport"
	ErrorCodeProviderServer           = "provider_5xx"
	ErrorCodeProviderAuth             = "provider_auth"
	ErrorCodeProviderQuota            = "provider_quota"
	ErrorCodeProviderModelUnavailable = "provider_model_unavailable"
)

var (
	secretQueryPattern = regexp.MustCompile(`(?i)([?&](?:key|api_key|token|access_token)=)[^&\s"']+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:bearer\s+)?)[^,;\s"']+`)
	bearerPattern      = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+\-/=]+`)
	googleKeyPattern   = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}\b`)
	providerKeyPattern = regexp.MustCompile(`\b(?:sk-(?:proj-|ant-api03-)?[A-Za-z0-9_-]{20,}|xai-[A-Za-z0-9_-]{20,}|gsk_[A-Za-z0-9_-]{20,})\b`)
)

// SanitizeText removes credentials from arbitrary provider and transport
// messages. Callers should also pass the configured secret value so custom
// key formats are removed even when they do not match a known pattern.
func SanitizeText(value string, secrets ...string) string {
	value = secretQueryPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = authHeaderPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = bearerPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = googleKeyPattern.ReplaceAllString(value, `[REDACTED]`)
	value = providerKeyPattern.ReplaceAllString(value, `[REDACTED]`)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

// SanitizeValue returns a JSON-plain deep copy with every string redacted.
// Evaluation storage uses this as a defense in depth for provider responses,
// traces, action logs, and errors nested inside interface values.
func SanitizeValue(value any, secrets ...string) any {
	data, err := json.Marshal(value)
	if err != nil {
		return SanitizeText(fmt.Sprint(value), secrets...)
	}
	var plain any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&plain); err != nil {
		return SanitizeText(string(data), secrets...)
	}
	return sanitizePlainValue(plain, secrets)
}

func sanitizePlainValue(value any, secrets []string) any {
	switch typed := value.(type) {
	case string:
		return SanitizeText(typed, secrets...)
	case []any:
		for i := range typed {
			typed[i] = sanitizePlainValue(typed[i], secrets)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = sanitizePlainValue(item, secrets)
		}
		return typed
	default:
		return value
	}
}

// SanitizeResponse deep-copies and redacts a structured agent response.
func SanitizeResponse(response Response, secrets ...string) Response {
	plain := SanitizeValue(response, secrets...)
	data, err := json.Marshal(plain)
	if err != nil {
		return response
	}
	var sanitized Response
	if err := json.Unmarshal(data, &sanitized); err != nil {
		return response
	}
	return sanitized
}

// ProviderErrorClassification is safe to persist and expose. It deliberately
// contains no raw provider response or URL.
type ProviderErrorClassification struct {
	Code      string
	Message   string
	Retryable bool
}

// The public classifications. Both the typed and the string path answer with
// these, so a category always reaches callers with one wording.
var (
	timeoutClassification   = ProviderErrorClassification{Code: ErrorCodeProviderTimeout, Message: "The model provider did not respond before the configured timeout.", Retryable: true}
	quotaClassification     = ProviderErrorClassification{Code: ErrorCodeProviderQuota, Message: "The model provider quota or credits are exhausted."}
	rateLimitClassification = ProviderErrorClassification{Code: ErrorCodeProviderRateLimit, Message: "The model provider is temporarily rate-limited.", Retryable: true}
	authClassification      = ProviderErrorClassification{Code: ErrorCodeProviderAuth, Message: "The model provider rejected the configured credentials."}
	modelClassification     = ProviderErrorClassification{Code: ErrorCodeProviderModelUnavailable, Message: "The configured model is unavailable."}
	serverClassification    = ProviderErrorClassification{Code: ErrorCodeProviderServer, Message: "The model provider returned a temporary server error.", Retryable: true}
	transportClassification = ProviderErrorClassification{Code: ErrorCodeProviderTransport, Message: "The model provider request failed in transport.", Retryable: true}
	agentClassification     = ProviderErrorClassification{Code: ErrorCodeAgentError, Message: "The GraphJin agent could not complete the request."}
)

// ClassifyProviderError maps provider failures to stable public categories.
// The typed ax envelope is read first because it carries the status the
// provider actually returned, where Error() renders only the message: Cerebras
// reports its token bucket without ever saying "rate limit", and 236 throttled
// benchmark episodes scored as model failures while a 429 sat unread in the
// error. Typed cancellation and deadline checks come next, then the string
// matching that remains the only signal for stringified errors -- eval
// rescoring reclassifies from a stored message -- and for provider SDKs that
// expose no structure at all.
func ClassifyProviderError(err error) ProviderErrorClassification {
	if err == nil {
		return ProviderErrorClassification{}
	}
	message := strings.ToLower(err.Error())
	if classification, ok := classifyAxEnvelope(err, message); ok {
		return classification
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "context deadline exceeded"), strings.Contains(message, "deadline exceeded"), strings.Contains(message, "request timeout"), strings.Contains(message, "timed out"):
		return timeoutClassification
	case quotaExhausted(message):
		return quotaClassification
	case strings.Contains(message, "rate limit"), strings.Contains(message, "too many requests"), strings.Contains(message, "resource exhausted"), strings.Contains(message, "resource_exhausted"), strings.Contains(message, "request queue is full"), strings.Contains(message, "tokens per minute limit exceeded"), strings.Contains(message, "requests per minute limit exceeded"):
		return rateLimitClassification
	case strings.Contains(message, "invalid api key"), strings.Contains(message, "invalid x-api-key"), strings.Contains(message, "incorrect api key"), strings.Contains(message, "api key not valid"), strings.Contains(message, "api_key_invalid"), strings.Contains(message, "authentication_error"), strings.Contains(message, "invalid authentication credentials"), strings.Contains(message, "unauthenticated"), strings.Contains(message, "unauthorized"):
		return authClassification
	case strings.Contains(message, "model_not_found"), strings.Contains(message, "model not found"), strings.Contains(message, "model is not available"), strings.Contains(message, "does not exist or you do not have access to it"), strings.Contains(message, "models/") && strings.Contains(message, "is not found"):
		return modelClassification
	case strings.Contains(message, "status code 500"), strings.Contains(message, "status code 502"), strings.Contains(message, "status code 503"), strings.Contains(message, "status code 504"), strings.Contains(message, "http 500"), strings.Contains(message, "http 502"), strings.Contains(message, "http 503"), strings.Contains(message, "http 504"), strings.Contains(message, "error 500"), strings.Contains(message, "error 502"), strings.Contains(message, "error 503"), strings.Contains(message, "error 504"), strings.Contains(message, "internal server error"), strings.Contains(message, "service unavailable"), strings.Contains(message, "bad gateway"), strings.Contains(message, "gateway timeout"):
		return serverClassification
	case strings.Contains(message, "connection reset"), strings.Contains(message, "connection refused"), strings.Contains(message, "no such host"), strings.Contains(message, "network is unreachable"), strings.Contains(message, "tls handshake"), strings.Contains(message, "unexpected eof"), strings.TrimSpace(message) == "eof":
		return transportClassification
	default:
		return agentClassification
	}
}

// DeepSeek says "Insufficient Balance" and others say "insufficient funds";
// neither matched the older phrasings, so an exhausted account read as a
// generic agent error. A benchmark run then scored every remaining episode
// as a model failure instead of halting: 204 zeros from a billing problem.
func quotaExhausted(message string) bool {
	switch {
	case strings.Contains(message, "no credits remaining"), strings.Contains(message, "credit balance"), strings.Contains(message, "insufficient balance"), strings.Contains(message, "insufficient funds"), strings.Contains(message, "insufficient_quota"), strings.Contains(message, "quota exceeded"), strings.Contains(message, "billing"), strings.Contains(message, "payment required"), strings.Contains(message, "status code 402"):
		return true
	default:
		return false
	}
}

// axEnvelope digs the structured ax error out of err. AIServiceError embeds
// AxError rather than wrapping it, so the concrete type has to be checked
// directly for ax releases before it grew an Unwrap; the AxError branch covers
// plain envelopes, wrapped errors, and every release after.
func axEnvelope(err error) (ax.AxError, bool) {
	var serviceErr ax.AIServiceError
	if errors.As(err, &serviceErr) {
		return serviceErr.AxError, true
	}
	var envelope ax.AxError
	if errors.As(err, &envelope) {
		return envelope, true
	}
	return ax.AxError{}, false
}

// classifyAxEnvelope maps the provider's own status onto a public category.
// Only statuses with a single honest reading are claimed; anything else falls
// through to the message rather than guessing. A 429 is the one status that
// still needs the message: providers bill exhausted credit as 429 as well, and
// quota is terminal where a rate limit is worth waiting out.
func classifyAxEnvelope(err error, message string) (ProviderErrorClassification, bool) {
	envelope, ok := axEnvelope(err)
	if !ok {
		return ProviderErrorClassification{}, false
	}
	switch envelope.Status {
	case 401, 403:
		return authClassification, true
	case 402:
		return quotaClassification, true
	case 408:
		return timeoutClassification, true
	case 429:
		// The provider's own error code carries the distinction when the
		// message is terse, so both are searched.
		if quotaExhausted(message + " " + strings.ToLower(envelope.Code)) {
			return quotaClassification, true
		}
		return rateLimitClassification, true
	case 500, 502, 503, 504, 529:
		return serverClassification, true
	}
	if envelope.Status != 0 {
		return ProviderErrorClassification{}, false
	}
	// No status reached us, but ax still names transports it failed to complete.
	switch {
	case envelope.Type == "AxAIServiceTimeoutError":
		return timeoutClassification, true
	case envelope.Type == "AxAIServiceNetworkError", envelope.Category == "network":
		return transportClassification, true
	}
	return ProviderErrorClassification{}, false
}

// PublicErrorInfo produces the only provider error payload GraphJin surfaces.
func PublicErrorInfo(err error, provider, model string, secrets ...string) ErrorInfo {
	classification := ClassifyProviderError(err)
	message := classification.Message
	if classification.Code == ErrorCodeAgentError {
		message = SanitizeText(err.Error(), secrets...)
	}
	extensions := map[string]any{"code": classification.Code, "retryable": classification.Retryable}
	if provider = strings.TrimSpace(provider); provider != "" {
		extensions["provider"] = provider
	}
	if model = strings.TrimSpace(model); model != "" {
		extensions["model"] = model
	}
	return ErrorInfo{Message: message, Extensions: extensions}
}

// PublicError returns a sanitized error suitable for logs, traces, and audit
// stores. Structured consumers should use PublicErrorInfo.
func PublicError(err error, provider, model string, secrets ...string) error {
	if err == nil {
		return nil
	}
	info := PublicErrorInfo(err, provider, model, secrets...)
	return errors.New(info.Message)
}
