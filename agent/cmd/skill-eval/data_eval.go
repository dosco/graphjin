// Data-accuracy ("ground truth") evaluation mode.
//
// Each case asks the agent a natural-language data question and scores the
// run on three independent dimensions:
//
//  1. Ground truth: the answer must match a runtime oracle — a trusted
//     GraphQL query (DB-side aggregates only) executed against the same
//     server's /api/v1/graphql, so date-relative demo seeds never desync.
//  2. Method: the agent must have made the database compute — the executed
//     queries (read from the response action trail) must match the case's
//     shape expectations. This catches "right number, wrong method" runs
//     that sum a row page client-side on a table small enough to get away
//     with it.
//  3. Efficiency: advisory budget on actor turns and tokens; exceeding it
//     warns and feeds the runaway failure bucket, never a hard gate.
//
// Failed runs are classified into failure buckets so a report reads as a
// diagnosis (what class of mistake) rather than a bare recall number.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

type dataEvalCase struct {
	ID                string            `json:"id"`
	Group             string            `json:"group"`
	Prompt            string            `json:"prompt"`
	CapabilityProfile capabilityProfile `json:"capability_profile"`
	ExpectedStatus    string            `json:"expected_status"`
	Oracle            dataOracle        `json:"oracle"`
	Answer            answerRule        `json:"answer"`
	Method            methodRule        `json:"method"`
	Budget            *caseBudget       `json:"budget,omitempty"`
	SanityHint        *float64          `json:"sanity_hint,omitempty"`
}

type dataOracle struct {
	Query            string         `json:"query"`
	Variables        map[string]any `json:"variables,omitempty"`
	Extract          string         `json:"extract,omitempty"`
	DimensionExtract string         `json:"dimension_extract,omitempty"`
	// PickMax selects the row with the largest numeric value from a grouped
	// result in the runner. Alternative to order_by-on-the-aggregate oracles:
	// it ranks outside the engine, so it stays trustworthy even if the
	// grouped-order-by compile path regresses.
	PickMax *pickMaxRule `json:"pick_max,omitempty"`
	// AnchorQuery resolves a live data anchor (e.g. max_<date_col>) whose
	// value substitutes {{anchor}} / {{anchor±Nd}} tokens in Variables.
	AnchorQuery   string `json:"anchor_query,omitempty"`
	AnchorExtract string `json:"anchor_extract,omitempty"`
}

type pickMaxRule struct {
	List      string `json:"list"`      // dotted path to the grouped row list
	Value     string `json:"value"`     // numeric field ranked on
	Dimension string `json:"dimension"` // field reported as the winning dimension
}

type answerRule struct {
	Kind             string    `json:"kind"` // number | string | date
	ExtractRegex     string    `json:"extract_regex,omitempty"`
	FromData         string    `json:"from_data,omitempty"`
	TolerancePct     float64   `json:"tolerance_pct,omitempty"`
	AcceptScales     []float64 `json:"accept_scales,omitempty"` // scales applied to the oracle value
	ForbiddenPhrases []string  `json:"forbidden_phrases,omitempty"`
}

type methodRule struct {
	RequireQueryMatch          []string `json:"require_query_match,omitempty"`
	ForbidFinalizeFromListOnly bool     `json:"forbid_finalize_from_list_only,omitempty"`
	RequireTools               []string `json:"require_tools,omitempty"`
	ForbidTools                []string `json:"forbid_tools,omitempty"`
}

type caseBudget struct {
	MaxActorTurns  int64 `json:"max_actor_turns,omitempty"`
	MaxTotalTokens int64 `json:"max_total_tokens,omitempty"`
}

