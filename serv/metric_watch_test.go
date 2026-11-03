package serv

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func mustWatchMetric(t *testing.T, raw map[string]any) watchMetric {
	t.Helper()
	m, ok, err := parseWatchMetricInput(raw)
	if err != nil || !ok {
		t.Fatalf("parseWatchMetricInput(%v) ok=%v err=%v", raw, ok, err)
	}
	return m
}

// runMetricSeries feeds scalar readings through evaluate and returns the
// indexes of the readings that fired.
func runMetricSeries(m watchMetric, values ...any) []int {
	var st watchMetricState
	var fired []int
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range values {
		var crossings []watchMetricCrossing
		st, crossings = m.evaluate(st, watchMetricObservation{Value: v}, at.Add(time.Duration(i)*time.Minute))
		if len(crossings) > 0 {
			fired = append(fired, i)
		}
	}
	return fired
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWatchMetricEvaluateOps(t *testing.T) {
	cases := []struct {
		name   string
		metric map[string]any
		values []any
		want   []int
	}{
		// Fires on the way over, stays quiet while over, re-arms below.
		{"gt crossing", map[string]any{"path": "a", "op": "gt", "threshold": 50}, []any{44.0, 57.0, 60.0, 40.0, 52.0}, []int{1, 4}},
		// 50 -> 51 -> 50 -> 51 fires twice: the old result-hash dedupe never would.
		{"gt returns to an old value", map[string]any{"path": "a", "op": "gt", "threshold": 50}, []any{50.0, 51.0, 50.0, 51.0}, []int{1, 3}},
		{"gt every tick", map[string]any{"path": "a", "op": "gt", "threshold": 50, "fire": "every_tick"}, []any{44.0, 57.0, 60.0, 40.0}, []int{1, 2}},
		{"gte", map[string]any{"path": "a", "op": "gte", "threshold": 10}, []any{9.0, 10.0}, []int{1}},
		{"lt", map[string]any{"path": "a", "op": "lt", "threshold": 5}, []any{8.0, 4.0, 3.0, 6.0, 1.0}, []int{1, 4}},
		{"lte", map[string]any{"path": "a", "op": "lte", "threshold": 0}, []any{1.0, 0.0}, []int{1}},
		{"eq string", map[string]any{"path": "a", "op": "eq", "threshold": "down"}, []any{"up", "down", "down", "up", "down"}, []int{1, 4}},
		{"ne", map[string]any{"path": "a", "op": "ne", "threshold": 0}, []any{0.0, 3.0}, []int{1}},
		// The first reading only seeds the previous value.
		{"changed", map[string]any{"path": "a", "op": "changed"}, []any{1.0, 1.0, 2.0, 2.0, 1.0}, []int{2, 4}},
		{"change_pct", map[string]any{"path": "a", "op": "change_pct", "threshold": 20}, []any{100.0, 110.0, 140.0, 100.0}, []int{2, 3}},
		{"numeric strings", map[string]any{"path": "a", "op": "gt", "threshold": "1.5"}, []any{"1.25", "2.00"}, []int{1}},
	}
	for _, tc := range cases {
		m := mustWatchMetric(t, tc.metric)
		if got := runMetricSeries(m, tc.values...); !sameInts(got, tc.want) {
			t.Errorf("%s: fired at %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWatchMetricGroupedCrossing(t *testing.T) {
	m := mustWatchMetric(t, map[string]any{"path": "orders", "key": "region", "value": "avg_ship_hours", "op": "gt", "threshold": 48})
	rows := func(pairs ...any) any {
		var out []any
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, map[string]any{"region": pairs[i], "avg_ship_hours": pairs[i+1]})
		}
		return map[string]any{"orders": out}
	}
	at := time.Now()
	var st watchMetricState
	step := func(data any) []watchMetricCrossing {
		t.Helper()
		obs, err := m.observe(data)
		if err != nil {
			t.Fatal(err)
		}
		var c []watchMetricCrossing
		st, c = m.evaluate(st, obs, at)
		return c
	}
	if c := step(rows("east", 30.0, "west", 50.0)); len(c) != 1 || c[0].Key != "west" {
		t.Fatalf("first tick crossings = %+v, want west", c)
	}
	if c := step(rows("east", 60.0, "west", 52.0)); len(c) != 1 || c[0].Key != "east" || c[0].Previous != 30.0 {
		t.Fatalf("second tick crossings = %+v, want only east (west is still over)", c)
	}
	if !st.Breached || len(st.Groups) != 2 || st.History[len(st.History)-1].Value != 2 {
		t.Fatalf("state = %+v", st)
	}
}

func TestWatchMetricHistoryIsCapped(t *testing.T) {
	m := mustWatchMetric(t, map[string]any{"path": "a", "op": "gt", "threshold": 1})
	var st watchMetricState
	for i := 0; i < watchMetricHistoryMax+10; i++ {
		st, _ = m.evaluate(st, watchMetricObservation{Value: float64(i)}, time.Now())
	}
	if len(st.History) != watchMetricHistoryMax || st.History[len(st.History)-1].Value != float64(watchMetricHistoryMax+9) {
		t.Fatalf("history len=%d last=%v", len(st.History), st.History[len(st.History)-1])
	}
}

func TestParseWatchMetricInputValidation(t *testing.T) {
	for _, raw := range []any{nil, "", "null", map[string]any{}} {
		if _, ok, err := parseWatchMetricInput(raw); ok || err != nil {
			t.Fatalf("parseWatchMetricInput(%#v) ok=%v err=%v, want none", raw, ok, err)
		}
	}
	if m, ok, err := parseWatchMetricInput(`{"path":"a.0.count_id","op":"GT","threshold":"7"}`); err != nil || !ok || m.Op != "gt" || m.Threshold != 7.0 || m.Fire != "crossing" {
		t.Fatalf("string input = %+v ok=%v err=%v", m, ok, err)
	}
	for _, tc := range []struct {
		raw  map[string]any
		want string
	}{
		{map[string]any{"op": "gt", "threshold": 1}, "metric.path is required"},
		{map[string]any{"path": "a", "op": "above", "threshold": 1}, "not supported"},
		{map[string]any{"path": "a", "op": "gt"}, "numeric threshold"},
		{map[string]any{"path": "a", "op": "gt", "threshold": "lots"}, "numeric threshold"},
		{map[string]any{"path": "a", "op": "changed", "threshold": 1}, "takes no threshold"},
		{map[string]any{"path": "a", "op": "eq"}, "needs a threshold"},
		{map[string]any{"path": "a", "op": "change_pct", "threshold": 0}, "positive threshold"},
		{map[string]any{"path": "a", "key": "region", "op": "gt", "threshold": 1}, "go together"},
		{map[string]any{"path": "a", "op": "gt", "threshold": 1, "fire": "always"}, "metric.fire"},
		{map[string]any{"path": "a", "op": "gt", "threshold": 1, "window": "1h"}, `unknown field "window"`},
	} {
		if _, _, err := parseWatchMetricInput(tc.raw); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseWatchMetricInput(%v) err = %v, want %q", tc.raw, err, tc.want)
		}
	}
}

func TestWatchMetricObserveErrorsNameWhatIsThere(t *testing.T) {
	data := map[string]any{"orders": []any{map[string]any{"count_id": 3.0, "status": "open"}}}
	for _, tc := range []struct {
		metric map[string]any
		want   string
	}{
		{map[string]any{"path": "order.0.count_id", "op": "gt", "threshold": 1}, "available keys: orders"},
		{map[string]any{"path": "orders.count_id", "op": "gt", "threshold": 1}, "use an index such as orders.0"},
		{map[string]any{"path": "orders.2.count_id", "op": "gt", "threshold": 1}, "has 1 rows"},
		{map[string]any{"path": "orders.0.total", "op": "gt", "threshold": 1}, "available keys: count_id, status"},
		{map[string]any{"path": "orders.0.status", "op": "gt", "threshold": 1}, "not a number"},
		{map[string]any{"path": "orders", "key": "region", "value": "count_id", "op": "gt", "threshold": 1}, "available columns: count_id, status"},
	} {
		m := mustWatchMetric(t, tc.metric)
		if _, err := m.observe(data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("observe(%v) err = %v, want %q", tc.metric, err, tc.want)
		}
	}
	// eq on text needs no number.
	m := mustWatchMetric(t, map[string]any{"path": "orders.0.status", "op": "eq", "threshold": "open"})
	if obs, err := m.observe(data); err != nil || obs.Value != "open" {
		t.Fatalf("observe text = %+v err %v", obs, err)
	}
}

func TestWatchMetricTimeVariables(t *testing.T) {
	ny := mustLocation(t, "America/New_York")
	// Wednesday 2026-03-04 15:30 in New York.
	now := time.Date(2026, 3, 4, 20, 30, 0, 0, time.UTC)
	for spec, want := range map[string]string{
		"now":       "2026-03-04T20:30:00Z",
		"now-24h":   "2026-03-03T20:30:00Z",
		"now-90m":   "2026-03-04T19:00:00Z",
		"today":     "2026-03-04T05:00:00Z",
		"today-7d":  "2026-02-25T05:00:00Z",
		"week":      "2026-03-02T05:00:00Z",
		"week-1w":   "2026-02-23T05:00:00Z",
		"month":     "2026-03-01T05:00:00Z",
		"year":      "2026-01-01T05:00:00Z",
		" TODAY+1d": "2026-03-05T05:00:00Z",
	} {
		got, err := watchMetricTime(spec, now, ny)
		if err != nil || got.UTC().Format(time.RFC3339) != want {
			t.Errorf("watchMetricTime(%q) = %s err %v, want %s", spec, got.UTC().Format(time.RFC3339), err, want)
		}
	}
	if _, err := watchMetricTime("yesterday", now, ny); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("bad spec err = %v", err)
	}
	vars, err := resolveWatchMetricVariables(`{"since":{"$time":"today"},"status":"open","raw":{"a":1}}`, now, ny)
	if err != nil {
		t.Fatal(err)
	}
	if string(vars) != `{"raw":{"a":1},"since":"2026-03-04T05:00:00Z","status":"open"}` {
		t.Fatalf("resolved vars = %s", vars)
	}
}

