package agent

import (
	"fmt"
	"strings"
)

// Structured-output modes are Ax deployment-profile vocabulary, not GraphJin
// invention. Ax resolves the requested mode against the profile's and model's
// declared capabilities and fails before transport when an explicit mode is
// unsupported, so GraphJin only has to carry the operator's choice through.
const (
	// StructuredOutputAuto lets the deployment profile and its model rules
	// pick the mechanism Ax has verified for that pairing. This is the default
	// and the right answer for almost every deployment.
	StructuredOutputAuto = "auto"
	// StructuredOutputNative requests native JSON Schema decoding.
	StructuredOutputNative = "native"
	// StructuredOutputFunction requests a synthetic function call.
	StructuredOutputFunction = "function"
	// StructuredOutputJSONObject requests a JSON object without strict schema
	// decoding.
	StructuredOutputJSONObject = "json_object"
)

// Legacy response_format values, kept so existing configuration keeps loading.
const (
	// ResponseFormatJSONSchema is the deprecated spelling of native decoding.
	ResponseFormatJSONSchema = "json_schema"
	// ResponseFormatJSONObject is the deprecated spelling of json_object.
	ResponseFormatJSONObject = "json_object"
)

// EffectiveStructuredOutputMode resolves the configured mode, folding in the
// deprecated agent.response_format alias. The canonical setting wins whenever
// it is set explicitly, so an operator migrating one key at a time never has to
// reason about which of the two is in effect.
func EffectiveStructuredOutputMode(mode, legacyResponseFormat string) string {
	if normalized := strings.ToLower(strings.TrimSpace(mode)); normalized != "" {
		return normalized
	}
	switch strings.ToLower(strings.TrimSpace(legacyResponseFormat)) {
	case ResponseFormatJSONSchema:
		return StructuredOutputNative
	case ResponseFormatJSONObject:
		return StructuredOutputJSONObject
	}
	return defaultStructuredOutputMode
}

// ValidateStructuredOutputMode rejects modes Ax does not define. Ax itself
// rejects a mode the deployment cannot serve; this only catches typos before a
// run starts.
func ValidateStructuredOutputMode(mode, legacyResponseFormat string) error {
	if legacy := strings.ToLower(strings.TrimSpace(legacyResponseFormat)); legacy != "" {
		switch legacy {
		case ResponseFormatJSONSchema, ResponseFormatJSONObject:
		default:
			return fmt.Errorf("agent.response_format must be one of %s, %s", ResponseFormatJSONSchema, ResponseFormatJSONObject)
		}
	}
	switch EffectiveStructuredOutputMode(mode, legacyResponseFormat) {
	case StructuredOutputAuto, StructuredOutputNative, StructuredOutputFunction, StructuredOutputJSONObject:
		return nil
	default:
		return fmt.Errorf("agent.structured_output_mode must be one of %s, %s, %s, %s",
			StructuredOutputAuto, StructuredOutputNative, StructuredOutputFunction, StructuredOutputJSONObject)
	}
}
