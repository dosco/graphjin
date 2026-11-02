package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestParseWatchScheduleValidation(t *testing.T) {
	valid := []struct {
		cron, tz         string
		wantCron, wantTZ string
	}{
		{"0 9 * * 1-5", "America/New_York", "0 9 * * 1-5", "America/New_York"},
		{"*/5 * * * *", "", "*/5 * * * *", "UTC"},
		{"  0   9 * *   *  ", " Europe/Berlin ", "0 9 * * *", "Europe/Berlin"},
		{"0 0 1 jan,jul *", "UTC", "0 0 1 jan,jul *", "UTC"},
	}
	for _, tc := range valid {
		sched, err := parseWatchSchedule(tc.cron, tc.tz)
		if err != nil {
			t.Fatalf("parseWatchSchedule(%q, %q): %v", tc.cron, tc.tz, err)
		}
		if sched.Cron != tc.wantCron || sched.Timezone != tc.wantTZ {
			t.Fatalf("parseWatchSchedule(%q, %q) = %q/%q, want %q/%q", tc.cron, tc.tz, sched.Cron, sched.Timezone, tc.wantCron, tc.wantTZ)
		}
	}

	invalid := []struct {
		cron, tz, want string
	}{
		{"0 9 * *", "UTC", "must have 5 fields"},
		{"0 0 9 * * *", "UTC", "must have 5 fields"},
		{"@daily", "UTC", "not supported"},
		{"CRON_TZ=UTC 0 9 * * *", "UTC", "not supported"},
		{"61 * * * *", "UTC", "is invalid"},
		{"0 25 * * *", "UTC", "is invalid"},
		{"0 9 * * mon-fri-sat", "UTC", "is invalid"},
		{"0 0 30 2 *", "UTC", "never fires"},
		{"0 9 * * *", "Mars/Olympus_Mons", "not a known IANA zone"},
		{"0 9 * * *", "Local", "must be an IANA zone"},
		{"0 9 * * *", "local", "must be an IANA zone"},
		{strings.Repeat("1,", 200) + "1 * * * *", "UTC", "longer than"},
	}
	for _, tc := range invalid {
		_, err := parseWatchSchedule(tc.cron, tc.tz)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("parseWatchSchedule(%q, %q) error = %v, want %q", tc.cron, tc.tz, err, tc.want)
		}
	}
}

func TestParseWatchScheduleInput(t *testing.T) {
	for _, raw := range []any{nil, "", "null", "  ", map[string]any{}, "{}"} {
		_, ok, err := parseWatchScheduleInput(raw)
		if err != nil || ok {
			t.Fatalf("parseWatchScheduleInput(%#v) = ok %v err %v, want no schedule", raw, ok, err)
		}
	}
	for _, raw := range []any{
		map[string]any{"cron": "0 9 * * *", "timezone": "Asia/Kolkata"},
		`{"cron":"0 9 * * *","timezone":"Asia/Kolkata"}`,
	} {
		sched, ok, err := parseWatchScheduleInput(raw)
		if err != nil || !ok || sched.Cron != "0 9 * * *" || sched.Timezone != "Asia/Kolkata" {
			t.Fatalf("parseWatchScheduleInput(%#v) = %+v ok %v err %v", raw, sched, ok, err)
		}
	}
	for _, tc := range []struct {
		raw  any
		want string
	}{
		{map[string]any{"cron": "0 9 * * *", "tz": "UTC"}, `unknown field "tz"`},
		{map[string]any{"timezone": "UTC"}, "schedule.cron is required"},
		{map[string]any{"cron": "0 9 * * *", "timezone": 5}, "must be a string"},
		{`["0 9 * * *"]`, "must be an object"},
		{42, "must be an object"},
		{map[string]any{"cron": "nope"}, "must have 5 fields"},
	} {
		_, _, err := parseWatchScheduleInput(tc.raw)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("parseWatchScheduleInput(%#v) error = %v, want %q", tc.raw, err, tc.want)
		}
	}
}

func mustWatchSchedule(t *testing.T, cronExpr, tz string) watchSchedule {
	t.Helper()
	sched, err := parseWatchSchedule(cronExpr, tz)
	if err != nil {
		t.Fatalf("parseWatchSchedule(%q, %q): %v", cronExpr, tz, err)
	}
	return sched
}

