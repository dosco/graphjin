package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// A metric watch is a schedule watch that also runs a query on every tick and
// fires when a number in the result crosses a threshold. The database computes
// the aggregate; the watch only compares the value with the previous tick, so
// it works for count_*, sum_*, avg_* and group-by results that have no cursor
// and cannot be a row-stream subscription.

const (
	watchMetricHistoryMax = 48
	watchMetricGroupMax   = 100
)

var watchMetricOps = map[string]bool{
	"gt": true, "gte": true, "lt": true, "lte": true, "eq": true, "ne": true,
	"changed": true, "change_pct": true,
}

// watchMetric is the stored condition. Path addresses a scalar
// ("orders.0.count_id"), or, with Key and Value, a list of grouped rows
// ("orders" + key "region" + value "avg_ship_hours").
type watchMetric struct {
	Path      string `json:"path"`
	Key       string `json:"key,omitempty"`
	Value     string `json:"value,omitempty"`
	Op        string `json:"op"`
	Threshold any    `json:"threshold,omitempty"`
	Fire      string `json:"fire,omitempty"`
}

func (m watchMetric) grouped() bool { return m.Key != "" }

func (m watchMetric) edgeTriggered() bool { return m.Fire != "every_tick" }

type watchMetricState struct {
	LastValue     any                              `json:"last_value,omitempty"`
	HasValue      bool                             `json:"has_value,omitempty"`
	Breached      bool                             `json:"breached"`
	Groups        map[string]watchMetricGroupState `json:"groups,omitempty"`
	LastCheckedAt string                           `json:"last_checked_at,omitempty"`
	LastError     string                           `json:"last_error,omitempty"`
	History       []watchMetricPoint               `json:"history,omitempty"`
}

type watchMetricGroupState struct {
	Value    any  `json:"value"`
	Breached bool `json:"breached"`
}

type watchMetricPoint struct {
	At    string  `json:"at"`
	Value float64 `json:"value"`
}

// watchMetricCrossing is one value that fired in a tick: the scalar, or a
// group of a grouped metric.
type watchMetricCrossing struct {
	Key      string `json:"key,omitempty"`
	Value    any    `json:"value"`
	Previous any    `json:"previous,omitempty"`
}

func parseWatchMetricInput(raw any) (watchMetric, bool, error) {
	errShape := fmt.Errorf(`metric must be an object like {"path": "orders.0.count_id", "op": "gt", "threshold": 50}`)
	var value map[string]any
	switch v := raw.(type) {
	case nil:
		return watchMetric{}, false, nil
	case map[string]any:
		value = v
	case string:
		return parseWatchMetricInput(json.RawMessage(v))
	case json.RawMessage:
		text := strings.TrimSpace(string(v))
		if text == "" || text == "null" {
			return watchMetric{}, false, nil
		}
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return watchMetric{}, false, errShape
		}
	default:
		return watchMetric{}, false, errShape
	}
	if len(value) == 0 {
		return watchMetric{}, false, nil
	}
	for key := range value {
		switch key {
		case "path", "key", "value", "op", "threshold", "fire":
		default:
			return watchMetric{}, false, fmt.Errorf("metric has unknown field %q; supported fields are path, key, value, op, threshold and fire", key)
		}
	}
	str := func(key string) string {
		s, _ := value[key].(string)
		return strings.TrimSpace(s)
	}
	m := watchMetric{
		Path: str("path"), Key: str("key"), Value: str("value"),
		Op: strings.ToLower(str("op")), Threshold: value["threshold"], Fire: strings.ToLower(str("fire")),
	}
	if m.Path == "" {
		return watchMetric{}, false, fmt.Errorf(`metric.path is required: the dot path to the number in the query result, e.g. "orders.0.count_id"`)
	}
	if (m.Key == "") != (m.Value == "") {
		return watchMetric{}, false, fmt.Errorf("metric.key and metric.value go together: path names the list of grouped rows, key the group column, value the number column")
	}
	if !watchMetricOps[m.Op] {
		return watchMetric{}, false, fmt.Errorf("metric.op %q is not supported; use gt, gte, lt, lte, eq, ne, changed or change_pct", m.Op)
	}
	switch m.Fire {
	case "":
		m.Fire = "crossing"
	case "crossing", "every_tick":
	default:
		return watchMetric{}, false, fmt.Errorf(`metric.fire must be "crossing" (fire once when the condition starts holding) or "every_tick"`)
	}
	switch m.Op {
	case "changed":
		if m.Threshold != nil {
			return watchMetric{}, false, fmt.Errorf("metric.op changed takes no threshold")
		}
	case "eq", "ne":
		if m.Threshold == nil {
			return watchMetric{}, false, fmt.Errorf("metric.op %s needs a threshold to compare with", m.Op)
		}
	default:
		f, ok := watchMetricNumber(m.Threshold)
		if !ok {
			return watchMetric{}, false, fmt.Errorf("metric.op %s needs a numeric threshold", m.Op)
		}
		if m.Op == "change_pct" && f <= 0 {
			return watchMetric{}, false, fmt.Errorf("metric.op change_pct needs a positive threshold, the percent change that fires")
		}
		m.Threshold = f
	}
	return m, true, nil
}