type dataCaseVerdict struct {
	CaseID          string   `json:"case_id"`
	Group           string   `json:"group"`
	GroundTruthPass bool     `json:"ground_truth_pass"`
	MethodPass      bool     `json:"method_pass"`
	Consistency     float64  `json:"consistency"` // fraction of repeats passing ground truth
	FailureBucket   string   `json:"failure_bucket,omitempty"`
	OracleValue     string   `json:"oracle_value,omitempty"`
	OracleDimension string   `json:"oracle_dimension,omitempty"`
	EvidenceQueries []string `json:"evidence_queries,omitempty"`
	GroundTruthRuns int      `json:"ground_truth_runs"`
	OracleErrorRuns int      `json:"oracle_error_runs,omitempty"`
}

var defaultForbiddenPhrases = []string{
	"cannot determine",
	"unable to retrieve",
	"unable to determine",
	"not found in the database",
	"not a column",
	"missing column",
	"schema change",
	"schema does not",
}

// aggregateFieldPattern recognizes DB-side aggregate fields in authored queries.
var aggregateFieldPattern = regexp.MustCompile(`\b(count|sum|avg|min|max|stddev|variance)_[a-zA-Z0-9_]+`)

// defaultNumberPattern extracts numeric answer candidates from prose.
var defaultNumberPattern = regexp.MustCompile(`-?\$?\d[\d,]*(?:\.\d+)?`)

var oracleTokenPattern = regexp.MustCompile(`\{\{(today|anchor)([+-]\d+)?d?\}\}`)

var anchorLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func validateDataCases(cases []dataEvalCase) error {
	seen := map[string]bool{}
	for _, c := range cases {
		if c.ID == "" || c.Prompt == "" || c.ExpectedStatus == "" {
			return fmt.Errorf("data case %q missing id, prompt, or expected_status", c.ID)
		}
		if seen[c.ID] {
			return fmt.Errorf("duplicate data case id %q", c.ID)
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Oracle.Query) == "" {
			return fmt.Errorf("data case %q needs oracle.query", c.ID)
		}
		if strings.TrimSpace(c.Oracle.Extract) == "" && c.Oracle.PickMax == nil {
			return fmt.Errorf("data case %q needs oracle.extract or oracle.pick_max", c.ID)
		}
	}
	return nil
}

type dataTask struct {
	testCase dataEvalCase
	repeat   int
	profile  endpointProfile
}

func makeDataTasks(cases []dataEvalCase, profiles []endpointProfile, repeats int) ([]dataTask, error) {
	tasks := make([]dataTask, 0, len(cases)*repeats)
	for _, testCase := range cases {
		profile, ok := findProfile(profiles, testCase.CapabilityProfile)
		if !ok {
			return nil, fmt.Errorf("data case %s has no exact endpoint mapping for capability profile %s", testCase.ID, profileKey(testCase.CapabilityProfile))
		}
		for repeat := 1; repeat <= repeats; repeat++ {
			tasks = append(tasks, dataTask{testCase: testCase, repeat: repeat, profile: profile})
		}
	}
	return tasks, nil
}

// oracleURL derives the plain GraphQL endpoint from the agent endpoint.
func oracleURL(agentURL string) string {
	if strings.Contains(agentURL, "/api/v1/agent") {
		return strings.Replace(agentURL, "/api/v1/agent", "/api/v1/graphql", 1)
	}
	return strings.TrimSuffix(agentURL, "/") + "/api/v1/graphql"
}