func assertWatchFireSequence(t *testing.T, sched watchSchedule, from string, want ...string) {
	t.Helper()
	cursor, err := time.Parse(time.RFC3339, from)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range want {
		cursor = sched.next(cursor)
		if got := formatWatchFireTime(cursor); got != w {
			t.Fatalf("%s/%s fire %d after %s = %s (%s local), want %s",
				sched.Cron, sched.Timezone, i, from, got, cursor.In(sched.spec.Location).Format(time.RFC3339), w)
		}
	}
}

func TestWatchScheduleNextFire(t *testing.T) {
	assertWatchFireSequence(t, mustWatchSchedule(t, "*/5 * * * *", "UTC"), "2026-11-03T10:02:30Z",
		"2026-11-03T10:05:00Z", "2026-11-03T10:10:00Z", "2026-11-03T10:15:00Z")
	// Strictly after: a time already on a tick moves to the following one.
	assertWatchFireSequence(t, mustWatchSchedule(t, "*/5 * * * *", "UTC"), "2026-11-03T10:05:00Z",
		"2026-11-03T10:10:00Z")
	// Weekdays at 09:00 New York (EST, UTC-5 in November): Friday then Monday.
	assertWatchFireSequence(t, mustWatchSchedule(t, "0 9 * * 1-5", "America/New_York"), "2026-11-06T13:00:00Z",
		"2026-11-06T14:00:00Z", "2026-11-09T14:00:00Z")
	// Half-hour offset zone.
	assertWatchFireSequence(t, mustWatchSchedule(t, "0 9 * * *", "Asia/Kolkata"), "2026-11-03T00:00:00Z",
		"2026-11-03T03:30:00Z", "2026-11-04T03:30:00Z")
	// The zone decides the calendar day, not UTC: 23:30 local on the 1st of
	// the month in Tokyo is still the previous day in UTC.
	assertWatchFireSequence(t, mustWatchSchedule(t, "30 23 1 * *", "Asia/Tokyo"), "2026-11-03T00:00:00Z",
		"2026-12-01T14:30:00Z")
}

func TestWatchScheduleNextFireAcrossDST(t *testing.T) {
	// US spring forward 2026-03-08: 02:00 EST jumps to 03:00 EDT (07:00Z).
	// A daily 02:30 job fires at the jump instead of skipping the day.
	assertWatchFireSequence(t, mustWatchSchedule(t, "30 2 * * *", "America/New_York"), "2026-03-07T17:00:00Z",
		"2026-03-08T07:00:00Z", "2026-03-09T06:30:00Z", "2026-03-10T06:30:00Z")
	// An hourly job neither skips nor doubles across the jump.
	assertWatchFireSequence(t, mustWatchSchedule(t, "0 * * * *", "America/New_York"), "2026-03-08T05:30:00Z",
		"2026-03-08T06:00:00Z", "2026-03-08T07:00:00Z", "2026-03-08T08:00:00Z")
	// Several matches inside the skipped hour still produce one fire.
	assertWatchFireSequence(t, mustWatchSchedule(t, "*/20 2 * * *", "America/New_York"), "2026-03-08T06:30:00Z",
		"2026-03-08T07:00:00Z", "2026-03-09T06:00:00Z")

	// US fall back 2026-11-01: 02:00 EDT returns to 01:00 EST (06:00Z), so
	// 01:30 happens twice. A daily 01:30 job fires once.
	assertWatchFireSequence(t, mustWatchSchedule(t, "30 1 * * *", "America/New_York"), "2026-10-31T16:00:00Z",
		"2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z")
	// An hourly job fires in both passes: they are different real hours.
	assertWatchFireSequence(t, mustWatchSchedule(t, "0 * * * *", "America/New_York"), "2026-11-01T04:30:00Z",
		"2026-11-01T05:00:00Z", "2026-11-01T06:00:00Z", "2026-11-01T07:00:00Z")
	// A fixed-hour, every-15-minutes job covers the first pass only.
	assertWatchFireSequence(t, mustWatchSchedule(t, "*/15 1 * * *", "America/New_York"), "2026-11-01T05:40:00Z",
		"2026-11-01T05:45:00Z", "2026-11-02T06:00:00Z")

	// Europe/London spring forward 2026-03-29: 01:00 GMT jumps to 02:00 BST.
	assertWatchFireSequence(t, mustWatchSchedule(t, "30 1 * * *", "Europe/London"), "2026-03-28T12:00:00Z",
		"2026-03-29T01:00:00Z", "2026-03-30T00:30:00Z")
	// Lord Howe Island shifts by 30 minutes; 02:00-02:30 is skipped on
	// 2026-10-04 (15:30Z) and 01:30-02:00 repeats on 2026-04-05.
	assertWatchFireSequence(t, mustWatchSchedule(t, "15 2 * * *", "Australia/Lord_Howe"), "2026-10-03T12:00:00Z",
		"2026-10-03T15:30:00Z", "2026-10-04T15:15:00Z")
	assertWatchFireSequence(t, mustWatchSchedule(t, "45 1 * * *", "Australia/Lord_Howe"), "2026-04-04T12:00:00Z",
		"2026-04-04T14:45:00Z", "2026-04-05T15:15:00Z")
}

