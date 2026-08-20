package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	DefaultMaxRequestBytes  int64 = 64 * 1024
	DefaultMaxResponseBytes int64 = 1024 * 1024
)

var forbiddenCallerHeaders = map[string]struct{}{
	"authorization": {}, "proxy-authenticate": {}, "proxy-authorization": {}, "host": {}, "cookie": {}, "set-cookie": {},
	"connection": {}, "proxy-connection": {}, "keep-alive": {}, "te": {}, "trailer": {},
	"transfer-encoding": {}, "upgrade": {}, "accept": {}, "content-encoding": {}, "content-length": {}, "content-type": {},
	"forwarded": {}, "x-forwarded-for": {}, "x-forwarded-host": {}, "x-forwarded-proto": {},
	"via": {}, "x-real-ip": {}, "x-request-id": {},
}

// ResolveMutationCall validates the stable GraphQL call envelope and converts
// it to the already-normalized values consumed by Caller. It performs all
// schema and size checks before a concurrency/rate-limit slot is acquired.
func (op *OpDescriptor) ResolveMutationCall(call map[string]interface{}) (CallParams, error) {
	p := CallParams{PathValues: map[string]string{}, QueryValues: map[string]string{}, HeaderValues: map[string]string{}}
	if op == nil || op.Mode != OpModeMutation {
		return p, fmt.Errorf("openapi: operation is not an exposed mutation")
	}
	for key := range call {
		switch key {
		case "path", "query", "headers", "body":
		default:
			return p, fmt.Errorf("openapi: unknown call field %q", key)
		}
	}

	path, err := envelopeObject(call, "path")
	if err != nil {
		return p, err
	}
	query, err := envelopeObject(call, "query")
	if err != nil {
		return p, err
	}
	headers, err := envelopeObject(call, "headers")
	if err != nil {
		return p, err
	}
	if err := resolveParameterValues(op.PathParams, path, op.Defaults, p.PathValues, ParamInPath, op.JSONSchema2020); err != nil {
		return p, err
	}
	if err := resolveParameterValues(op.QueryParams, query, op.Defaults, p.QueryValues, ParamInQuery, op.JSONSchema2020); err != nil {
		return p, err
	}
	if err := resolveParameterValues(op.HeaderParams, headers, nil, p.HeaderValues, ParamInHeader, op.JSONSchema2020); err != nil {
		return p, err
	}

	body, hasBody := call["body"]
	if op.RequestBodyRequired && !hasBody {
		return p, fmt.Errorf("openapi: request body is required for %s", op.OperationID)
	}
	if hasBody && (op.RequestBodySchema == nil || op.RequestBodySchema.Value == nil) {
		return p, fmt.Errorf("openapi: request body is not declared for %s", op.OperationID)
	}
	if hasBody {
		if err := validateRequestSchemaValue(op.RequestBodySchema, body, op.JSONSchema2020); err != nil {
			return p, fmt.Errorf("openapi: invalid request body for %s: %w", op.OperationID, err)
		}
		p.BodyJSON, err = json.Marshal(body)
		if err != nil {
			return p, fmt.Errorf("openapi: encode request body: %w", err)
		}
		limit := op.MaxRequestBytes
		if limit <= 0 {
			limit = DefaultMaxRequestBytes
		}
		if int64(len(p.BodyJSON)) > limit {
			return p, fmt.Errorf("openapi: request body exceeds max_request_bytes (%d > %d)", len(p.BodyJSON), limit)
		}
	}
	return p, nil
}

func envelopeObject(call map[string]interface{}, key string) (map[string]interface{}, error) {
	v, ok := call[key]
	if !ok || v == nil {
		return map[string]interface{}{}, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("openapi: call.%s must be an object", key)
	}
	return m, nil
}

