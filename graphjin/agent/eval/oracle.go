package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type OracleResult struct {
	Value     string `json:"value"`
	Dimension string `json:"dimension,omitempty"`
	// DimensionAlternates are other identifiers that name the same row. A task
	// asking "which record" is answered by any unambiguous identifier, so the
	// oracle carries every one it selected rather than forcing the one field it
	// happened to project.
	DimensionAlternates []string       `json:"dimension_alternates,omitempty"`
	Data                any            `json:"data,omitempty"`
	Variables           map[string]any `json:"variables,omitempty"`
}

type Verifier struct {
	Client  HTTPDoer
	Now     func() time.Time
	BaseURL string
	Headers map[string]string
}

func (v Verifier) Resolve(ctx context.Context, oracle OracleSpec) (OracleResult, error) {
	if err := oracle.Validate(); err != nil {
		return OracleResult{}, err
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	anchor := ""
	if strings.TrimSpace(oracle.AnchorQuery) != "" {
		anchorData, err := postGraphQL(ctx, client, v.BaseURL, v.Headers, oracle.AnchorQuery, nil)
		if err != nil {
			return OracleResult{}, fmt.Errorf("anchor query: %w", err)
		}
		raw, ok := walkPath(anchorData, oracle.AnchorExtract)
		if !ok {
			return OracleResult{}, fmt.Errorf("anchor extract %q not found", oracle.AnchorExtract)
		}
		anchor = valueString(raw)
		if anchor == "" {
			return OracleResult{}, fmt.Errorf("anchor extract %q is empty", oracle.AnchorExtract)
		}
	}
	variables, err := resolveOracleVariables(oracle.Variables, anchor, now())
	if err != nil {
		return OracleResult{}, err
	}
	query, queryVariables, err := resolveOracleQueryVariables(oracle.Query, variables)
	if err != nil {
		return OracleResult{}, err
	}
	data, err := postGraphQL(ctx, client, v.BaseURL, v.Headers, query, queryVariables)
	if err != nil {
		return OracleResult{}, err
	}
	result := OracleResult{Data: data, Variables: variables, Dimension: oracle.DimensionLiteral}
	if oracle.PickMax != nil {
		result.Value, result.Dimension, err = pickMaxRow(data, oracle.PickMax)
		return result, err
	}
	raw, ok := walkPath(data, oracle.Extract)
	if !ok {
		if oracle.AllowMissing {
			return result, nil
		}
		return OracleResult{}, fmt.Errorf("oracle extract %q not found in result", oracle.Extract)
	}
	result.Value = valueString(raw)
	if strings.TrimSpace(result.Value) == "" {
		if oracle.AllowMissing {
			return result, nil
		}
		return OracleResult{}, fmt.Errorf("oracle extract %q is empty", oracle.Extract)
	}
	for _, path := range oracle.DimensionAlternateExtracts {
		raw, ok := walkPath(data, path)
		if !ok {
			continue
		}
		if alt := strings.TrimSpace(valueString(raw)); alt != "" {
			result.DimensionAlternates = append(result.DimensionAlternates, alt)
		}
	}
	if oracle.DimensionExtract != "" {
		rawDimension, ok := walkPath(data, oracle.DimensionExtract)
		if !ok {
			return OracleResult{}, fmt.Errorf("oracle dimension extract %q not found in result", oracle.DimensionExtract)
		}
		result.Dimension = valueString(rawDimension)
		if strings.TrimSpace(result.Dimension) == "" {
			return OracleResult{}, fmt.Errorf("oracle dimension extract %q is empty", oracle.DimensionExtract)
		}
	}
	return result, nil
}

func oracleVariableMarker(name string) string {
	return "__GRAPHJIN_EVAL_" + strings.ToUpper(strings.TrimSpace(name)) + "__"
}

func resolveOracleQueryVariables(query string, variables map[string]any) (string, map[string]any, error) {
	remaining := make(map[string]any, len(variables))
	for name, value := range variables {
		remaining[name] = value
		markerJSON, err := json.Marshal(oracleVariableMarker(name))
		if err != nil {
			return "", nil, fmt.Errorf("encode oracle marker %q: %w", name, err)
		}
		if !strings.Contains(query, string(markerJSON)) {
			continue
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return "", nil, fmt.Errorf("encode oracle variable %q: %w", name, err)
		}
		query = strings.ReplaceAll(query, string(markerJSON), string(valueJSON))
		delete(remaining, name)
	}
	return query, remaining, nil
}

func postGraphQL(ctx context.Context, client HTTPDoer, baseURL string, headers map[string]string, query string, variables map[string]any) (any, error) {
	payload := map[string]any{"query": query}
	if len(variables) != 0 {
		payload["variables"] = variables
	}
	raw, status, _, err := postJSON(ctx, client, graphqlURL(baseURL), headers, payload)
	if err != nil {
		return nil, err
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode HTTP %d oracle response: %w", status, err)
	}
	if errs := toSlice(envelope["errors"]); len(errs) != 0 {
		return nil, fmt.Errorf("oracle errors: %s", compactJSON(envelope["errors"], 1024))
	}
	if data, ok := envelope["data"]; ok && data != nil {
		return data, nil
	}
	return envelope, nil
}

func postAgent(ctx context.Context, client HTTPDoer, baseURL string, headers map[string]string, prompt string, history []TurnSpec) (gjagent.Response, int, int64, error) {
	trace := true
	turns := make([]gjagent.Turn, 0, len(history))
	for _, turn := range history {
		turns = append(turns, gjagent.Turn{Role: turn.Role, Content: turn.Content})
	}
	payload := gjagent.Request{Instruction: prompt, History: turns, ReturnTrace: &trace}
	raw, status, latency, err := postJSON(ctx, client, agentURL(baseURL), headers, payload)
	if err != nil {
		// The server intentionally preserves its historical HTTP status while
		// returning stable provider codes in the JSON body. Decode that body so
		// Eval classifies the structured code instead of flattening every HTTP
		// 500 into a generic transport/server failure.
		var response gjagent.Response
		if len(raw) != 0 && json.Unmarshal(raw, &response) == nil && len(response.Errors) != 0 {
			return response, status, latency, nil
		}
		return gjagent.Response{}, status, latency, err
	}
	var response gjagent.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return gjagent.Response{}, status, latency, fmt.Errorf("decode HTTP %d agent response: %w", status, err)
	}
	return response, status, latency, nil
}