func parseWatchMetricJSON(text string) (watchMetric, bool) {
	text = strings.TrimSpace(text)
	if text == "" || text == "null" {
		return watchMetric{}, false
	}
	var m watchMetric
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.Path == "" || m.Op == "" {
		return watchMetric{}, false
	}
	if m.Fire == "" {
		m.Fire = "crossing"
	}
	return m, true
}

func parseWatchMetricState(text string) watchMetricState {
	var st watchMetricState
	text = strings.TrimSpace(text)
	if text != "" && text != "null" {
		_ = json.Unmarshal([]byte(text), &st)
	}
	return st
}

func watchMetricNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		// Numeric and decimal columns often come back as JSON strings.
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

// watchMetricLookup follows a dot path through a decoded query result. A
// numeric segment indexes a list. The error names what was there instead, so a
// wrong path is a repairable mistake rather than a dead end.
func watchMetricLookup(data any, path string) (any, error) {
	cur := data
	walked := []string{}
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("metric.path %q: no %q at %s; available keys: %s",
					path, seg, watchMetricWhere(walked), strings.Join(sortedKeys(node), ", "))
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("metric.path %q: %s is a list; use an index such as %s.0", path, watchMetricWhere(walked), strings.Join(walked, "."))
			}
			if i < 0 || i >= len(node) {
				return nil, fmt.Errorf("metric.path %q: %s has %d rows, no index %d", path, watchMetricWhere(walked), len(node), i)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("metric.path %q: %s is a value, not an object or list", path, watchMetricWhere(walked))
		}
		walked = append(walked, seg)
	}
	return cur, nil
}

func watchMetricWhere(walked []string) string {
	if len(walked) == 0 {
		return "the result root"
	}
	return strings.Join(walked, ".")
}

// watchMetricObservation is one tick's reading: a scalar, or the groups in
// result order.
type watchMetricObservation struct {
	Value  any
	Groups []watchMetricGroupValue
}

type watchMetricGroupValue struct {
	Key   string
	Value any
}

func (m watchMetric) observe(data any) (watchMetricObservation, error) {
	node, err := watchMetricLookup(data, m.Path)
	if err != nil {
		return watchMetricObservation{}, err
	}
	numeric := m.Op != "changed" && m.Op != "eq" && m.Op != "ne"
	if !m.grouped() {
		if numeric {
			if _, ok := watchMetricNumber(node); !ok {
				return watchMetricObservation{}, fmt.Errorf("metric.path %q is %s, not a number", m.Path, watchMetricDescribe(node))
			}
		}
		return watchMetricObservation{Value: node}, nil
	}
	rows, ok := node.([]any)
	if !ok {
		return watchMetricObservation{}, fmt.Errorf("metric.path %q must name a list of grouped rows when key is set; it is %s", m.Path, watchMetricDescribe(node))
	}
	var obs watchMetricObservation
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		keyVal, ok := row[m.Key]
		if !ok {
			return watchMetricObservation{}, fmt.Errorf("metric.key %q is not in the rows at %q; available columns: %s", m.Key, m.Path, strings.Join(sortedKeys(row), ", "))
		}
		v, ok := row[m.Value]
		if !ok {
			return watchMetricObservation{}, fmt.Errorf("metric.value %q is not in the rows at %q; available columns: %s", m.Value, m.Path, strings.Join(sortedKeys(row), ", "))
		}
		if numeric {
			if _, ok := watchMetricNumber(v); !ok && v != nil {
				return watchMetricObservation{}, fmt.Errorf("metric.value %q is %s, not a number", m.Value, watchMetricDescribe(v))
			}
		}
		obs.Groups = append(obs.Groups, watchMetricGroupValue{Key: fmt.Sprint(keyVal), Value: v})
		if len(obs.Groups) == watchMetricGroupMax {
			break
		}
	}
	return obs, nil
}