func resolveParameterValues(specs []ParamSpec, values map[string]interface{}, defaults map[string]string, out map[string]string, location ParamLocation, jsonSchema2020 bool) error {
	header := location == ParamInHeader
	declared := make(map[string]ParamSpec, len(specs))
	canonical := make(map[string]string, len(specs))
	for _, spec := range specs {
		key := spec.Name
		if header {
			key = strings.ToLower(key)
		}
		declared[key] = spec
		canonical[key] = spec.Name
	}
	for name, value := range values {
		key := name
		if header {
			key = strings.ToLower(key)
		}
		spec, ok := declared[key]
		if !ok {
			return fmt.Errorf("openapi: undeclared %s parameter %q", location, name)
		}
		if header && forbiddenHeader(name) {
			return fmt.Errorf("openapi: caller header %q is forbidden", name)
		}
		encoded, err := encodePrimitive(spec, value, jsonSchema2020)
		if err != nil {
			return err
		}
		out[canonical[key]] = encoded
	}
	for _, spec := range specs {
		if _, ok := out[spec.Name]; ok {
			continue
		}
		if value, ok := defaults[spec.Name]; ok && !header {
			out[spec.Name] = value
			continue
		}
		if spec.Required {
			return fmt.Errorf("openapi: required %s parameter %q missing", spec.In, spec.Name)
		}
	}
	return nil
}

func encodePrimitive(spec ParamSpec, value interface{}, jsonSchema2020 bool) (string, error) {
	if err := validateRequestSchemaValue(spec.Schema, value, jsonSchema2020); err != nil {
		return "", fmt.Errorf("openapi: invalid %s parameter %q: %w", spec.In, spec.Name, err)
	}
	switch spec.Type {
	case "string":
		v, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("openapi: %s parameter %q must be a string", spec.In, spec.Name)
		}
		return v, nil
	case "boolean":
		v, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("openapi: %s parameter %q must be a boolean", spec.In, spec.Name)
		}
		if v {
			return "true", nil
		}
		return "false", nil
	case "integer":
		v, ok := integerValue(value)
		if !ok {
			return "", fmt.Errorf("openapi: %s parameter %q must be an integer", spec.In, spec.Name)
		}
		return v, nil
	case "number":
		v, ok := numericValue(value)
		if !ok {
			return "", fmt.Errorf("openapi: %s parameter %q must be a number", spec.In, spec.Name)
		}
		return fmt.Sprint(v), nil
	default:
		return "", fmt.Errorf("openapi: %s parameter %q has unsupported type %q", spec.In, spec.Name, spec.Type)
	}
}

func validateRequestSchemaValue(schema *openapi3.SchemaRef, value interface{}, jsonSchema2020 bool) error {
	if schema == nil || schema.Value == nil {
		return nil
	}
	opts := []openapi3.SchemaValidationOption{openapi3.SetSchemaErrorMessageCustomizer(redactedSchemaErrorMessage)}
	if jsonSchema2020 {
		opts = append(opts, openapi3.EnableJSONSchema2020())
	}
	if err := schema.Value.VisitJSON(value, opts...); err != nil {
		// JSON Schema 2020 errors do not use the message customizer, and either
		// validator may include the rejected value or property path. Never wrap
		// the original error into a caller-visible request rejection.
		var schemaErr *openapi3.SchemaError
		if errors.As(err, &schemaErr) {
			return errors.New(redactedSchemaErrorMessage(schemaErr))
		}
		return errors.New("schema validation failed")
	}
	return nil
}

func redactedSchemaErrorMessage(err *openapi3.SchemaError) string {
	if err == nil {
		return "schema validation failed"
	}
	message := "schema validation failed"
	if field := strings.TrimSpace(err.SchemaField); field != "" {
		message += ": does not satisfy " + field
	}
	return message
}

func integerValue(value interface{}) (string, bool) {
	switch v := value.(type) {
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		f := float64(v)
		if math.Trunc(f) == f {
			return strconv.FormatFloat(f, 'f', 0, 64), true
		}
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatFloat(v, 'f', 0, 64), true
		}
	case json.Number:
		if _, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return string(v), true
		}
		if _, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return string(v), true
		}
	}
	return "", false
}

func numericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func forbiddenHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if _, ok := forbiddenCallerHeaders[lower]; ok {
		return true
	}
	return strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "x-forwarded-") || http.CanonicalHeaderKey(name) == ""
}