func postJSON(ctx context.Context, client HTTPDoer, url string, headers map[string]string, payload any) ([]byte, int, int64, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	started := time.Now()
	response, err := client.Do(request)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return nil, 0, latency, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, response.StatusCode, latency, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return raw, response.StatusCode, latency, fmt.Errorf("HTTP %d: %s", response.StatusCode, truncateText(string(raw), 500))
	}
	return raw, response.StatusCode, latency, nil
}

func graphqlURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base + "/api/v1/graphql"
}

func agentURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base + "/api/v1/agent"
}

func agentStatusURL(base string) string { return agentURL(base) + "/status" }

var oracleTokenPattern = regexp.MustCompile(`\{\{(today|anchor)([+-]\d+)?d?\}\}`)

var anchorLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func resolveOracleVariables(variables map[string]any, anchor string, now time.Time) (map[string]any, error) {
	if len(variables) == 0 {
		return nil, nil
	}
	resolved := make(map[string]any, len(variables))
	for key, value := range variables {
		text, ok := value.(string)
		if !ok {
			resolved[key] = value
			continue
		}
		out, err := substituteOracleTokens(text, anchor, now)
		if err != nil {
			return nil, fmt.Errorf("variable %s: %w", key, err)
		}
		// A variable that is nothing but an anchor carries whatever the anchor
		// query selected. When that is a row's numeric identifier it has to stay
		// a number: inlined as a string it becomes {eq: "7"} against an integer
		// column, which is a different query and usually not a legal one.
		if strings.TrimSpace(text) == "{{anchor}}" {
			if number, convErr := strconv.ParseInt(strings.TrimSpace(out), 10, 64); convErr == nil {
				resolved[key] = number
				continue
			}
		}
		resolved[key] = out
	}
	return resolved, nil
}