// resolveOracle executes the (optional) anchor query, substitutes variable
// tokens, executes the oracle query, and extracts the expected value and
// dimension. The anchor's own timestamp layout is preserved when shifting so
// generated bounds compare correctly against stored values.
func resolveOracle(client *http.Client, profile endpointProfile, oracle dataOracle, now time.Time) (value string, dimension string, err error) {
	anchor := ""
	if strings.TrimSpace(oracle.AnchorQuery) != "" {
		anchorData, err := postGraphQL(client, profile, oracle.AnchorQuery, nil)
		if err != nil {
			return "", "", fmt.Errorf("anchor query: %w", err)
		}
		raw, ok := walkPath(anchorData, oracle.AnchorExtract)
		if !ok {
			return "", "", fmt.Errorf("anchor extract %q not found", oracle.AnchorExtract)
		}
		anchor = valueString(raw)
		if anchor == "" {
			return "", "", fmt.Errorf("anchor extract %q is empty", oracle.AnchorExtract)
		}
	}
	variables, err := resolveOracleVariables(oracle.Variables, anchor, now)
	if err != nil {
		return "", "", err
	}
	data, err := postGraphQL(client, profile, oracle.Query, variables)
	if err != nil {
		return "", "", err
	}
	if oracle.PickMax != nil {
		return pickMaxRow(data, oracle.PickMax)
	}
	raw, ok := walkPath(data, oracle.Extract)
	if !ok {
		return "", "", fmt.Errorf("oracle extract %q not found in result", oracle.Extract)
	}
	value = valueString(raw)
	if oracle.DimensionExtract != "" {
		rawDim, ok := walkPath(data, oracle.DimensionExtract)
		if !ok {
			return "", "", fmt.Errorf("oracle dimension extract %q not found", oracle.DimensionExtract)
		}
		dimension = valueString(rawDim)
	}
	return value, dimension, nil
}

// pickMaxRow resolves a grouped-ranking oracle: from the complete grouped
// row list, return the value and dimension of the row with the largest
// numeric value.
func pickMaxRow(data any, rule *pickMaxRule) (string, string, error) {
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

// shiftAnchor moves an anchor timestamp by whole days while preserving the
// exact layout it arrived in, so lexicographic comparisons against stored
// values stay valid (e.g. "T" vs " " separators).
func shiftAnchor(anchor string, offsetDays int) (string, error) {
	for _, layout := range anchorLayouts {
		parsed, err := time.Parse(layout, anchor)
		if err == nil {
			return parsed.AddDate(0, 0, offsetDays).Format(layout), nil
		}
	}
	return "", fmt.Errorf("anchor %q matches no supported timestamp layout", anchor)
}

// postGraphQL executes a trusted oracle query and returns the data object.
func postGraphQL(client *http.Client, profile endpointProfile, query string, variables map[string]any) (any, error) {
	payload := map[string]any{"query": query}
	if len(variables) != 0 {
		payload["variables"] = variables
	}
	raw, status, err := postJSON(client, oracleURL(profile.URL), profile.Headers, payload)
	if err != nil {
		return nil, err
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode HTTP %d oracle response: %w", status, err)
	}
	if errs := toSlice(envelope["errors"]); len(errs) != 0 {
		return nil, fmt.Errorf("oracle errors: %s", compactJSON(envelope["errors"]))
	}
	if data, ok := envelope["data"]; ok && data != nil {
		return data, nil
	}
	return envelope, nil
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	if len(data) > 512 {
		data = data[:512]
	}
	return string(data)
}

// runDataOne runs one data case repeat: oracle first, then the agent, then
// the three scoring dimensions.
func runDataOne(client *http.Client, item dataTask, order int) runResult {
	testCase := item.testCase
	result := runResult{
		CaseID:         testCase.ID,
		Group:          testCase.Group,
		Repeat:         item.repeat,
		Order:          order,
		Profile:        item.profile.Name,
		Prompt:         testCase.Prompt,
		ExpectedStatus: testCase.ExpectedStatus,
	}

	oracleValue, oracleDimension, oracleErr := resolveOracle(client, item.profile, testCase.Oracle, time.Now())
	if oracleErr != nil {
		result.OracleError = oracleErr.Error()
	} else {
		result.OracleValue = oracleValue
		result.OracleDimension = oracleDimension
		if testCase.SanityHint != nil {
			if oracleNumber, ok := numberFromString(oracleValue); !ok || relativeDelta(oracleNumber, *testCase.SanityHint) > 0.001 {
				result.OracleWarning = fmt.Sprintf("oracle value %s disagrees with sanity hint %v; seed or oracle drift", oracleValue, *testCase.SanityHint)
			}
		}
	}

	agentResponse, httpStatus, latencyMS, err := postAgent(client, item.profile, testCase.Prompt)
	result.HTTPStatus = httpStatus
	result.LatencyMS = latencyMS
	if err != nil {
		result.Error = err.Error()
		result.FailureBucket = "transport_error"
		return result
	}
	result.Status = agentResponse.Status
	result.AnswerExcerpt = truncateText(agentResponse.Answer, 400)
	result.Tokens = extractTokenUsage(agentResponse.Usage)
	result.ActorTurns, result.ActorTurnsSource = actorTurns(agentResponse.Trace, result.Tokens.LLMCalls)
	result.GraphJinToolCalls, result.ActionOutcomes = actionInventory(agentResponse.Actions)

	queries, okTools := executedQueries(agentResponse.Actions)
	result.ExecutedQueries = queries

	if testCase.Budget != nil {
		if (testCase.Budget.MaxActorTurns > 0 && result.ActorTurns > testCase.Budget.MaxActorTurns) ||
			(testCase.Budget.MaxTotalTokens > 0 && result.Tokens.Total > testCase.Budget.MaxTotalTokens) {
			result.BudgetExceeded = true
		}
	}

	methodPass := evaluateMethod(testCase.Method, testCase.Answer, queries, okTools, agentResponse.Answer)
	result.MethodPass = &methodPass

	if result.OracleError == "" {
		pass, detail := evaluateGroundTruth(testCase, oracleValue, oracleDimension, agentResponse)
		result.GroundTruthPass = &pass
		result.GroundTruthDetail = detail
	}

	result.FailureBucket = classifyFailure(testCase, result, agentResponse)
	return result
}

// postAgent posts one instruction to the agent endpoint.
func postAgent(client *http.Client, profile endpointProfile, prompt string) (*gjagent.Response, int, int64, error) {
	body, _ := json.Marshal(gjagent.Request{Instruction: prompt, ReturnTrace: boolPointer(true)})
	raw, status, latency, err := postJSONTimed(client, profile.URL, profile.Headers, body)
	if err != nil {
		return nil, status, latency, err
	}
	var response gjagent.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, status, latency, fmt.Errorf("decode HTTP %d response: %w", status, err)
	}
	return &response, status, latency, nil
}

