package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

// ClassifyProviderError maps provider failures to stable public categories.
// Typed cancellation and deadline checks win over the compatibility string
// matching used for provider SDKs that do not expose structured errors.
func ClassifyProviderError(err error) ProviderErrorClassification {
	if err == nil {
		return ProviderErrorClassification{}
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "context deadline exceeded"), strings.Contains(message, "deadline exceeded"), strings.Contains(message, "request timeout"), strings.Contains(message, "timed out"):
		return ProviderErrorClassification{Code: ErrorCodeProviderTimeout, Message: "The model provider did not respond before the configured timeout.", Retryable: true}
	case strings.Contains(message, "no credits remaining"), strings.Contains(message, "credit balance"), strings.Contains(message, "insufficient_quota"), strings.Contains(message, "quota exceeded"), strings.Contains(message, "billing"):
		return ProviderErrorClassification{Code: ErrorCodeProviderQuota, Message: "The model provider quota or credits are exhausted."}
	case strings.Contains(message, "rate limit"), strings.Contains(message, "too many requests"), strings.Contains(message, "resource exhausted"), strings.Contains(message, "resource_exhausted"), strings.Contains(message, "request queue is full"):
		return ProviderErrorClassification{Code: ErrorCodeProviderRateLimit, Message: "The model provider is temporarily rate-limited.", Retryable: true}
	case strings.Contains(message, "invalid api key"), strings.Contains(message, "invalid x-api-key"), strings.Contains(message, "incorrect api key"), strings.Contains(message, "api key not valid"), strings.Contains(message, "api_key_invalid"), strings.Contains(message, "authentication_error"), strings.Contains(message, "invalid authentication credentials"), strings.Contains(message, "unauthenticated"), strings.Contains(message, "unauthorized"):
		return ProviderErrorClassification{Code: ErrorCodeProviderAuth, Message: "The model provider rejected the configured credentials."}
	case strings.Contains(message, "model_not_found"), strings.Contains(message, "model not found"), strings.Contains(message, "model is not available"), strings.Contains(message, "does not exist or you do not have access to it"), strings.Contains(message, "models/") && strings.Contains(message, "is not found"):
		return ProviderErrorClassification{Code: ErrorCodeProviderModelUnavailable, Message: "The configured model is unavailable."}
	case strings.Contains(message, "status code 500"), strings.Contains(message, "status code 502"), strings.Contains(message, "status code 503"), strings.Contains(message, "status code 504"), strings.Contains(message, "http 500"), strings.Contains(message, "http 502"), strings.Contains(message, "http 503"), strings.Contains(message, "http 504"), strings.Contains(message, "error 500"), strings.Contains(message, "error 502"), strings.Contains(message, "error 503"), strings.Contains(message, "error 504"), strings.Contains(message, "internal server error"), strings.Contains(message, "service unavailable"), strings.Contains(message, "bad gateway"), strings.Contains(message, "gateway timeout"):
		return ProviderErrorClassification{Code: ErrorCodeProviderServer, Message: "The model provider returned a temporary server error.", Retryable: true}
	case strings.Contains(message, "connection reset"), strings.Contains(message, "connection refused"), strings.Contains(message, "no such host"), strings.Contains(message, "network is unreachable"), strings.Contains(message, "tls handshake"), strings.Contains(message, "unexpected eof"), strings.TrimSpace(message) == "eof":
		return ProviderErrorClassification{Code: ErrorCodeProviderTransport, Message: "The model provider request failed in transport.", Retryable: true}
	default:
		return ProviderErrorClassification{Code: ErrorCodeAgentError, Message: "The GraphJin agent could not complete the request."}
	}
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