func insertMetricWatch(t *testing.T, svc *graphjinService, ctx context.Context, name string, input map[string]any) (map[string]any, error) {
	t.Helper()
	base := map[string]any{
		"name":          name,
		"schedule":      map[string]any{"cron": "*/5 * * * *", "timezone": "UTC"},
		"query":         `query { orders(where: { status: { eq: "open" } }) { count_id } }`,
		"metric":        map[string]any{"path": "orders.0.count_id", "op": "gt", "threshold": 1},
		"delivery_json": map[string]any{"kind": "inbox"},
	}
	for k, v := range input {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	return newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert", Input: base,
	})
}

func setOpenOrders(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM orders`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := db.Exec(`INSERT INTO orders (id, status, updated_at) VALUES (?, 'open', '2026-01-01')`, i); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMetricWatchCreateValidation(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	ctx := artifactUserCtx("user_1")
	setOpenOrders(t, db, 1)

	row, err := insertMetricWatch(t, svc, ctx, "open_orders", nil)
	if err != nil {
		t.Fatalf("create metric watch: %v", err)
	}
	state := mapFromAny(row["metric_state"])
	if state["last_value"] != 1.0 || state["breached"] != false || mapFromAny(row["metric"])["op"] != "gt" {
		t.Fatalf("created watch should report the current reading: metric=%v state=%v", row["metric"], row["metric_state"])
	}

	for _, tc := range []struct {
		input map[string]any
		want  string
	}{
		{map[string]any{"schedule": nil}, "check their query on a schedule"},
		{map[string]any{"metric": nil}, "add metric to check a number"},
		{map[string]any{"query": nil}, "metric needs query"},
		{map[string]any{"query": `subscription { orders { count_id } }`}, "plain query operation"},
		{map[string]any{"metric": map[string]any{"path": "orders.0.count", "op": "gt", "threshold": 1}}, "available keys: count_id"},
		{map[string]any{"query": `query { nope { count_id } }`}, "metric probe failed"},
	} {
		if _, err := insertMetricWatch(t, svc, ctx, "bad_metric", tc.input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("create with %v err = %v, want %q", tc.input, err, tc.want)
		}
	}

	// The row-stream path explains the way out instead of a bare refusal.
	_, err = newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "agg_sub", "query": `subscription aggSub { orders { count_id } }`},
	})
	if err == nil || !strings.Contains(err.Error(), "cursor pagination") || !strings.Contains(err.Error(), "schedule + metric") {
		t.Fatalf("aggregate subscription err = %v", err)
	}
}

func metricWatchState(t *testing.T, svc *graphjinService, ctx context.Context, id string) watchMetricState {
	t.Helper()
	row, err := svc.internalWatchStoreRow(ctx, id)
	if err != nil || row == nil {
		t.Fatalf("load watch %s: %v", id, err)
	}
	return parseWatchMetricState(jsonMapString(row, "metric_state_json"))
}

func TestMetricWatchFiresOnceOnCrossingAcrossReplicas(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	ctx := artifactUserCtx("user_1")
	setOpenOrders(t, db, 1)
	row, err := insertMetricWatch(t, replicas[0], ctx, "open_orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := stringFromAny(row["id"])
	t0 := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	setWatchNextFire(t, db, id, t0)

	tick := func(i int) int {
		t.Helper()
		now := t0.Add(time.Duration(i)*5*time.Minute + 2*time.Second)
		var wg sync.WaitGroup
		var mu sync.Mutex
		fired := 0
		start := make(chan struct{})
		for _, svc := range replicas {
			wg.Add(1)
			go func(svc *graphjinService) {
				defer wg.Done()
				<-start
				sweep, err := svc.sweepWatchSchedules(ctx, now)
				if err != nil {
					t.Errorf("tick %d sweep: %v", i, err)
				}
				mu.Lock()
				fired += sweep.Fired
				mu.Unlock()
			}(svc)
		}
		close(start)
		wg.Wait()
		return fired
	}

	steps := []struct {
		open      int
		wantFired int
	}{
		{1, 0}, // at threshold, not over
		{3, 1}, // crosses: one event across both replicas
		{4, 0}, // still over: no repeat
		{0, 0}, // recovers: re-arms
		{2, 1}, // crosses again
	}
	for i, step := range steps {
		setOpenOrders(t, db, step.open)
		if got := tick(i); got != step.wantFired {
			t.Fatalf("step %d (%d open) fired %d, want %d", i, step.open, got, step.wantFired)
		}
		st := metricWatchState(t, replicas[1], ctx, id)
		if st.LastValue != float64(step.open) || st.Breached != (step.open > 1) {
			t.Fatalf("step %d state = %+v", i, st)
		}
	}
	if ids := watchEventIDs(t, db); len(ids) != 2 {
		t.Fatalf("events = %v, want 2", ids)
	}
	if err := replicas[1].processPendingWatchDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := newWatchControlPlane(replicas[0]).watchEventRows(ctx)
	if err != nil || len(events) != 2 {
		t.Fatalf("events %+v err %v", events, err)
	}
	for _, event := range events {
		data := mapFromAny(parseJSONValue(jsonMapString(event, "data_json")))
		if data["kind"] != "metric" || event["delivery_status"] != "delivered" {
			t.Fatalf("metric event = %+v data %+v", event, data)
		}
		v := data["value"]
		if !(v == 3.0 && data["previous"] == 1.0) && !(v == 2.0 && data["previous"] == 0.0) {
			t.Fatalf("metric event value/previous = %v/%v", v, data["previous"])
		}
	}
	if st := metricWatchState(t, replicas[0], ctx, id); len(st.History) != len(steps)+1 {
		t.Fatalf("history = %+v, want the probe plus one point per tick", st.History)
	}
}

// A replica that wrote the event and died before saving the state must not
// make the next sweep fire the same breach again.
func TestMetricWatchRecoversFromCrashBeforeStateSave(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	crashed, survivor := replicas[0], replicas[1]
	ctx := artifactUserCtx("user_1")
	setOpenOrders(t, db, 0)
	row, err := insertMetricWatch(t, crashed, ctx, "crash_metric", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := stringFromAny(row["id"])
	sched := mustWatchSchedule(t, "*/5 * * * *", "UTC")
	due := time.Now().UTC().Truncate(5 * time.Minute)
	setWatchNextFire(t, db, id, due)
	setOpenOrders(t, db, 5)

	stored, _ := crashed.internalWatchStoreRow(ctx, id)
	def := watchScheduleDefinition(crashed, stored)
	m, _ := parseWatchMetricJSON(jsonMapString(stored, "metric_json"))
	state, crossings, err := crashed.evaluateWatchMetricTick(ctx, stored, def, m, sched, due)
	if err != nil || len(crossings) != 1 {
		t.Fatalf("crashed evaluate crossings=%v err=%v", crossings, err)
	}
	if inserted, err := crashed.insertWatchScheduleEvent(context.Background(), &def, watchActionProposal{}, sched, due, due.Add(time.Second), watchMetricEventPayload(m, state, crossings)); err != nil || !inserted {
		t.Fatalf("crashed insert=%v err=%v", inserted, err)
	}

	if sweep, err := survivor.sweepWatchSchedules(ctx, due.Add(5*time.Second)); err != nil || sweep.Fired != 0 {
		t.Fatalf("survivor sweep fired=%d err=%v", sweep.Fired, err)
	}
	if st := metricWatchState(t, survivor, ctx, id); !st.Breached || st.LastValue != 5.0 {
		t.Fatalf("survivor should record the breach: %+v", st)
	}
	if sweep, err := survivor.sweepWatchSchedules(ctx, due.Add(5*time.Minute+time.Second)); err != nil || sweep.Fired != 0 {
		t.Fatalf("next tick fired=%d err=%v; the breach was already reported", sweep.Fired, err)
	}
	if ids := watchEventIDs(t, db); len(ids) != 1 {
		t.Fatalf("events = %v, want only the crashed replica's", ids)
	}
}

// A failing query records the error and keeps the schedule moving.
func TestMetricWatchQueryErrorKeepsSchedule(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	ctx := artifactUserCtx("user_1")
	setOpenOrders(t, db, 0)
	row, err := insertMetricWatch(t, svc, ctx, "flaky_metric", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(5 * time.Minute)
	setWatchNextFire(t, db, id, due)
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET metric_json = ? WHERE id = ?`, `{"path":"orders.0.missing","op":"gt","threshold":1}`, id); err != nil {
		t.Fatal(err)
	}
	now := due.Add(time.Second)
	if _, err := svc.sweepWatchSchedules(ctx, now); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	stored, _ := svc.internalWatchStoreRow(ctx, id)
	if !strings.Contains(stringMapValue(stored, "last_error"), "available keys: count_id") || stringMapValue(stored, "status") != "active" {
		t.Fatalf("watch after a failing tick = status %v last_error %q", stored["status"], stringMapValue(stored, "last_error"))
	}
	if got := storedWatchNextFire(t, db, id); got == formatWatchFireTime(due) {
		t.Fatalf("schedule did not move after a failing tick")
	}
}

