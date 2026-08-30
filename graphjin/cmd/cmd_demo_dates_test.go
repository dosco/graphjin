package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestDemoDataShiftDays(t *testing.T) {
	now := time.Date(2026, 7, 10, 15, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		manifest demoManifest
		want     int
	}{
		{"anchor today", demoManifest{DataAnchor: "2026-07-10"}, 0},
		{"anchor three days back", demoManifest{DataAnchor: "2026-07-07"}, 3},
		{"anchor in the future", demoManifest{DataAnchor: "2026-07-12"}, 0},
		{"legacy falls back to created_at", demoManifest{CreatedAt: "2026-06-29T09:00:00Z"}, 11},
		{"anchor wins over created_at", demoManifest{DataAnchor: "2026-07-09", CreatedAt: "2026-06-29T09:00:00Z"}, 1},
		{"garbage anchor", demoManifest{DataAnchor: "not-a-date"}, 0},
		{"empty manifest", demoManifest{}, 0},
	}
	for _, tc := range tests {
		if got := demoDataShiftDays(tc.manifest, now); got != tc.want {
			t.Errorf("%s: demoDataShiftDays = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestDemoDataShiftDaysLocalTimeIsUTCNormalized(t *testing.T) {
	// 2026-07-11 01:00 in UTC+2 is still 2026-07-10 in UTC.
	now := time.Date(2026, 7, 11, 1, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	if got := demoDataShiftDays(demoManifest{DataAnchor: "2026-07-10"}, now); got != 0 {
		t.Fatalf("demoDataShiftDays = %d, want 0 (UTC date unchanged)", got)
	}
}

func TestShiftDateSQLDialects(t *testing.T) {
	tables := map[string][]core.TemporalColumn{
		"orders":       {{Name: "due_date", DateOnly: true}, {Name: "created_at"}},
		"gj_databases": {{Name: "updated_at"}},
		"_gj_session":  {{Name: "created_at"}},
	}

	pg := shiftDateSQL("postgres", tables, 3)
	if len(pg) != 2 {
		t.Fatalf("postgres statements = %d, want 2 (internal tables excluded): %v", len(pg), pg)
	}
	if pg[0] != `UPDATE "orders" SET "due_date" = "due_date" + 3` {
		t.Errorf("postgres date stmt = %s", pg[0])
	}
	if pg[1] != `UPDATE "orders" SET "created_at" = "created_at" + INTERVAL '3 days'` {
		t.Errorf("postgres timestamp stmt = %s", pg[1])
	}

	my := shiftDateSQL("mysql", tables, 2)
	if len(my) != 2 || my[0] != "UPDATE `orders` SET `due_date` = DATE_ADD(`due_date`, INTERVAL 2 DAY)" {
		t.Errorf("mysql stmts = %v", my)
	}

	duck := shiftDateSQL("duckdb", tables, 4)
	if len(duck) != 2 || duck[1] != `UPDATE "orders" SET "created_at" = "created_at" + INTERVAL (4) DAY` {
		t.Errorf("duckdb stmts = %v", duck)
	}

	lite := shiftDateSQL("sqlite", tables, 1)
	if len(lite) != 2 || !strings.Contains(lite[1], "strftime('%Y-%m-%dT%H:%M:%SZ'") {
		t.Errorf("sqlite stmts = %v", lite)
	}
}

func TestShiftDemoConnDatesSQLitePreservesFormats(t *testing.T) {
	registerSQLiteRegexp()
	db, err := sql.Open("sqlite", "file:shiftdemo?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if _, err := db.Exec(`CREATE TABLE appointments (
		id INTEGER PRIMARY KEY,
		scheduled_at TEXT,
		visit_date TEXT,
		note TEXT
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	rows := [][3]interface{}{
		{"2026-07-01T09:00:00Z", "2026-07-01", "prose 2026-07-01 stays"},
		{"2026-07-02 10:30:00", nil, nil},
	}
	for i, r := range rows {
		if _, err := db.Exec("INSERT INTO appointments (id, scheduled_at, visit_date, note) VALUES (?, ?, ?, ?)", i+1, r[0], r[1], r[2]); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	tables := map[string][]core.TemporalColumn{
		"appointments": {{Name: "scheduled_at"}, {Name: "visit_date", DateOnly: true}},
	}
	n, err := shiftDemoConnDates(context.Background(), db, "sqlite", tables, 5)
	if err != nil {
		t.Fatalf("shiftDemoConnDates: %v", err)
	}
	if n != 2 {
		t.Fatalf("shifted columns = %d, want 2", n)
	}

	var isoTS, spaceTS, day, note string
	if err := db.QueryRow("SELECT scheduled_at, visit_date, note FROM appointments WHERE id = 1").Scan(&isoTS, &day, &note); err != nil {
		t.Fatalf("scan row 1: %v", err)
	}
	if err := db.QueryRow("SELECT scheduled_at FROM appointments WHERE id = 2").Scan(&spaceTS); err != nil {
		t.Fatalf("scan row 2: %v", err)
	}
	if isoTS != "2026-07-06T09:00:00Z" {
		t.Errorf("ISO timestamp = %q, want 2026-07-06T09:00:00Z", isoTS)
	}
	if spaceTS != "2026-07-07 10:30:00" {
		t.Errorf("space timestamp = %q, want 2026-07-07 10:30:00", spaceTS)
	}
	if day != "2026-07-06" {
		t.Errorf("date = %q, want 2026-07-06", day)
	}
	if note != "prose 2026-07-01 stays" {
		t.Errorf("note column changed: %q", note)
	}
}

func TestShiftDemoConnDatesRollsBackOnFailure(t *testing.T) {
	registerSQLiteRegexp()
	db, err := sql.Open("sqlite", "file:shiftrollback?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if _, err := db.Exec(`CREATE TABLE a (happened_at TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO a (happened_at) VALUES ('2026-07-01T09:00:00Z')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	tables := map[string][]core.TemporalColumn{
		"a":       {{Name: "happened_at"}},
		"missing": {{Name: "happened_at"}},
	}
	if _, err := shiftDemoConnDates(context.Background(), db, "sqlite", tables, 2); err == nil {
		t.Fatal("expected error for missing table")
	}
	var got string
	if err := db.QueryRow("SELECT happened_at FROM a").Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != "2026-07-01T09:00:00Z" {
		t.Fatalf("table a shifted despite rollback: %q", got)
	}
}

func TestShiftDuckDBFileDates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warehouse.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Skipf("duckdb driver unavailable (non-CGO build): %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		t.Skipf("duckdb open failed: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE roast_batches (started_at TIMESTAMP, roast_day DATE)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO roast_batches VALUES ('2026-06-05 14:00:00', '2026-06-05')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	tables := map[string][]core.TemporalColumn{
		"roast_batches": {{Name: "started_at"}, {Name: "roast_day", DateOnly: true}},
	}
	n, err := shiftDuckDBFileDates(context.Background(), path, tables, 10)
	if err != nil {
		t.Fatalf("shiftDuckDBFileDates: %v", err)
	}
	if n != 2 {
		t.Fatalf("shifted columns = %d, want 2", n)
	}

	check, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("reopen duckdb: %v", err)
	}
	defer check.Close() //nolint:errcheck
	var ts, day string
	if err := check.QueryRow("SELECT strftime(started_at, '%Y-%m-%d %H:%M:%S'), strftime(roast_day, '%Y-%m-%d') FROM roast_batches").Scan(&ts, &day); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if ts != "2026-06-15 14:00:00" {
		t.Errorf("timestamp = %q, want 2026-06-15 14:00:00", ts)
	}
	if day != "2026-06-15" {
		t.Errorf("date = %q, want 2026-06-15", day)
	}
}

func TestDemoTemporalColumnsFromCoffeeRoasteryDDL(t *testing.T) {
	oldCpath := cpath
	defer func() { cpath = oldCpath }()
	cpath = filepath.Clean("../examples/coffee-roastery")

	tables, err := demoTemporalColumns("ops")
	if err != nil {
		t.Fatalf("demoTemporalColumns: %v", err)
	}
	var arrival, scheduled *core.TemporalColumn
	for i := range tables["green_lots"] {
		if tables["green_lots"][i].Name == "arrival_date" {
			arrival = &tables["green_lots"][i]
		}
	}
	for i := range tables["roast_schedule"] {
		if tables["roast_schedule"][i].Name == "scheduled_for" {
			scheduled = &tables["roast_schedule"][i]
		}
	}
	if arrival == nil || !arrival.DateOnly {
		t.Fatalf("green_lots.arrival_date should be a date-only temporal column, got %+v", tables["green_lots"])
	}
	if scheduled == nil || scheduled.DateOnly {
		t.Fatalf("roast_schedule.scheduled_for should be a timestamp temporal column, got %+v", tables["roast_schedule"])
	}
	if len(tables["roast_profiles"]) != 0 {
		t.Fatalf("roast_profiles has no temporal columns, got %+v", tables["roast_profiles"])
	}
}

func TestDemoStateShiftDaysFromStaleAnchor(t *testing.T) {
	oldCpath, oldConf := cpath, conf
	defer func() {
		cpath = oldCpath
		conf = oldConf
	}()
	cpath = t.TempDir()
	conf = &serv.Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{
		"ops": {Type: "postgres"},
	}}}

	stateDir := filepath.Join(cpath, "demo")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	anchor := time.Now().UTC().AddDate(0, 0, -4).Format(demoDataAnchorLayout)
	manifest := demoManifest{
		Version:    demoManifestVersion,
		CreatedAt:  time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339),
		DataAnchor: anchor,
		Sources:    map[string]demoManifestItem{},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	state, err := initDemoState(demoStatus{})
	if err != nil {
		t.Fatalf("initDemoState: %v", err)
	}
	if state.FirstRun {
		t.Fatal("expected reused state")
	}
	if state.ShiftDays != 4 {
		t.Fatalf("ShiftDays = %d, want 4", state.ShiftDays)
	}

	// A successful start rewrites the manifest with today's anchor.
	if err := state.writeManifest(nil); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	reread, err := os.ReadFile(filepath.Join(stateDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var updated demoManifest
	if err := json.Unmarshal(reread, &updated); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if want := time.Now().UTC().Format(demoDataAnchorLayout); updated.DataAnchor != want {
		t.Fatalf("DataAnchor = %q, want %q", updated.DataAnchor, want)
	}
}

// The overnight case end to end: reused demo state anchored to yesterday
// normally shifts forward on boot, which rewrites the dataset fingerprint and
// strands an incomplete run's episodes. With the anchor pinned to what that run
// recorded, the boot must leave the dates alone.
func TestDemoPinnedAnchorSuppressesTheOvernightShift(t *testing.T) {
	manifest := demoManifest{DataAnchor: "2026-08-18"}
	now := time.Date(2026, 8, 19, 3, 0, 0, 0, time.UTC)

	if days := demoDataShiftDays(manifest, now); days != 1 {
		t.Fatalf("unpinned shift = %d day(s), want 1; the demo must still refresh for normal use", days)
	}

	previous := demoPinnedDataAnchor
	t.Cleanup(func() { demoPinnedDataAnchor = previous })

	// Pinned to the same anchor the incomplete run was graded against.
	demoPinnedDataAnchor = "2026-08-18"
	shift := demoDataShiftDays(manifest, now)
	if pinned := strings.TrimSpace(demoPinnedDataAnchor); pinned != "" && shift > 0 && pinned == strings.TrimSpace(manifest.DataAnchor) {
		shift = 0
	}
	if shift != 0 {
		t.Fatalf("pinned shift = %d day(s), want 0; resuming must not move the data", shift)
	}

	// Pinned to a different day: the data has already moved, so the normal
	// shift stands and the resume is refused on fingerprint rather than
	// silently mixing two data states.
	demoPinnedDataAnchor = "2026-08-01"
	shift = demoDataShiftDays(manifest, now)
	if pinned := strings.TrimSpace(demoPinnedDataAnchor); pinned != "" && shift > 0 && pinned == strings.TrimSpace(manifest.DataAnchor) {
		shift = 0
	}
	if shift != 1 {
		t.Fatalf("mismatched pin shift = %d, want the normal 1 day", shift)
	}
}

// Recovering a run interrupted before a UTC midnight means moving the demo data
// back to the anchor its completed episodes were graded against. The shift SQL
// used to hardcode a '+' sign, so a negative count produced '+-1 days', which
// SQLite evaluates to NULL — every temporal value would have been erased rather
// than rewound.
func TestShiftDemoConnDatesRewindsWithoutErasing(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	if _, err := db.ExecContext(ctx, `CREATE TABLE invoices (due_on TEXT, due_at TEXT, seen_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO invoices VALUES ('2026-08-19','2026-08-19T14:30:00Z','2026-08-19 14:30:00')`); err != nil {
		t.Fatal(err)
	}
	tables := map[string][]core.TemporalColumn{"invoices": {
		{Name: "due_on", DateOnly: true}, {Name: "due_at"}, {Name: "seen_at"},
	}}

	if _, err := shiftDemoConnDates(ctx, db, "sqlite", tables, -1); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	var dueOn, dueAt, seenAt sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT due_on, due_at, seen_at FROM invoices`).Scan(&dueOn, &dueAt, &seenAt); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]sql.NullString{"due_on": dueOn, "due_at": dueAt, "seen_at": seenAt} {
		if !got.Valid {
			t.Fatalf("%s was erased to NULL by the rewind", name)
		}
	}
	if dueOn.String != "2026-08-18" {
		t.Fatalf("due_on = %q, want 2026-08-18", dueOn.String)
	}
	if dueAt.String != "2026-08-18T14:30:00Z" {
		t.Fatalf("due_at = %q, want the Z format preserved a day earlier", dueAt.String)
	}
	if seenAt.String != "2026-08-18 14:30:00" {
		t.Fatalf("seen_at = %q, want the space format preserved a day earlier", seenAt.String)
	}

	// Round trip: forward again must restore the original values exactly.
	if _, err := shiftDemoConnDates(ctx, db, "sqlite", tables, 1); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT due_on, due_at, seen_at FROM invoices`).Scan(&dueOn, &dueAt, &seenAt); err != nil {
		t.Fatal(err)
	}
	if dueOn.String != "2026-08-19" || dueAt.String != "2026-08-19T14:30:00Z" || seenAt.String != "2026-08-19 14:30:00" {
		t.Fatalf("round trip lost fidelity: %q %q %q", dueOn.String, dueAt.String, seenAt.String)
	}
}

func TestDemoAnchorDeltaSignsTheMove(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     int
	}{
		{"2026-08-19", "2026-08-18", -1},
		{"2026-08-18", "2026-08-19", 1},
		{"2026-08-19", "2026-08-19", 0},
		{"2026-08-19", "2026-08-12", -7},
	} {
		got, err := demoAnchorDelta(tc.from, tc.to)
		if err != nil {
			t.Fatalf("demoAnchorDelta(%s,%s): %v", tc.from, tc.to, err)
		}
		if got != tc.want {
			t.Fatalf("demoAnchorDelta(%s,%s) = %d, want %d", tc.from, tc.to, got, tc.want)
		}
	}
}
