package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/openapi"
)

func (s *gstate) executeOpenAPIMutation(ctx context.Context) (bool, error) {
	if s == nil || s.r.operation != qcode.QTMutation || s.cs == nil || s.cs.st.qc == nil || s.cs.st.qc.SType != qcode.QTOpenAPICall {
		return false, nil
	}
	qc := s.cs.st.qc
	if len(qc.Roots) != 1 || len(qc.Mutates) != 1 {
		return true, fmt.Errorf("openapi mutations support exactly one root operation")
	}
	rootID := qc.Roots[0]
	sel := &qc.Selects[rootID]
	if sel.Ti.Type != "openapi_mutation" {
		return true, fmt.Errorf("openapi: invalid mutation root %q", sel.Ti.Name)
	}
	if s.gj.openapiRuntime == nil {
		return true, fmt.Errorf("openapi: runtime not initialized")
	}
	op, caller, ok := s.gj.openapiRuntime.MutationByRoot(sel.Ti.Name)
	if !ok {
		return true, fmt.Errorf("openapi: mutation root %q is not active", sel.Ti.Name)
	}
	event := openAPIMutationCompletion{
		Event:       "openapi_mutation_completion",
		SourceName:  op.SourceName,
		SpecKey:     op.SpecKey,
		OperationID: op.OperationID,
		Method:      op.Method,
		RoleClass:   s.role,
		RequestID:   openapi.NewRequestID(),
		Outcome:     "rejected",
		Gate:        "operation",
	}
	started := time.Now()
	defer func() {
		event.DurationMS = time.Since(started).Milliseconds()
		s.logOpenAPIMutationCompletion(event)
	}()
	decision := s.gj.conf.authorizeOpenAPIOperation(ctx, op, s.role)
	event.Authorization = "denied"
	event.Gate = decision.Gate
	if !decision.Allowed {
		return true, fmt.Errorf("openapi: unauthorized (%s): %s", decision.Gate, decision.Reason)
	}
	event.Authorization = "allowed"
	event.Gate = "input"

	value, err := managedNodeToValue(qc.Mutates[0].Data, s.vmap)
	if err != nil {
		return true, fmt.Errorf("openapi: decode call: %w", err)
	}
	call, ok := value.(map[string]interface{})
	if !ok {
		return true, fmt.Errorf("openapi: call must be a JSON object")
	}
	params, err := op.ResolveMutationCall(call)
	if err != nil {
		return true, err
	}
	params.RequestID = event.RequestID
	event.RequestBytes = int64(len(params.BodyJSON))
	event.RequestSHA256 = hashOpenAPIBody(params.BodyJSON)
	event.Gate = "upstream"
	result, err := caller.CallMutation(ctx, params)
	event.StatusCode = result.StatusCode
	event.ResponseBytes = result.ResponseBytes
	event.ResponseSHA256 = hashOpenAPIBody(result.Body)
	event.RetryCount = result.RetryCount
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			event.Outcome = "cancelled"
		case errors.Is(err, context.DeadlineExceeded):
			event.Outcome = "timeout"
		case strings.Contains(err.Error(), "outcome may be ambiguous"):
			event.Outcome = "ambiguous"
		default:
			event.Outcome = "upstream_error"
		}
		return true, err
	}

	response := map[string]interface{}{
		"ok":            true,
		"status_code":   result.StatusCode,
		"operation_id":  op.OperationID,
		"request_id":    result.RequestID,
		"response_json": nil,
	}
	if len(result.Body) != 0 {
		var body interface{}
		if err := json.Unmarshal(result.Body, &body); err != nil {
			return true, fmt.Errorf("openapi: decode successful response: %w", err)
		}
		response["response_json"] = body
		if object, ok := body.(map[string]interface{}); ok {
			for key, item := range object {
				if _, reserved := response[key]; !reserved {
					response[key] = item
				}
			}
		}
	}
	event.Outcome = "success"
	event.Gate = "allowed"

	selected := make(map[string]interface{}, len(sel.Fields))
	for _, field := range sel.Fields {
		if field.Type != qcode.FieldTypeCol {
			continue
		}
		selected[field.FieldName] = response[field.Col.Name]
	}
	payload, err := json.Marshal(map[string]interface{}{sel.FieldName: selected})
	if err != nil {
		return true, err
	}
	s.data = payload
	s.dhash = sha256.Sum256(s.data)
	s.data, err = encryptValues(s.data, s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	return true, err
}

type openAPIMutationCompletion struct {
	Event          string `json:"event"`
	SourceName     string `json:"source_name"`
	SpecKey        string `json:"spec_key"`
	OperationID    string `json:"operation_id"`
	Method         string `json:"method"`
	RoleClass      string `json:"role_class"`
	Authorization  string `json:"authorization"`
	Gate           string `json:"gate"`
	RequestID      string `json:"request_id"`
	RequestBytes   int64  `json:"request_bytes"`
	RequestSHA256  string `json:"request_sha256,omitempty"`
	ResponseBytes  int64  `json:"response_bytes"`
	ResponseSHA256 string `json:"response_sha256,omitempty"`
	StatusCode     int    `json:"upstream_status,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	Outcome        string `json:"outcome"`
	RetryCount     int    `json:"retry_count"`
}

func (s *gstate) logOpenAPIMutationCompletion(event openAPIMutationCompletion) {
	if s == nil || s.gj == nil || s.gj.log == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.gj.log.Printf("%s", payload)
}

func hashOpenAPIBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	hash := sha256.Sum256(body)
	return fmt.Sprintf("%x", hash[:])
}