// gj_watch is served from two schemas: the store-backed managed table and the
// nano system DB the agentic demo uses. A field added to one but not the other
// works in tests and fails in the demo, as schedule and metric once did.
func TestWatchColumnsMatchNanoSystemColumns(t *testing.T) {
	nano := map[string]bool{}
	for _, c := range watchNanoColumns() {
		nano[c.Name] = true
	}
	for _, c := range watchColumns() {
		if !nano[c.Name] {
			t.Errorf("gj_watch column %q is missing from watchNanoColumns", c.Name)
		}
	}
}

// A non-admin owner reads gj_watch through a column allowlist; a field missing
// from it fails every console query that selects it.
func TestWatchOwnerProjectionCarriesScheduleAndMetricFields(t *testing.T) {
	cols := map[string]bool{}
	for _, c := range watchPublicProjectionColumns() {
		cols[c] = true
	}
	for _, want := range []string{"schedule", "next_fire_at", "metric", "metric_state", "routine_thread_id", "absence_json"} {
		if !cols[want] {
			t.Errorf("watchPublicProjectionColumns is missing %q", want)
		}
	}
	events := map[string]bool{}
	for _, c := range watchEventPublicProjectionColumns() {
		events[c] = true
	}
	if !events["snoozed_until"] {
		t.Errorf("watchEventPublicProjectionColumns is missing snoozed_until")
	}
}