func newScheduleWatchReplicas(t *testing.T, count int) (*sql.DB, []*graphjinService) {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/watches.db?_pragma=busy_timeout(10000)"
	var first *sql.DB
	var svcs []*graphjinService
	for i := 0; i < count; i++ {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		_, svc := newSQLiteWatchServiceWithDB(t, db, dsn, 20)
		if err := svc.initArtifactsBeforeCore(); err != nil {
			t.Fatalf("initArtifactsBeforeCore replica %d: %v", i, err)
		}
		startSQLiteWatchCore(t, svc, db)
		if first == nil {
			first = db
		}
		svcs = append(svcs, svc)
	}
	return first, svcs
}

func insertScheduleWatch(t *testing.T, svc *graphjinService, ctx context.Context, name string, schedule map[string]any) map[string]any {
	t.Helper()
	row, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]any{
			"name":          name,
			"schedule":      schedule,
			"delivery_json": map[string]any{"kind": "inbox"},
		},
	})
	if err != nil {
		t.Fatalf("insert schedule watch %s: %v", name, err)
	}
	return row
}

func setWatchNextFire(t *testing.T, db *sql.DB, id string, at time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = ? WHERE id = ?`, formatWatchFireTime(at), id); err != nil {
		t.Fatalf("set next_fire_at: %v", err)
	}
}

func storedWatchNextFire(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var next sql.NullString
	if err := db.QueryRow(`SELECT next_fire_at FROM "_graphjin_watches" WHERE id = ?`, id).Scan(&next); err != nil {
		t.Fatalf("read next_fire_at: %v", err)
	}
	return next.String
}

func TestWatchScheduleTwoRunnersFireOncePerTick(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, replicas[0], ctx, "five_minute_digest", map[string]any{"cron": "*/5 * * * *", "timezone": "UTC"})
	id := stringFromAny(row["id"])
	sched := mustWatchSchedule(t, "*/5 * * * *", "UTC")

	// Start the clock an hour back so every tick is inside event retention.
	t0 := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	setWatchNextFire(t, db, id, t0)

	const ticks = 6
	wantIDs := map[string]bool{}
	for i := 0; i < ticks; i++ {
		due := t0.Add(time.Duration(i) * 5 * time.Minute)
		now := due.Add(2 * time.Second)
		eventID, _ := watchScheduleEventID(id, due)
		wantIDs[eventID] = true

		// Every replica sweeps the same instant, twice, all at once.
		var wg sync.WaitGroup
		start := make(chan struct{})
		var mu sync.Mutex
		fired := 0
		for _, svc := range replicas {
			for range 2 {
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
		}
		close(start)
		wg.Wait()
		if fired != 1 {
			t.Fatalf("tick %d (%s) fired %d times across replicas, want exactly 1", i, formatWatchFireTime(due), fired)
		}
		if got, want := storedWatchNextFire(t, db, id), formatWatchFireTime(sched.next(now)); got != want {
			t.Fatalf("tick %d next_fire_at = %s, want %s", i, got, want)
		}
		// A sweep after the tick has fired finds nothing due.
		if sweep, err := replicas[1].sweepWatchSchedules(ctx, now); err != nil || sweep.Fired != 0 {
			t.Fatalf("tick %d re-sweep fired=%d err=%v", i, sweep.Fired, err)
		}
	}

	ids := watchEventIDs(t, db)
	if len(ids) != ticks {
		t.Fatalf("watch events = %v, want %d", ids, ticks)
	}
	for _, eventID := range ids {
		if !wantIDs[eventID] {
			t.Fatalf("unexpected event id %s, want one of %v", eventID, wantIDs)
		}
	}

	// The events take the normal delivery path.
	if err := replicas[1].processPendingWatchDeliveries(ctx); err != nil {
		t.Fatalf("processPendingWatchDeliveries: %v", err)
	}
	rows, err := newWatchControlPlane(replicas[0]).watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows: %v", err)
	}
	if len(rows) != ticks {
		t.Fatalf("projected events = %d, want %d", len(rows), ticks)
	}
	for _, event := range rows {
		if event["delivery_status"] != "delivered" {
			t.Fatalf("schedule event delivery_status = %v, want delivered: %+v", event["delivery_status"], event)
		}
		data := mapFromAny(parseJSONValue(jsonMapString(event, "data_json")))
		if data["kind"] != "schedule" || data["watch_id"] != id || data["cron"] != "*/5 * * * *" {
			t.Fatalf("schedule event payload = %+v", data)
		}
		if due, ok := parseWatchTime(stringFromAny(data["scheduled_for"])); !ok || due.Before(t0) {
			t.Fatalf("schedule event scheduled_for = %v", data["scheduled_for"])
		}
	}
}

func TestWatchScheduleAdvanceIsExclusive(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, replicas[0], ctx, "hourly", map[string]any{"cron": "0 * * * *"})
	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(time.Hour)
	next := due.Add(time.Hour)
	setWatchNextFire(t, db, id, due)

	// Every replica read the same row and computed the same next fire time,
	// which is exactly the case a read-back check cannot tell apart.
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		svc := replicas[i%len(replicas)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := svc.advanceWatchSchedule(ctx, id, formatWatchFireTime(due), due, next)
			if err != nil {
				t.Errorf("advance: %v", err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("advances won = %d, want exactly 1", wins)
	}
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(next) {
		t.Fatalf("next_fire_at = %s, want %s", got, formatWatchFireTime(next))
	}
}

func TestWatchScheduleMigratesStoreWithoutScheduleColumns(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/old-watches.db"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Today's store minus the schedule columns: what every existing
	// deployment has on disk before this change.
	for _, stmt := range watchDDL("sqlite", "_graphjin") {
		var kept []string
		for _, line := range strings.Split(stmt, "\n") {
			if strings.HasPrefix(line, "schedule_cron ") || strings.HasPrefix(line, "schedule_tz ") || strings.HasPrefix(line, "next_fire_at ") {
				continue
			}
			kept = append(kept, line)
		}
		if _, err := db.Exec(strings.Join(kept, "\n")); err != nil {
			t.Fatalf("create old store: %v", err)
		}
	}
	for _, column := range []string{"schedule_cron", "schedule_tz", "next_fire_at"} {
		if ok, err := sqliteColumnExists(context.Background(), db, quoteSQLIdent("_graphjin_watches"), column); err != nil || ok {
			t.Fatalf("old store already has %s (err %v)", column, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, owner_id) VALUES ('watch:legacy', 'legacy', 'subscription { orders { id } }', 'user_1')`); err != nil {
		t.Fatalf("insert legacy watch: %v", err)
	}

	_, svc := newSQLiteWatchServiceWithDB(t, db, dsn, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	for _, column := range []string{"schedule_cron", "schedule_tz", "next_fire_at"} {
		if ok, err := sqliteColumnExists(context.Background(), db, quoteSQLIdent("_graphjin_watches"), column); err != nil || !ok {
			t.Fatalf("migration did not add %s (err %v)", column, err)
		}
	}
	// Running the migration again is a no-op.
	if err := svc.migrateWatchSchema(context.Background(), db, "sqlite", "_graphjin"); err != nil {
		t.Fatalf("second migrateWatchSchema: %v", err)
	}
	var cronExpr, tz string
	var next sql.NullString
	if err := db.QueryRow(`SELECT schedule_cron, schedule_tz, next_fire_at FROM "_graphjin_watches" WHERE id = 'watch:legacy'`).Scan(&cronExpr, &tz, &next); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if cronExpr != "" || tz != "" || next.Valid {
		t.Fatalf("legacy row schedule = %q/%q/%v, want empty", cronExpr, tz, next)
	}

	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, svc, ctx, "migrated_schedule", map[string]any{"cron": "0 * * * *", "timezone": "UTC"})
	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(time.Hour)
	setWatchNextFire(t, db, id, due)
	sweep, err := svc.sweepWatchSchedules(ctx, due.Add(time.Second))
	if err != nil || sweep.Fired != 1 {
		t.Fatalf("sweep on migrated store fired=%d err=%v", sweep.Fired, err)
	}
	if want, _ := watchScheduleEventID(id, due); !slices.Contains(watchEventIDs(t, db), want) {
		t.Fatalf("migrated store events = %v, want %s", watchEventIDs(t, db), want)
	}
}