func watchMetricDescribe(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "a list"
	case string:
		return fmt.Sprintf("the text %q", v)
	case bool:
		return "a boolean"
	}
	return fmt.Sprintf("%v", v)
}

// holds reports whether the condition is true for value given the previous
// reading. changed and change_pct never hold without a previous reading.
func (m watchMetric) holds(value, prev any, hasPrev bool) bool {
	switch m.Op {
	case "changed":
		return hasPrev && !watchMetricEqual(value, prev)
	case "change_pct":
		if !hasPrev {
			return false
		}
		v, ok1 := watchMetricNumber(value)
		p, ok2 := watchMetricNumber(prev)
		if !ok1 || !ok2 || p == 0 {
			return false
		}
		t, _ := watchMetricNumber(m.Threshold)
		return math.Abs((v-p)/math.Abs(p)*100) >= t
	case "eq":
		return watchMetricEqual(value, m.Threshold)
	case "ne":
		return !watchMetricEqual(value, m.Threshold)
	}
	v, ok := watchMetricNumber(value)
	if !ok {
		return false
	}
	t, _ := watchMetricNumber(m.Threshold)
	switch m.Op {
	case "gt":
		return v > t
	case "gte":
		return v >= t
	case "lt":
		return v < t
	case "lte":
		return v <= t
	}
	return false
}