// postJSONTimed posts a JSON body and returns the raw response, HTTP status,
// and latency.
func postJSONTimed(client *http.Client, url string, headers map[string]string, body []byte) ([]byte, int, int64, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
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
	return raw, response.StatusCode, latency, nil
}

func postJSON(client *http.Client, url string, headers map[string]string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	raw, status, _, err := postJSONTimed(client, url, headers, body)
	if err != nil {
		return nil, status, err
	}
	if status < 200 || status >= 300 {
		return nil, status, fmt.Errorf("HTTP %d: %s", status, truncateText(string(raw), 300))
	}
	return raw, status, nil
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

// evaluateGroundTruth checks the answer (and optionally response data)
// against the oracle value and dimension.
func evaluateGroundTruth(testCase dataEvalCase, oracleValue, oracleDimension string, response *gjagent.Response) (bool, string) {
	if response.Status != testCase.ExpectedStatus {
		detail := fmt.Sprintf("status %q, expected %q", response.Status, testCase.ExpectedStatus)
		if len(response.Errors) != 0 {
			detail += ": " + truncateText(response.Errors[0].Message, 300)
		}
		return false, detail
	}
	answer := response.Answer
	lowered := strings.ToLower(answer)
	for _, phrase := range append(append([]string(nil), defaultForbiddenPhrases...), testCase.Answer.ForbiddenPhrases...) {
		if phrase != "" && strings.Contains(lowered, strings.ToLower(phrase)) {
			return false, fmt.Sprintf("forbidden phrase %q in answer", phrase)
		}
	}
	dataText := compactJSONFull(response.Data)
	if oracleDimension != "" {
		haystack := strings.ToLower(answer + "\n" + dataText)
		if !strings.Contains(haystack, strings.ToLower(oracleDimension)) {
			return false, fmt.Sprintf("dimension %q missing from answer", oracleDimension)
		}
	}
	switch testCase.Answer.Kind {
	case "number", "":
		oracleNumber, ok := numberFromString(oracleValue)
		if !ok {
			return false, fmt.Sprintf("oracle value %q is not numeric", oracleValue)
		}
		candidates := answerNumberCandidates(testCase.Answer, answer, response.Data)
		if len(candidates) == 0 {
			return false, "no numeric candidates in answer"
		}
		scales := testCase.Answer.AcceptScales
		if len(scales) == 0 {
			scales = []float64{1}
		}
		for _, candidate := range candidates {
			if matchWithScales(candidate, oracleNumber, scales, testCase.Answer.TolerancePct) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("no candidate within tolerance of %s (candidates %v)", oracleValue, boundedCandidates(candidates))
	case "date":
		normalized := dateFromValue(oracleValue)
		if normalized == "" {
			return false, fmt.Sprintf("oracle value %q is not a date", oracleValue)
		}
		haystack := strings.ToLower(answer + "\n" + dataText)
		for _, variant := range dateVariants(normalized) {
			if strings.Contains(haystack, strings.ToLower(variant)) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("date %s missing from answer", normalized)
	case "string":
		if strings.Contains(strings.ToLower(answer+"\n"+dataText), strings.ToLower(oracleValue)) {
			return true, ""
		}
		return false, fmt.Sprintf("value %q missing from answer", oracleValue)
	default:
		return false, fmt.Sprintf("unknown answer kind %q", testCase.Answer.Kind)
	}
}

func answerNumberCandidates(rule answerRule, answer string, data any) []float64 {
	pattern := defaultNumberPattern
	if rule.ExtractRegex != "" {
		if custom, err := regexp.Compile(rule.ExtractRegex); err == nil {
			pattern = custom
		}
	}
	var candidates []float64
	for _, match := range pattern.FindAllString(answer, 64) {
		if number, ok := numberFromString(match); ok {
			candidates = append(candidates, number)
		}
	}
	if rule.FromData != "" {
		if raw, ok := walkPath(data, rule.FromData); ok {
			if number, ok := numberFromString(valueString(raw)); ok {
				candidates = append(candidates, number)
			}
		}
	}
	return candidates
}

func boundedCandidates(candidates []float64) []float64 {
	if len(candidates) > 8 {
		return candidates[:8]
	}
	return candidates
}

// matchWithScales reports whether candidate matches oracle at any accepted
// scale. Scales apply to the oracle value: an oracle of 1980000 cents with
// scales [1, 0.01] accepts 1980000 and 19800.
func matchWithScales(candidate, oracle float64, scales []float64, tolerancePct float64) bool {
	tolerance := tolerancePct
	if tolerance <= 0 {
		tolerance = 1e-9
	}
	for _, scale := range scales {
		expected := oracle * scale
		limit := tolerance * math.Max(1, math.Abs(expected))
		if math.Abs(candidate-expected) <= limit {
			return true
		}
	}
	return false
}

func relativeDelta(a, b float64) float64 {
	return math.Abs(a-b) / math.Max(1, math.Abs(b))
}

// evaluateMethod scores whether the database did the computing.
func evaluateMethod(rule methodRule, answer answerRule, queries, okTools []string, answerText string) bool {
	joined := strings.ToLower(strings.Join(queries, "\n"))
	for _, required := range rule.RequireQueryMatch {
		pattern, err := regexp.Compile("(?i)" + required)
		if err != nil {
			return false
		}
		if !pattern.MatchString(joined) {
			return false
		}
	}
	for _, tool := range rule.RequireTools {
		if !contains(okTools, tool) {
			return false
		}
	}
	for _, tool := range rule.ForbidTools {
		if contains(okTools, tool) {
			return false
		}
	}
	if rule.ForbidFinalizeFromListOnly {
		numericAnswer := answer.Kind == "number" || answer.Kind == ""
		if numericAnswer && !aggregateFieldPattern.MatchString(joined) {
			return false
		}
	}
	return true
}

// executedQueries collects successful execution query texts and tool names
// from the response action trail.
func executedQueries(actions any) ([]string, []string) {
	items := toSlice(actions)
	var queries []string
	var okTools []string
	for _, item := range items {
		action := toMap(item)
		tool := stringValue(action["tool"])
		status := stringValue(action["status"])
		if status != "" && status != "ok" {
			continue
		}
		if tool != "" {
			okTools = appendUnique(okTools, tool)
		}
		if tool != "execute_graphql" && tool != "execute_saved_query" {
			continue
		}
		args := toMap(action["args"])
		if query := stringValue(args["query"]); query != "" {
			queries = append(queries, query)
		}
	}
	return queries, okTools
}

// truncatedActionPresent reports whether any action summary carries the
// truncation marker (populated once execution results surface truncation).
func truncatedActionPresent(actions any) bool {
	for _, item := range toSlice(actions) {
		action := toMap(item)
		summary := toMap(action["summary"])
		if summary == nil {
			continue
		}
		if _, ok := summary["truncated"]; ok {
			return true
		}
	}
	return false
}

// classifyFailure buckets a failed run for the diagnosis report. Empty means
// the run passed both ground truth and method.
func classifyFailure(testCase dataEvalCase, result runResult, response *gjagent.Response) string {
	groundTruthPass := result.GroundTruthPass != nil && *result.GroundTruthPass
	methodPass := result.MethodPass != nil && *result.MethodPass
	if groundTruthPass && methodPass {
		return ""
	}
	switch {
	case result.OracleError != "":
		return "oracle_error"
	case result.Status != testCase.ExpectedStatus:
		return "refused_or_blocked"
	case result.BudgetExceeded:
		return "runaway"
	case !groundTruthPass && truncatedActionPresent(response.Actions):
		return "truncated_finalize"
	case !methodPass && (testCase.Answer.Kind == "number" || testCase.Answer.Kind == ""):
		if testCase.Group == "ranking" {
			return "ranking_method"
		}
		return "client_side_aggregation"
	case !groundTruthPass && testCase.Group == "anchor" && testCase.Answer.Kind == "date":
		return "stale_anchor"
	case !groundTruthPass && testCase.Group == "anchor":
		return "wrong_window"
	case !groundTruthPass:
		return "value_mismatch"
	default:
		return "method_only"
	}
}

// aggregateDataVerdicts folds run repeats into per-case majority verdicts.
func aggregateDataVerdicts(cases []dataEvalCase, results []runResult) []dataCaseVerdict {
	byCase := make(map[string][]runResult)
	for _, result := range results {
		byCase[result.CaseID] = append(byCase[result.CaseID], result)
	}
	verdicts := make([]dataCaseVerdict, 0, len(cases))
	for _, testCase := range cases {
		runs := byCase[testCase.ID]
		verdict := dataCaseVerdict{CaseID: testCase.ID, Group: testCase.Group}
		var groundTruthHits, methodHits, scored int
		buckets := map[string]int{}
		for _, run := range runs {
			if run.OracleError != "" {
				verdict.OracleErrorRuns++
				continue
			}
			scored++
			if run.GroundTruthPass != nil && *run.GroundTruthPass {
				groundTruthHits++
			} else if run.FailureBucket != "" {
				buckets[run.FailureBucket]++
			}
			if run.MethodPass != nil && *run.MethodPass {
				methodHits++
			}
			if verdict.OracleValue == "" {
				verdict.OracleValue = run.OracleValue
				verdict.OracleDimension = run.OracleDimension
			}
			if len(verdict.EvidenceQueries) == 0 && len(run.ExecutedQueries) != 0 {
				verdict.EvidenceQueries = run.ExecutedQueries
			}
		}
		verdict.GroundTruthRuns = scored
		if scored > 0 {
			verdict.GroundTruthPass = groundTruthHits*2 > scored
			verdict.MethodPass = methodHits*2 > scored
			verdict.Consistency = float64(groundTruthHits) / float64(scored)
		}
		if !verdict.GroundTruthPass || !verdict.MethodPass {
			verdict.FailureBucket = dominantBucket(buckets, verdict.GroundTruthPass, verdict.MethodPass)
		}
		verdicts = append(verdicts, verdict)
	}
	return verdicts
}

func dominantBucket(buckets map[string]int, groundTruthPass, methodPass bool) string {
	best, bestCount := "", 0
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if buckets[key] > bestCount {
			best, bestCount = key, buckets[key]
		}
	}
	if best == "" && groundTruthPass && !methodPass {
		return "method_only"
	}
	if best == "" {
		return "value_mismatch"
	}
	return best
}

type dataMetrics struct {
	CaseCount                int                `json:"case_count"`
	RunCount                 int                `json:"run_count"`
	GroundTruthRecall        float64            `json:"ground_truth_recall"`
	MethodRecall             float64            `json:"method_recall"`
	GroundTruthRecallByGroup map[string]float64 `json:"ground_truth_recall_by_group,omitempty"`
	FailureBuckets           map[string]int     `json:"failure_buckets,omitempty"`
	MeanConsistency          float64            `json:"mean_consistency"`
	OracleErrorRuns          int                `json:"oracle_error_runs,omitempty"`
}

func calculateDataMetrics(verdicts []dataCaseVerdict, results []runResult) dataMetrics {
	out := dataMetrics{CaseCount: len(verdicts), RunCount: len(results)}
	if len(verdicts) == 0 {
		return out
	}
	groupTotals := map[string]int{}
	groupHits := map[string]int{}
	buckets := map[string]int{}
	var groundTruthHits, methodHits int
	var consistencySum float64
	for _, verdict := range verdicts {
		groupTotals[verdict.Group]++
		if verdict.GroundTruthPass {
			groundTruthHits++
			groupHits[verdict.Group]++
		}
		if verdict.MethodPass {
			methodHits++
		}
		if verdict.FailureBucket != "" {
			buckets[verdict.FailureBucket]++
		}
		consistencySum += verdict.Consistency
		out.OracleErrorRuns += verdict.OracleErrorRuns
	}
	out.GroundTruthRecall = ratio(groundTruthHits, len(verdicts))
	out.MethodRecall = ratio(methodHits, len(verdicts))
	out.MeanConsistency = consistencySum / float64(len(verdicts))
	byGroup := map[string]float64{}
	for group, total := range groupTotals {
		byGroup[group] = ratio(groupHits[group], total)
	}
	out.GroundTruthRecallByGroup = byGroup
	if len(buckets) != 0 {
		out.FailureBuckets = buckets
	}
	return out
}

// applyDataAcceptance folds data gates into the acceptance verdict as a
// RATCHET: the hard gates are no-regression vs the baseline (ground truth
// and method — method is the leading indicator, it can improve before
// answers do). The 0.90 ground-truth target stays a warning until reached,
// so candidates that improve on a below-target baseline still land instead
// of the loop hard-failing throughout the climb.
func applyDataAcceptance(out *acceptance, candidate dataMetrics, baseline *dataMetrics) {
	groundTruthPass := true
	methodPass := true
	if baseline != nil && baseline.CaseCount > 0 {
		groundTruthPass = candidate.GroundTruthRecall >= baseline.GroundTruthRecall-1e-9
		methodPass = candidate.MethodRecall >= baseline.MethodRecall-1e-9
	}
	if candidate.GroundTruthRecall < 0.90 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"ground-truth recall %.2f is below the 0.90 target", candidate.GroundTruthRecall))
	}
	out.GroundTruthRecallPass = &groundTruthPass
	out.MethodRecallPass = &methodPass
	out.HardPass = out.HardPass && groundTruthPass && methodPass
	if candidate.OracleErrorRuns > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d oracle-error runs were excluded from scoring", candidate.OracleErrorRuns))
	}
}