func substituteOracleTokens(text, anchor string, now time.Time) (string, error) {
	var tokenErr error
	out := oracleTokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		parts := oracleTokenPattern.FindStringSubmatch(token)
		offsetDays := 0
		if parts[2] != "" {
			offsetDays, _ = strconv.Atoi(parts[2])
		}
		switch parts[1] {
		case "today":
			return now.UTC().AddDate(0, 0, offsetDays).Format("2006-01-02")
		case "anchor":
			if anchor == "" {
				tokenErr = fmt.Errorf("token %s used but no anchor_query configured", token)
				return token
			}
			shifted, err := shiftAnchor(anchor, offsetDays)
			if err != nil {
				tokenErr = err
				return token
			}
			return shifted
		}
		return token
	})
	return out, tokenErr
}

func shiftAnchor(anchor string, offsetDays int) (string, error) {
	for _, layout := range anchorLayouts {
		parsed, err := time.Parse(layout, anchor)
		if err == nil {
			return parsed.AddDate(0, 0, offsetDays).Format(layout), nil
		}
	}
	// An unshifted anchor need not be a date. Tasks that pin a row rather than a
	// window resolve the row's identifier and substitute it as-is; only shifting
	// one by days requires a timestamp to shift.
	if offsetDays == 0 {
		return anchor, nil
	}
	return "", fmt.Errorf("anchor %q matches no supported timestamp layout", anchor)
}

func pickMaxRow(data any, rule *PickMaxRule) (string, string, error) {
	rawList, ok := walkPath(data, rule.List)
	if !ok {
		return "", "", fmt.Errorf("pick_max list %q not found", rule.List)
	}
	rows := toSlice(rawList)
	if len(rows) == 0 {
		return "", "", fmt.Errorf("pick_max list %q is empty", rule.List)
	}
	best := math.Inf(-1)
	var bestRow map[string]any
	for _, item := range rows {
		row := toMap(item)
		number, ok := numberFromString(valueString(row[rule.Value]))
		if !ok {
			continue
		}
		if number > best {
			best = number
			bestRow = row
		}
	}
	if bestRow == nil {
		return "", "", fmt.Errorf("pick_max found no numeric %q values in %q", rule.Value, rule.List)
	}
	return valueString(bestRow[rule.Value]), valueString(bestRow[rule.Dimension]), nil
}

func walkPath(value any, path string) (any, bool) {
	if strings.TrimSpace(path) == "" {
		return value, true
	}
	current := normalizeJSON(value)
	for _, part := range strings.Split(path, ".") {
		if index, err := strconv.Atoi(part); err == nil {
			items := toSlice(current)
			if index < 0 || index >= len(items) {
				return nil, false
			}
			current = items[index]
			continue
		}
		mapped := toMap(current)
		if mapped == nil {
			return nil, false
		}
		var ok bool
		current, ok = mapped[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func normalizeJSON(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if json.Unmarshal(data, &normalized) != nil {
		return value
	}
	return normalized
}

func toMap(value any) map[string]any {
	if direct, ok := value.(map[string]any); ok {
		return direct
	}
	mapped, _ := normalizeJSON(value).(map[string]any)
	return mapped
}

func toSlice(value any) []any {
	if direct, ok := value.([]any); ok {
		return direct
	}
	items, _ := normalizeJSON(value).([]any)
	return items
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		data, _ := json.Marshal(typed)
		return strings.Trim(string(data), `"`)
	}
}

func numberFromString(value string) (float64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, ",", ""), "$", ""))
	if value == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}

func compactJSON(value any, limit int) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	if limit > 0 && len(data) > limit {
		data = data[:limit]
	}
	return string(data)
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