func watchMetricEqual(a, b any) bool {
	if fa, ok := watchMetricNumber(a); ok {
		if fb, ok := watchMetricNumber(b); ok {
			return fa == fb
		}
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// evaluate folds one observation into the state and reports what fired. With
// fire "crossing" a threshold op fires when its condition starts holding and
// re-arms once it stops, so a breach that persists fires once, and a value
// that recovers and breaches again fires again. changed and change_pct compare
// consecutive readings, so they are edges already.
func (m watchMetric) evaluate(prev watchMetricState, obs watchMetricObservation, at time.Time) (watchMetricState, []watchMetricCrossing) {
	next := watchMetricState{LastCheckedAt: at.UTC().Format(time.RFC3339), History: prev.History}
	var crossings []watchMetricCrossing
	fires := func(holds, wasBreached bool) bool {
		if !holds {
			return false
		}
		if m.Op == "changed" || m.Op == "change_pct" || !m.edgeTriggered() {
			return true
		}
		return !wasBreached
	}
	if !m.grouped() {
		holds := m.holds(obs.Value, prev.LastValue, prev.HasValue)
		if fires(holds, prev.Breached) {
			c := watchMetricCrossing{Value: obs.Value}
			if prev.HasValue {
				c.Previous = prev.LastValue
			}
			crossings = append(crossings, c)
		}
		next.LastValue, next.HasValue, next.Breached = obs.Value, true, holds
		if f, ok := watchMetricNumber(obs.Value); ok {
			next.History = append(next.History, watchMetricPoint{At: next.LastCheckedAt, Value: f})
		}
	} else {
		next.Groups = make(map[string]watchMetricGroupState, len(obs.Groups))
		breaching := 0.0
		for _, g := range obs.Groups {
			before, had := prev.Groups[g.Key]
			holds := m.holds(g.Value, before.Value, had)
			if fires(holds, before.Breached) {
				c := watchMetricCrossing{Key: g.Key, Value: g.Value}
				if had {
					c.Previous = before.Value
				}
				crossings = append(crossings, c)
			}
			next.Groups[g.Key] = watchMetricGroupState{Value: g.Value, Breached: holds}
			if holds {
				breaching++
				next.Breached = true
			}
		}
		// A grouped metric's sparkline counts the groups in breach.
		next.History = append(next.History, watchMetricPoint{At: next.LastCheckedAt, Value: breaching})
	}
	if over := len(next.History) - watchMetricHistoryMax; over > 0 {
		next.History = append([]watchMetricPoint(nil), next.History[over:]...)
	}
	return next, crossings
}

// watchMetricTimeVar is a rolling-window variable resolved on every tick:
// {"$time": "now-24h"}, or the start of "today", "week", "month" or "year" in
// the schedule's time zone, each with an optional offset such as "today-7d".
var watchMetricTimeVar = regexp.MustCompile(`^(now|today|week|month|year)(?:([+-])(\d+)([mhdw]))?$`)

func resolveWatchMetricVariables(variablesJSON string, now time.Time, loc *time.Location) (json.RawMessage, error) {
	text := strings.TrimSpace(variablesJSON)
	if text == "" || text == "null" {
		return nil, nil
	}
	var vars map[string]any
	if err := json.Unmarshal([]byte(text), &vars); err != nil {
		return nil, fmt.Errorf("variables_json must be a JSON object: %w", err)
	}
	if loc == nil {
		loc = time.UTC
	}
	for name, v := range vars {
		obj, ok := v.(map[string]any)
		if !ok || len(obj) != 1 {
			continue
		}
		spec, ok := obj["$time"].(string)
		if !ok {
			continue
		}
		t, err := watchMetricTime(spec, now, loc)
		if err != nil {
			return nil, fmt.Errorf("variables_json.%s: %w", name, err)
		}
		vars[name] = t.UTC().Format(time.RFC3339)
	}
	out, err := json.Marshal(vars)
	return out, err
}

func watchMetricTime(spec string, now time.Time, loc *time.Location) (time.Time, error) {
	match := watchMetricTimeVar.FindStringSubmatch(strings.ToLower(strings.TrimSpace(spec)))
	if match == nil {
		return time.Time{}, fmt.Errorf(`$time %q is not supported; use now, today, week, month or year with an optional offset like "now-24h" or "today-7d"`, spec)
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	var t time.Time
	switch match[1] {
	case "now":
		t = now
	case "today":
		t = time.Date(y, mo, d, 0, 0, 0, 0, loc)
	case "week":
		// Weeks start on Monday.
		offset := (int(local.Weekday()) + 6) % 7
		t = time.Date(y, mo, d-offset, 0, 0, 0, 0, loc)
	case "month":
		t = time.Date(y, mo, 1, 0, 0, 0, 0, loc)
	case "year":
		t = time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	}
	if match[2] != "" {
		n, _ := strconv.Atoi(match[3])
		if match[2] == "-" {
			n = -n
		}
		switch match[4] {
		case "m":
			t = t.Add(time.Duration(n) * time.Minute)
		case "h":
			t = t.Add(time.Duration(n) * time.Hour)
		case "d":
			t = t.AddDate(0, 0, n)
		case "w":
			t = t.AddDate(0, 0, 7*n)
		}
	}
	return t, nil
}

// runWatchMetricQuery runs the metric query once as the watch owner, with its
// rolling variables resolved for now, and returns the decoded data.
func (s *graphjinService) runWatchMetricQuery(ctx context.Context, query, variablesJSON string, now time.Time, loc *time.Location) (any, error) {
	if s.gj == nil {
		return nil, fmt.Errorf("GraphJin core is not initialized")
	}
	vars, err := resolveWatchMetricVariables(variablesJSON, now, loc)
	if err != nil {
		return nil, err
	}
	res, err := s.gj.GraphQL(ctx, query, vars, nil)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("metric query returned no result")
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("metric query: %s", res.Errors[0].Message)
	}
	var data any
	if len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &data); err != nil {
			return nil, fmt.Errorf("decode metric query result: %w", err)
		}
	}
	return data, nil
}

// watchMetricStateProjection is the read-side view of a metric watch's state:
// the current value and whether it is in breach, plus the sparkline.
func watchMetricStateProjection(row map[string]any) any {
	if _, ok := parseWatchMetricJSON(jsonMapString(row, "metric_json")); !ok {
		return nil
	}
	return parseJSONValue(jsonMapString(row, "metric_state_json"))
}

const watchMetricHint = `aggregates and group-bys have no cursor, so to watch a number (a count, sum or average crossing a threshold) use schedule + metric with a plain query, e.g. schedule: {cron: "*/15 * * * *"}, query: "query { orders { count_id } }", metric: {path: "orders.0.count_id", op: "gt", threshold: 50}`

func watchMetricJSONText(m watchMetric, metered bool) string {
	if !metered {
		return ""
	}
	return mustMarshalString(m)
}