// Pausing or retuning a watch by id sends only what changes; the rest of the
// definition comes from the stored watch.
func TestWatchPartialUpdateByID(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	ctx := artifactUserCtx("user_1")
	setOpenOrders(t, db, 2)
	row, err := insertMetricWatch(t, svc, ctx, "partial_update", map[string]any{
		"delivery_json": map[string]any{"kind": "inbox", "digest": map[string]any{"window": "1h"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := stringFromAny(row["id"])
	update := func(input map[string]any) map[string]any {
		t.Helper()
		out, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
			Table: watchesRootTable, Operation: "update", Input: input,
			Where: map[string]any{"id": map[string]any{"eq": id}},
		})
		if err != nil {
			t.Fatalf("update %v: %v", input, err)
		}
		return out
	}
	paused := update(map[string]any{"enabled": false})
	if paused["enabled"] != false || paused["name"] != "partial_update" || mapFromAny(paused["metric"])["threshold"] != 1.0 {
		t.Fatalf("paused watch = %+v", paused)
	}
	if delivery := mapFromAny(parseJSONValue(jsonMapString(paused, "delivery_json"))); mapFromAny(delivery["digest"])["window"] != "1h0m0s" {
		t.Fatalf("pause dropped the delivery digest: %v", paused["delivery_json"])
	}
	retuned := update(map[string]any{"enabled": true, "metric": map[string]any{"path": "orders.0.count_id", "op": "gt", "threshold": 5}})
	if retuned["enabled"] != true || mapFromAny(retuned["metric"])["threshold"] != 5.0 || mapFromAny(retuned["schedule"])["cron"] != "*/5 * * * *" {
		t.Fatalf("retuned watch = %+v", retuned)
	}

	// Another owner cannot reach it by id.
	_, err = newWatchControlPlane(svc).mutateRow(artifactUserCtx("user_2"), core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update", Input: map[string]any{"enabled": false},
		Where: map[string]any{"id": map[string]any{"eq": id}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("other owner's partial update err = %v", err)
	}
}