func TestUpsertScheduleWatchDefinition(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")

	for _, tc := range []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"with_query", map[string]any{"query": cursorOrdersWatchQuery("with_query"), "schedule": map[string]any{"cron": "0 9 * * *"}}, "omit query and saved_query_name"},
		{"with_saved", map[string]any{"saved_query_name": "orders", "schedule": map[string]any{"cron": "0 9 * * *"}}, "omit query and saved_query_name"},
		{"with_absence", map[string]any{"schedule": map[string]any{"cron": "0 9 * * *"}, "absence_json": map[string]any{"enabled": true, "window": "1h"}}, "absence_json needs a data subscription"},
		{"bad_cron", map[string]any{"schedule": map[string]any{"cron": "0 9 * *"}}, "must have 5 fields"},
		{"bad_tz", map[string]any{"schedule": map[string]any{"cron": "0 9 * * *", "timezone": "Nowhere/Land"}}, "not a known IANA zone"},
		{"nothing", map[string]any{}, "requires query, saved_query_name, or schedule"},
	} {
		input := tc.input
		input["name"] = tc.name
		_, err := cp.mutateRow(ctx, core.ManagedMutationRoot{Table: watchesRootTable, Operation: "insert", Input: input})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error = %v, want %q", tc.name, err, tc.want)
		}
	}

	before := time.Now().UTC()
	row := insertScheduleWatch(t, svc, ctx, "morning_brief", map[string]any{"cron": "0 9 * * 1-5", "timezone": "America/New_York"})
	id := stringFromAny(row["id"])
	schedule := mapFromAny(row["schedule"])
	if schedule["cron"] != "0 9 * * 1-5" || schedule["timezone"] != "America/New_York" {
		t.Fatalf("projected schedule = %+v", row["schedule"])
	}
	next, ok := parseWatchTime(stringFromAny(row["next_fire_at"]))
	want := mustWatchSchedule(t, "0 9 * * 1-5", "America/New_York").next(before)
	if !ok || !next.Equal(want) {
		t.Fatalf("next_fire_at = %v, want %s", row["next_fire_at"], formatWatchFireTime(want))
	}
	if !watchRowRunnable(row) {
		t.Fatalf("inbox schedule watch should be runnable: %+v", row)
	}
	// A schedule watch never reaches the subscription runner.
	defs, err := svc.loadRunnableWatches(ctx)
	if err != nil {
		t.Fatalf("loadRunnableWatches: %v", err)
	}
	for _, def := range defs {
		if def.ID == id {
			t.Fatalf("schedule watch was loaded as a subscription: %+v", def)
		}
	}

	// An edit that leaves the schedule alone does not touch the pending tick.
	pinned := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)
	setWatchNextFire(t, db, id, pinned)
	update := func(input map[string]any) map[string]any {
		t.Helper()
		input["name"] = "morning_brief"
		input["delivery_json"] = map[string]any{"kind": "inbox"}
		row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{Table: watchesRootTable, Operation: "update", Input: input})
		if err != nil {
			t.Fatalf("update schedule watch: %v", err)
		}
		return row
	}
	update(map[string]any{"description": "Weekday brief", "schedule": `{"cron":"0 9 * * 1-5","timezone":"America/New_York"}`})
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(pinned) {
		t.Fatalf("description edit moved next_fire_at to %s, want %s", got, formatWatchFireTime(pinned))
	}
	// Changing the cadence restarts the clock from now.
	update(map[string]any{"schedule": map[string]any{"cron": "30 7 * * *", "timezone": "Europe/Paris"}})
	if got, ok := parseWatchTime(storedWatchNextFire(t, db, id)); !ok || !got.After(time.Now().UTC()) || got.In(mustLocation(t, "Europe/Paris")).Format("15:04") != "07:30" {
		t.Fatalf("rescheduled next_fire_at = %v", got)
	}

	// A paused watch is skipped; resuming it starts from now instead of
	// delivering the ticks it missed.
	update(map[string]any{"schedule": map[string]any{"cron": "* * * * *"}, "status": "paused"})
	setWatchNextFire(t, db, id, pinned)
	if sweep, err := svc.sweepWatchSchedules(ctx, time.Now().UTC()); err != nil || sweep.Fired != 0 {
		t.Fatalf("paused sweep fired=%d err=%v", sweep.Fired, err)
	}
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(pinned) {
		t.Fatalf("paused watch next_fire_at advanced to %s", got)
	}
	update(map[string]any{"schedule": map[string]any{"cron": "* * * * *"}, "status": "active"})
	if got, ok := parseWatchTime(storedWatchNextFire(t, db, id)); !ok || !got.After(time.Now().UTC().Add(-time.Second)) {
		t.Fatalf("resumed next_fire_at = %v, want a future tick", got)
	}

	// Turning the schedule off makes it an ordinary watch again.
	row = update(map[string]any{"query": cursorOrdersWatchQuery("morning_brief")})
	if row["schedule"] != nil || stringFromAny(row["next_fire_at"]) != "" {
		t.Fatalf("schedule should be cleared: schedule=%v next=%v", row["schedule"], row["next_fire_at"])
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestWatchScheduleSweepInitializesMissingNextFire(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, replicas[0], ctx, "legacy_clock", map[string]any{"cron": "*/10 * * * *"})
	id := stringFromAny(row["id"])
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET next_fire_at = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, svc := range replicas {
		sweep, err := svc.sweepWatchSchedules(ctx, now)
		if err != nil || sweep.Fired != 0 {
			t.Fatalf("initializing sweep fired=%d err=%v", sweep.Fired, err)
		}
		if want := mustWatchSchedule(t, "*/10 * * * *", "UTC").next(now); !sweep.Soonest.Equal(want) {
			t.Fatalf("sweep soonest = %s, want %s", sweep.Soonest, want)
		}
	}
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(mustWatchSchedule(t, "*/10 * * * *", "UTC").next(now)) {
		t.Fatalf("initialized next_fire_at = %q", got)
	}
	if ids := watchEventIDs(t, db); len(ids) != 0 {
		t.Fatalf("initializing a clock must not fire: %v", ids)
	}
}