func watchMetricProjection(row map[string]any) any {
	m, ok := parseWatchMetricJSON(jsonMapString(row, "metric_json"))
	if !ok {
		return nil
	}
	return m
}

// probeWatchMetric runs a new metric watch's query once as its owner. A query
// or path mistake is reported at create time, and the reading seeds the state,
// so a value that already meets the condition fires only after it clears and
// crosses again; the caller sees the current value in metric_state.
func (h watchControlPlane) probeWatchMetric(ctx context.Context, query, variablesJSON string, m watchMetric, sched watchSchedule) (watchMetricState, error) {
	if strings.TrimSpace(query) == "" {
		return watchMetricState{}, fmt.Errorf("gj_watch metric needs query, the GraphQL query whose result holds the number")
	}
	header, err := core.Operation(query)
	if err != nil {
		return watchMetricState{}, err
	}
	if header.Type != core.OpQuery {
		return watchMetricState{}, fmt.Errorf("gj_watch metric query must be a plain query operation (\"query { ... }\"); the schedule re-runs it, so it needs no subscription or cursor")
	}
	if h.service == nil || h.service.gj == nil {
		return watchMetricState{}, fmt.Errorf("gj_watch metric validation requires initialized GraphJin core")
	}
	probeCtx, cancel := context.WithTimeout(ctx, watchValidationProbeTimeout)
	defer cancel()
	now := time.Now()
	data, err := h.service.runWatchMetricQuery(probeCtx, query, variablesJSON, now, sched.location())
	if err != nil {
		return watchMetricState{}, fmt.Errorf("gj_watch metric probe failed: %w", err)
	}
	obs, err := m.observe(data)
	if err != nil {
		return watchMetricState{}, fmt.Errorf("gj_watch %w", err)
	}
	state, _ := m.evaluate(watchMetricState{}, obs, now)
	return state, nil
}

func (sch watchSchedule) location() *time.Location {
	if sch.spec == nil || sch.spec.Location == nil {
		return time.UTC
	}
	return sch.spec.Location
}

// evaluateWatchMetricTick runs the metric query for one tick and folds the
// reading into the stored state. On a query or path error the previous state
// comes back unchanged apart from the error, and the schedule keeps running:
// a database that is briefly down must not silence the watch.
func (s *graphjinService) evaluateWatchMetricTick(
	ctx context.Context,
	row map[string]any,
	def watchRuntimeDefinition,
	m watchMetric,
	sched watchSchedule,
	now time.Time,
) (watchMetricState, []watchMetricCrossing, error) {
	prev := parseWatchMetricState(jsonMapString(row, "metric_state_json"))
	fail := func(err error) (watchMetricState, []watchMetricCrossing, error) {
		prev.LastCheckedAt = now.UTC().Format(time.RFC3339)
		prev.LastError = err.Error()
		return prev, nil, err
	}
	data, err := s.runWatchMetricQuery(ctx, def.Query, def.VariablesJSON, now, sched.location())
	if err != nil {
		return fail(err)
	}
	obs, err := m.observe(data)
	if err != nil {
		return fail(err)
	}
	state, crossings := m.evaluate(prev, obs, now)
	return state, crossings, nil
}

func (s *graphjinService) saveWatchMetricState(ctx context.Context, id string, state watchMetricState, evalErr error) error {
	lastError := ""
	if evalErr != nil {
		lastError = "metric: " + evalErr.Error()
	}
	_, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, "id", map[string]any{
		"id": id,
		"input": map[string]any{
			"metric_state_json": mustMarshalString(state),
			"last_error":        lastError,
			"updated_at":        time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	return err
}

// watchMetricEventPayload is what a fired metric tick tells delivery, routines
// and the inbox: the condition, the value that crossed and the one before it.
func watchMetricEventPayload(m watchMetric, state watchMetricState, crossings []watchMetricCrossing) map[string]any {
	payload := map[string]any{
		"kind":   "metric",
		"metric": m,
	}
	if m.grouped() {
		payload["crossings"] = crossings
		payload["groups_in_breach"] = len(crossings)
		return payload
	}
	payload["value"] = state.LastValue
	if len(crossings) > 0 && crossings[0].Previous != nil {
		payload["previous"] = crossings[0].Previous
	}
	return payload
}