func compactJSONFull(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < 1e15 {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return compactJSONFull(value)
	}
}

func numberFromString(text string) (float64, bool) {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "$")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSuffix(cleaned, "%")
	if cleaned == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

// walkPath resolves a dotted path ("accounts.0.sum_mrr_cents") through maps
// and slices.
func walkPath(value any, path string) (any, bool) {
	current := value
	if strings.TrimSpace(path) == "" {
		return nil, false
	}
	for _, segment := range strings.Split(path, ".") {
		switch typed := normalizeContainer(current).(type) {
		case map[string]any:
			next, ok := typed[segment]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func normalizeContainer(value any) any {
	switch value.(type) {
	case map[string]any, []any:
		return value
	default:
		return normalizeJSON(value)
	}
}

func dateFromValue(value string) string {
	trimmed := strings.TrimSpace(value)
	for _, layout := range anchorLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if len(trimmed) >= 10 {
		if parsed, err := time.Parse("2006-01-02", trimmed[:10]); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func dateVariants(isoDate string) []string {
	parsed, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return []string{isoDate}
	}
	return []string{
		isoDate,
		parsed.Format("January 2, 2006"),
		parsed.Format("Jan 2, 2006"),
		parsed.Format("2 January 2006"),
	}
}

// writeLedgerCopy appends the report to the ledger directory so recall can be
// tracked across iterations with -trend.
func writeLedgerCopy(ledgerDir string, out report, data []byte) error {
	if err := os.MkdirAll(ledgerDir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	model := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		default:
			return '_'
		}
	}, out.Model)
	name := fmt.Sprintf("%s-%s-%s.json", stamp, out.Phase, model)
	return os.WriteFile(filepath.Join(ledgerDir, name), data, 0o644)
}

// runTrend prints ground-truth and method recall across ledger reports,
// oldest first, so improvement over iterations is visible at a glance.
func runTrend(ledgerDir string) error {
	entries, err := os.ReadDir(ledgerDir)
	if err != nil {
		return err
	}
	type trendRow struct {
		file   string
		loaded report
	}
	var rows []trendRow
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(ledgerDir, entry.Name()))
		if err != nil {
			continue
		}
		var loaded report
		if err := json.Unmarshal(raw, &loaded); err != nil {
			continue
		}
		rows = append(rows, trendRow{file: entry.Name(), loaded: loaded})
	}
	if len(rows) == 0 {
		return fmt.Errorf("no reports found in %s", ledgerDir)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].loaded.GeneratedAt < rows[j].loaded.GeneratedAt })
	fmt.Printf("%-22s %-10s %-24s %-9s %-9s %-9s %s\n",
		"GENERATED", "PHASE", "MODEL", "COMMIT", "GT", "METHOD", "BY GROUP")
	for _, row := range rows {
		commit := row.loaded.GraphJinCommit
		if len(commit) > 8 {
			commit = commit[:8]
		}
		groundTruth, method, byGroup := "-", "-", ""
		if dm := row.loaded.DataMetrics; dm != nil && dm.CaseCount > 0 {
			groundTruth = fmt.Sprintf("%.2f", dm.GroundTruthRecall)
			method = fmt.Sprintf("%.2f", dm.MethodRecall)
			groups := make([]string, 0, len(dm.GroundTruthRecallByGroup))
			for group := range dm.GroundTruthRecallByGroup {
				groups = append(groups, group)
			}
			sort.Strings(groups)
			parts := make([]string, 0, len(groups))
			for _, group := range groups {
				parts = append(parts, fmt.Sprintf("%s=%.2f", group, dm.GroundTruthRecallByGroup[group]))
			}
			byGroup = strings.Join(parts, " ")
		}
		fmt.Printf("%-22s %-10s %-24s %-9s %-9s %-9s %s\n",
			row.loaded.GeneratedAt, row.loaded.Phase, row.loaded.Model, commit, groundTruth, method, byGroup)
	}
	return nil
}
