package main

import (
	"bytes"
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

// reusedDemo writes a reused demo state anchored `lag` days ago over a sqlite
// source "app" that has only the accounts table, while the project DDL also
// declares a payments table and an accounts column the table lacks. That is the shape of a demo provisioned before
// its DDL gained a table: migrations do not run on reuse, so the table never
// arrives.
func reusedDemo(t *testing.T, lag int) (*sql.DB, string) {
	t.Helper()
	oldCpath, oldConf := cpath, conf
	t.Cleanup(func() { cpath, conf = oldCpath, oldConf })
	cpath = t.TempDir()
	conf = &serv.Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{"app": {Type: "sqlite"}}}}

	ddl := filepath.Join(cpath, filepath.FromSlash(core.SourceSchemaDDLPath("app")))
	if err := os.MkdirAll(filepath.Dir(ddl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ddl, []byte(`type accounts {
  id: Bigint! @id
  renewal_date: Date!
  resolved_at: TimestampWithTimeZone
}

type payments {
  id: Bigint! @id
  recorded_at: TimestampWithTimeZone!
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	registerSQLiteRegexp()
	db, err := sql.Open("sqlite", filepath.Join(cpath, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	if _, err := db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, renewal_date TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (id, renewal_date) VALUES (1, '2026-08-10')`); err != nil {
		t.Fatal(err)
	}

	writeTestManifest(t, demoManifest{
		Version:    demoManifestVersion,
		CreatedAt:  time.Now().UTC().AddDate(0, 0, -lag).Format(time.RFC3339),
		DataAnchor: time.Now().UTC().AddDate(0, 0, -lag).Format(demoDataAnchorLayout),
		Sources:    map[string]demoManifestItem{"app": {Type: "sqlite", Status: "verified"}},
	})
	return db, filepath.Join(cpath, "demo")
}

func writeTestManifest(t *testing.T, m demoManifest) {
	t.Helper()
	dir := filepath.Join(cpath, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestManifest(t *testing.T) demoManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cpath, "demo", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m demoManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func renewal(t *testing.T, db *sql.DB) string {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT renewal_date FROM accounts WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// A date column the DDL gained after the demo was provisioned used to fail the
// one shift transaction on every boot, while the manifest still advanced its
// anchor: the saas-ops demo sat on its seed day for months, quietly answering
// "upcoming renewal" questions from the past. The columns the database has
// must move, and the ones it lacks must be named.
func TestDemoShiftMovesThePresentColumnsWhenTheDDLIsAhead(t *testing.T) {
	db, _ := reusedDemo(t, 4)
	var out bytes.Buffer
	state, err := initDemoState(demoStatus{out: &out})
	if err != nil {
		t.Fatal(err)
	}
	shiftDemoDates(context.Background(), map[string]*sql.DB{"app": db}, state)

	if got := renewal(t, db); got != "2026-08-14" {
		t.Fatalf("renewal_date = %q, want it moved 4 days to 2026-08-14\n%s", got, out.String())
	}
	// A missing column on a table that exists is the case a select probe
	// misses on SQLite, which reads the unmatched quoted name as a literal.
	if !strings.Contains(out.String(), "payments.recorded_at") || !strings.Contains(out.String(), "accounts.resolved_at") ||
		!strings.Contains(out.String(), "reseed") {
		t.Fatalf("the missing column must be named with the way out:\n%s", out.String())
	}
	if len(state.Lagging) != 0 {
		t.Fatalf("a shift that moved every present column is not lagging: %v", state.Lagging)
	}
}

// When a source's shift does fail, the manifest must not claim its data is
// current. The lag is recorded on that source and made up by the next boot,
// and cleared once it is.
func TestDemoShiftFailureCarriesTheLagToTheNextBoot(t *testing.T) {
	db, _ := reusedDemo(t, 4)
	staleAnchor := time.Now().UTC().AddDate(0, 0, -4).Format(demoDataAnchorLayout)

	state, err := initDemoState(demoStatus{})
	if err != nil {
		t.Fatal(err)
	}
	broken, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "missing-dir", "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	shiftDemoDates(context.Background(), map[string]*sql.DB{"app": broken}, state)
	if err := state.writeManifest(map[string]*sql.DB{"app": db}); err != nil {
		t.Fatal(err)
	}
	m := readTestManifest(t)
	if m.DataAnchor != time.Now().UTC().Format(demoDataAnchorLayout) || m.Sources["app"].DataAnchor != staleAnchor {
		t.Fatalf("manifest = %+v, want today's anchor with app held at %s", m, staleAnchor)
	}

	// Next boot, same day: no new days elapsed, but app still owes four.
	state, err = initDemoState(demoStatus{})
	if err != nil {
		t.Fatal(err)
	}
	if state.ShiftDays != 0 || state.shiftFor("app") != 4 {
		t.Fatalf("shift = %d, app = %d; want 0 and the owed 4", state.ShiftDays, state.shiftFor("app"))
	}
	shiftDemoDates(context.Background(), map[string]*sql.DB{"app": db}, state)
	if got := renewal(t, db); got != "2026-08-14" {
		t.Fatalf("renewal_date = %q, want the owed 4 days made up", got)
	}
	if err := state.writeManifest(map[string]*sql.DB{"app": db}); err != nil {
		t.Fatal(err)
	}
	if lag := readTestManifest(t).Sources["app"].DataAnchor; lag != "" {
		t.Fatalf("a source that caught up must drop its lag, got %q", lag)
	}
}