func TestWatchScheduleSleep(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		soonest time.Time
		want    time.Duration
	}{
		{time.Time{}, watchScheduleMaxSleep},
		{now.Add(5 * time.Second), 5 * time.Second},
		{now.Add(time.Hour), watchScheduleMaxSleep},
		{now.Add(-time.Minute), watchScheduleMinSleep},
	} {
		if got := watchScheduleSleep(tc.soonest, now); got != tc.want {
			t.Fatalf("watchScheduleSleep(%s) = %s, want %s", tc.soonest, got, tc.want)
		}
	}
}

// The documented example must go through the real GraphQL engine, not only
// the control-plane helper: schedule is an object-valued input on gj_watch.
func TestScheduleWatchGraphQLSurface(t *testing.T) {
	_, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	ctx := artifactUserCtx("graphql_schedule_owner")
	res, err := svc.gj.GraphQL(ctx, `mutation {
		gj_watch(insert: { name: "weekday_brief", schedule: { cron: "0 9 * * 1-5", timezone: "America/New_York" }, delivery_json: { kind: "inbox" } }) {
			id schedule next_fire_at status enabled
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("create schedule watch GraphQL: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("create schedule watch GraphQL errors: %+v", res.Errors)
	}
	var created struct {
		Watch struct {
			ID         string         `json:"id"`
			Schedule   map[string]any `json:"schedule"`
			NextFireAt string         `json:"next_fire_at"`
			Status     string         `json:"status"`
			Enabled    bool           `json:"enabled"`
		} `json:"gj_watch"`
	}
	if err := json.Unmarshal(res.Data, &created); err != nil {
		t.Fatalf("decode: %v\n%s", err, res.Data)
	}
	w := created.Watch
	if w.ID == "" || w.Schedule["cron"] != "0 9 * * 1-5" || w.Schedule["timezone"] != "America/New_York" || w.Status != "active" || !w.Enabled {
		t.Fatalf("created schedule watch = %+v", w)
	}
	next, ok := parseWatchTime(w.NextFireAt)
	if !ok || next.In(mustLocation(t, "America/New_York")).Format("15:04") != "09:00" {
		t.Fatalf("next_fire_at = %q", w.NextFireAt)
	}

	res, err = svc.gj.GraphQL(ctx, `query { gj_watch(where: { id: { eq: "`+w.ID+`" } }) { id schedule next_fire_at } }`, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("read schedule watch: %v %+v", err, res)
	}
	if !strings.Contains(string(res.Data), `"cron": "0 9 * * 1-5"`) && !strings.Contains(string(res.Data), `"cron":"0 9 * * 1-5"`) {
		t.Fatalf("read schedule watch = %s", res.Data)
	}
}

// A webhook schedule is an autonomous action: it needs review, the review is
// re-opened when the cadence changes, and the fired event carries an action
// hash the delivery path accepts.
func TestScheduleWatchWebhookActionReview(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	var hits atomic.Int64
	var gotBody []byte
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer hook.Close()
	svc.conf.Core.Watches.WebhookAllow = []string{hook.URL}

	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")
	mutate := func(op string, input map[string]any) map[string]any {
		t.Helper()
		input["name"] = "hourly_hook"
		input["delivery_json"] = map[string]any{"kind": "webhook", "url": hook.URL + "/brief"}
		row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{Table: watchesRootTable, Operation: op, Input: input})
		if err != nil {
			t.Fatalf("%s webhook schedule watch: %v", op, err)
		}
		return row
	}
	row := mutate("insert", map[string]any{"schedule": map[string]any{"cron": "0 * * * *"}})
	if row["action_approval"] != watchReviewPending || row["status"] != "paused" || row["enabled"] != false {
		t.Fatalf("new webhook schedule should wait for review: %+v", row)
	}
	id := stringFromAny(row["id"])

	// Approval resumes the watch from now, not from the tick computed at
	// creation time.
	stale := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	setWatchNextFire(t, db, id, stale)
	approved := approveWatchActionForTest(t, cp, ctx, row)
	if approved["status"] != "active" || approved["enabled"] != true {
		t.Fatalf("approved schedule watch did not resume: %+v", approved)
	}
	if next, ok := parseWatchTime(storedWatchNextFire(t, db, id)); !ok || !next.After(time.Now().UTC()) {
		t.Fatalf("approval kept stale next_fire_at %v", next)
	}

	// Editing something other than the cadence keeps the approval.
	row = mutate("update", map[string]any{"description": "hourly ping", "schedule": map[string]any{"cron": "0 * * * *"}})
	if row["action_approval"] != watchReviewApproved || row["status"] != "active" {
		t.Fatalf("description edit lost approval: %+v", row)
	}

	// A due tick fires and the delivery path accepts its action hash.
	due := time.Now().UTC().Truncate(time.Hour)
	setWatchNextFire(t, db, id, due)
	if sweep, err := svc.sweepWatchSchedules(ctx, due.Add(time.Second)); err != nil || sweep.Fired != 1 {
		t.Fatalf("sweep fired=%d err=%v", sweep.Fired, err)
	}
	if err := svc.processPendingWatchDeliveries(ctx); err != nil {
		t.Fatalf("deliver schedule event: %v", err)
	}
	events, err := cp.watchEventRows(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("schedule events = %+v err %v", events, err)
	}
	if events[0]["delivery_status"] != "delivered" || hits.Load() != 1 {
		t.Fatalf("webhook delivery status=%v hits=%d receipt=%v", events[0]["delivery_status"], hits.Load(), events[0]["receipt_json"])
	}
	if !strings.Contains(string(gotBody), `"kind":"schedule"`) {
		t.Fatalf("webhook body = %s", gotBody)
	}

	// Changing the cadence of an approved action re-opens review.
	row = mutate("update", map[string]any{"schedule": map[string]any{"cron": "* * * * *"}})
	if row["action_approval"] != watchReviewPending || row["status"] != "paused" || row["enabled"] != false {
		t.Fatalf("cadence change kept approval: %+v", row)
	}
}

// A replica that dies after writing the event but before moving the clock
// must not lose the tick or let another replica fire it again.
func TestWatchScheduleRecoversFromCrashAfterInsert(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 2)
	crashed, survivor := replicas[0], replicas[1]
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, crashed, ctx, "crash_tick", map[string]any{"cron": "*/15 * * * *"})
	id := stringFromAny(row["id"])
	sched := mustWatchSchedule(t, "*/15 * * * *", "UTC")
	due := time.Now().UTC().Truncate(15 * time.Minute)
	setWatchNextFire(t, db, id, due)

	// The crashed replica got exactly as far as the insert.
	stored, err := crashed.internalWatchStoreRow(ctx, id)
	if err != nil || stored == nil {
		t.Fatalf("load watch: %v", err)
	}
	def := watchScheduleDefinition(crashed, stored)
	if inserted, err := crashed.insertWatchScheduleEvent(context.Background(), &def, watchActionProposal{}, sched, due, due.Add(time.Second)); err != nil || !inserted {
		t.Fatalf("crashed replica insert=%v err=%v", inserted, err)
	}
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(due) {
		t.Fatalf("tick should still be due after the crash, next_fire_at = %s", got)
	}
	revisionBefore := sqliteRevision(t, db, "watch_events")

	now := due.Add(5 * time.Second)
	sweep, err := survivor.sweepWatchSchedules(ctx, now)
	if err != nil {
		t.Fatalf("survivor sweep: %v", err)
	}
	if sweep.Fired != 0 {
		t.Fatalf("survivor fired %d new events for a tick that already has one", sweep.Fired)
	}
	if got, want := storedWatchNextFire(t, db, id), formatWatchFireTime(sched.next(now)); got != want {
		t.Fatalf("survivor did not move the clock on: next_fire_at = %s, want %s", got, want)
	}
	if ids := watchEventIDs(t, db); len(ids) != 1 {
		t.Fatalf("events after recovery = %v, want the crashed replica's one", ids)
	}
	if got := sqliteRevision(t, db, "watch_events"); got <= revisionBefore {
		t.Fatalf("survivor did not wake delivery: watch_events revision %d -> %d", revisionBefore, got)
	}
	// The orphaned event still reaches delivery.
	if err := survivor.processPendingWatchDeliveries(ctx); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	events, err := newWatchControlPlane(survivor).watchEventRows(ctx)
	if err != nil || len(events) != 1 || events[0]["delivery_status"] != "delivered" {
		t.Fatalf("recovered event = %+v err %v", events, err)
	}
}

// If the event cannot be written, the clock must not move: the tick stays
// due and fires on a later sweep instead of being dropped.
func TestWatchScheduleFailedInsertLeavesTickDue(t *testing.T) {
	db, replicas := newScheduleWatchReplicas(t, 1)
	svc := replicas[0]
	ctx := artifactUserCtx("user_1")
	row := insertScheduleWatch(t, svc, ctx, "store_outage", map[string]any{"cron": "0 * * * *"})
	id := stringFromAny(row["id"])
	due := time.Now().UTC().Truncate(time.Hour)
	setWatchNextFire(t, db, id, due)

	if _, err := db.Exec(`ALTER TABLE "_graphjin_watch_events" RENAME TO "_graphjin_watch_events_away"`); err != nil {
		t.Fatal(err)
	}
	sweep, err := svc.sweepWatchSchedules(ctx, due.Add(time.Second))
	if err == nil || sweep.Fired != 0 {
		t.Fatalf("sweep with the event table gone fired=%d err=%v, want an error", sweep.Fired, err)
	}
	if !sweep.Soonest.IsZero() {
		t.Fatalf("a failed tick must not schedule a fast retry loop: soonest = %s", sweep.Soonest)
	}
	if got := storedWatchNextFire(t, db, id); got != formatWatchFireTime(due) {
		t.Fatalf("failed insert moved next_fire_at to %s", got)
	}

	if _, err := db.Exec(`ALTER TABLE "_graphjin_watch_events_away" RENAME TO "_graphjin_watch_events"`); err != nil {
		t.Fatal(err)
	}
	sweep, err = svc.sweepWatchSchedules(ctx, due.Add(time.Minute))
	if err != nil || sweep.Fired != 1 {
		t.Fatalf("retry sweep fired=%d err=%v", sweep.Fired, err)
	}
	if want, _ := watchScheduleEventID(id, due); !slices.Contains(watchEventIDs(t, db), want) {
		t.Fatalf("retried tick events = %v, want %s", watchEventIDs(t, db), want)
	}
}
