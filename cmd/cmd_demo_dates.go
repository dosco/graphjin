package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// Demo seed data is date-anchored to the day the seed scripts ran ("what
// should we roast today?" needs roasts scheduled today). The manifest records
// that day as data_anchor; when a persisted demo state is reused later, every
// date and timestamp column is shifted forward by the elapsed whole days so
// the demo keeps answering "today" and "this week" questions. Shifting whole
// days preserves times of day and the relative spacing between rows.

const demoDataAnchorLayout = "2006-01-02"

// demoDataShiftDays reports how many whole days the demo data lags behind
// now (UTC). Manifests written before data anchors existed fall back to the
// manifest creation date, which is when their seed scripts ran.
func demoDataShiftDays(m demoManifest, now time.Time) int {
	anchor := strings.TrimSpace(m.DataAnchor)
	if anchor == "" && strings.TrimSpace(m.CreatedAt) != "" {
		if created, err := time.Parse(time.RFC3339, m.CreatedAt); err == nil {
			anchor = created.UTC().Format(demoDataAnchorLayout)
		}
	}
	if anchor == "" {
		return 0
	}
	anchorDay, err := time.Parse(demoDataAnchorLayout, anchor)
	if err != nil {
		return 0
	}
	today, err := time.Parse(demoDataAnchorLayout, now.UTC().Format(demoDataAnchorLayout))
	if err != nil {
		return 0
	}
	days := int(today.Sub(anchorDay).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// demoAnchorDelta reports the whole-day move that takes demo data anchored at
// `from` to the anchor `to`. Negative means rewinding data a later boot already
// shifted forward, which is how a run interrupted before a UTC midnight is
// resumed the next day against the data it was actually graded on.
func demoAnchorDelta(from, to string) (int, error) {
	fromDay, err := time.Parse(demoDataAnchorLayout, strings.TrimSpace(from))
	if err != nil {
		return 0, err
	}
	toDay, err := time.Parse(demoDataAnchorLayout, strings.TrimSpace(to))
	if err != nil {
		return 0, err
	}
	return int(toDay.Sub(fromDay).Hours() / 24), nil
}

// demoTemporalColumns returns the date/timestamp columns per table for a demo
// source, parsed from the project's GraphJin schema DDL. Sources without a
// per-source DDL file fall back to the project-level db.ddl.
func demoTemporalColumns(source string) (map[string][]core.TemporalColumn, error) {
	path := filepath.Join(cpath, filepath.FromSlash(core.SourceSchemaDDLPath(source)))
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, _, err = readProjectSchemaDDL()
	}
	if err != nil {
		return nil, err
	}
	return core.SchemaDDLTemporalColumns(data)
}

// shiftDemoDates moves seeded dates forward on reused demo state so
// "today"-anchored demo questions keep returning data. Warehouse simulator
// sources are shifted earlier, directly in their DuckDB files, before the
// simulator locks them. Failures never block startup: each failed source is
// reported with the reset path and skipped.
func shiftDemoDates(ctx context.Context, dbs map[string]*sql.DB, state *demoState) {
	if state == nil || !state.shiftPending() || len(dbs) == 0 {
		return
	}
	names := make([]string, 0, len(dbs))
	for name := range dbs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		dbType := strings.ToLower(strings.TrimSpace(demoSourceDBType(name)))
		switch dbType {
		case "postgres", "postgresql", "mysql", "mariadb", "sqlite", "sqlite3":
		case "bigquery", "snowflake":
			continue // shifted in the DuckDB file before the simulator opened it
		default:
			state.Status.Emit(name, "skipped", fmt.Sprintf("date refresh is not supported for %s", dbType))
			continue
		}
		days := state.shiftFor(name)
		if days == 0 {
			continue
		}
		tables, err := demoTemporalColumns(name)
		if err != nil {
			state.Status.Emit(name, "skipped", fmt.Sprintf("date refresh needs schema DDL: %s", err))
			continue
		}
		tables, err = presentTemporalColumns(ctx, dbs[name], dbType, tables, name, state)
		if err != nil {
			state.shiftFailed(name, err)
			continue
		}
		n, err := shiftDemoConnDates(ctx, dbs[name], dbType, tables, days)
		if err != nil {
			state.shiftFailed(name, err)
			continue
		}
		if n > 0 {
			state.Status.Emit(name, "refreshed", fmt.Sprintf("%d date column(s) moved %+d day(s)", n, days))
		}
	}
}

// presentTemporalColumns drops the DDL's date columns that the database does
// not have.
//
// Reused demo state keeps the schema it was provisioned with, while the
// project's DDL moves on. Shifting runs in one transaction, so a single column
// the DDL gained since then used to roll back every column, every boot — and
// the manifest still advanced its anchor, so the demo silently froze on the
// day it was seeded. Each column is probed with the shape the shift runs, a
// zero-row self-assignment, which is the same statement on every supported
// dialect. Not a select: SQLite reads a double-quoted name that matches no
// column as a string literal, so selecting a missing column succeeds, while an
// assignment target must be a real column. A failed ping is a failure,
// not a database with no columns: reading it as the latter would shift
// nothing, report success, and advance the anchor over unshifted data.
func presentTemporalColumns(ctx context.Context, conn *sql.DB, dbType string, tables map[string][]core.TemporalColumn, source string, state *demoState) (map[string][]core.TemporalColumn, error) {
	if conn == nil {
		return nil, fmt.Errorf("no open database connection")
	}
	if err := conn.PingContext(ctx); err != nil {
		return nil, err
	}
	present := make(map[string][]core.TemporalColumn, len(tables))
	var missing []string
	for table, cols := range tables {
		for _, col := range cols {
			c := seedQuoteIdent(dbType, col.Name)
			probe := fmt.Sprintf("UPDATE %s SET %s = %s WHERE 1 = 0", seedQuoteIdent(dbType, table), c, c)
			if _, err := conn.ExecContext(ctx, probe); err != nil {
				missing = append(missing, table+"."+col.Name)
				continue
			}
			present[table] = append(present[table], col)
		}
	}
	if len(missing) != 0 && state != nil {
		sort.Strings(missing)
		state.Status.Emit(source, "warning", fmt.Sprintf(
			"the schema DDL has %d date column(s) this demo database lacks (%s); they are not shifted. Delete %s to reseed with the current schema",
			len(missing), strings.Join(missing, ", "), state.Dir))
	}
	return present, nil
}

// demoSourceDBType resolves the configured database type for a demo source,
// covering both multi-database and single-database modes.
func demoSourceDBType(name string) string {
	if dbConf, ok := conf.Databases[name]; ok && dbConf.Type != "" {
		return dbConf.Type
	}
	return conf.DB.Type
}

// shiftDemoConnDates shifts every date/timestamp column in tables forward by
// days on an open connection, in one transaction so a failure leaves the
// source untouched. It returns the number of columns shifted.
func shiftDemoConnDates(ctx context.Context, conn *sql.DB, dbType string, tables map[string][]core.TemporalColumn, days int) (int, error) {
	if conn == nil {
		return 0, fmt.Errorf("no open database connection")
	}
	stmts := shiftDateSQL(dbType, tables, days)
	if len(stmts) == 0 {
		return 0, nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback() //nolint:errcheck
			return 0, fmt.Errorf("%w (sql: %s)", err, sqlSummary(stmt))
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(stmts), nil
}

func sqlSummary(stmt string) string {
	stmt = strings.Join(strings.Fields(stmt), " ")
	if len(stmt) > 120 {
		stmt = stmt[:120] + "…"
	}
	return stmt
}

// shiftDateSQL renders one UPDATE per date/timestamp column, in stable table
// then column order.
func shiftDateSQL(dbType string, tables map[string][]core.TemporalColumn, days int) []string {
	names := make([]string, 0, len(tables))
	for table := range tables {
		names = append(names, table)
	}
	sort.Strings(names)

	var stmts []string
	for _, table := range names {
		if demoInternalTable(table) {
			continue
		}
		for _, col := range tables[table] {
			if stmt := shiftColumnSQL(dbType, table, col, days); stmt != "" {
				stmts = append(stmts, stmt)
			}
		}
	}
	return stmts
}

// demoInternalTable reports GraphJin-internal store tables (artifact, watch,
// metadata, and simulator session tables) whose timestamps record real events
// and must not be shifted.
func demoInternalTable(table string) bool {
	table = strings.ToLower(strings.TrimSpace(table))
	return strings.HasPrefix(table, "gj_") ||
		strings.HasPrefix(table, "_gj") ||
		strings.HasPrefix(table, "_graphjin") ||
		strings.HasPrefix(table, "sqlite_")
}

func shiftColumnSQL(dbType, table string, col core.TemporalColumn, days int) string {
	t := seedQuoteIdent(dbType, table)
	c := seedQuoteIdent(dbType, col.Name)
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "postgresql":
		if col.DateOnly {
			return fmt.Sprintf("UPDATE %s SET %s = %s + %d", t, c, c, days)
		}
		return fmt.Sprintf("UPDATE %s SET %s = %s + INTERVAL '%d days'", t, c, c, days)
	case "mysql", "mariadb":
		return fmt.Sprintf("UPDATE %s SET %s = DATE_ADD(%s, INTERVAL %d DAY)", t, c, c, days)
	case "sqlite", "sqlite3":
		return shiftSQLiteColumnSQL(t, c, days)
	case "duckdb":
		// Warehouse simulator sources (bigquery, snowflake) are shifted
		// directly in their DuckDB file, so plain DuckDB SQL applies.
		if col.DateOnly {
			return fmt.Sprintf("UPDATE %s SET %s = %s + %d", t, c, c, days)
		}
		return fmt.Sprintf("UPDATE %s SET %s = %s + INTERVAL (%d) DAY", t, c, c, days)
	}
	return ""
}

// shiftSQLiteColumnSQL shifts SQLite temporal values, which are stored as
// TEXT in whatever format the seed wrote. Each recognized layout is shifted
// and re-rendered in place; values that don't start with a calendar date are
// left alone.
func shiftSQLiteColumnSQL(t, c string, days int) string {
	// The modifier carries its own sign. Writing '+%d days' with a negative
	// count produces '+-1 days', which SQLite evaluates to NULL rather than
	// rejecting — every temporal value in the column would be silently erased.
	modifier := fmt.Sprintf("%+d days", days)
	return fmt.Sprintf(`UPDATE %[1]s SET %[2]s = CASE
WHEN length(%[2]s) = 10 THEN date(%[2]s, '%[3]s')
WHEN instr(%[2]s, 'T') > 0 AND substr(%[2]s, -1) = 'Z' THEN strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', %[2]s, '%[3]s')
WHEN instr(%[2]s, 'T') > 0 THEN strftime('%%Y-%%m-%%dT%%H:%%M:%%S', %[2]s, '%[3]s')
ELSE strftime('%%Y-%%m-%%d %%H:%%M:%%S', %[2]s, '%[3]s')
END
WHERE %[2]s IS NOT NULL AND %[2]s GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]*'`, t, c, modifier)
}

// shiftDuckDBFileDates shifts a warehouse simulator's DuckDB file in place.
// It must run before hostedemu opens the file: DuckDB allows a single
// read-write handle per file and per process. The duckdb driver is registered
// by the hostedemu package in CGO builds; without it sql.Open fails and the
// caller reports the source as skipped.
func shiftDuckDBFileDates(ctx context.Context, path string, tables map[string][]core.TemporalColumn, days int, source string, state *demoState) (int, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return 0, fmt.Errorf("duckdb driver unavailable: %w", err)
	}
	defer db.Close() //nolint:errcheck
	if tables, err = presentTemporalColumns(ctx, db, "duckdb", tables, source, state); err != nil {
		return 0, err
	}
	return shiftDemoConnDates(ctx, db, "duckdb", tables, days)
}

// shiftPending reports whether any source has dates to move this boot.
func (s *demoState) shiftPending() bool {
	if s.ShiftDays != 0 {
		return true
	}
	for _, days := range s.Owed {
		if days != 0 {
			return true
		}
	}
	return false
}

// shiftFor is the whole-day move one source needs: the boot's shift plus
// whatever an earlier failed boot left it owing.
func (s *demoState) shiftFor(source string) int {
	return s.ShiftDays + s.Owed[source]
}

// shiftFailed reports a source whose dates did not move and remembers the
// anchor its data still sits on, so the manifest does not claim the source
// is current and the next boot shifts it by the full lag. One manifest
// anchor cannot say this for a single source: holding it back would shift
// every source that did succeed a second time.
func (s *demoState) shiftFailed(source string, err error) {
	s.Status.Emit(source, "failed", fmt.Sprintf("date refresh failed (%s); the next start retries it, or delete %s to reseed", err, s.Dir))
	if s.Lagging == nil {
		s.Lagging = map[string]string{}
	}
	s.Lagging[source] = s.sourceAnchor(source)
}

// sourceAnchor is the day a source's data is anchored to before this boot's
// shift: its own recorded lag, else the manifest's anchor, else (for manifests
// older than anchors, and for a fresh provision) the creation day.
func (s *demoState) sourceAnchor(source string) string {
	if anchor := strings.TrimSpace(s.Manifest.Sources[source].DataAnchor); anchor != "" {
		return anchor
	}
	if anchor := strings.TrimSpace(s.Manifest.DataAnchor); anchor != "" {
		return anchor
	}
	if created, err := time.Parse(time.RFC3339, s.Manifest.CreatedAt); err == nil {
		return created.UTC().Format(demoDataAnchorLayout)
	}
	return time.Now().UTC().Format(demoDataAnchorLayout)
}

// demoOwedDays reads each source's recorded lag as days owed beyond the
// manifest's own anchor.
func demoOwedDays(m demoManifest) map[string]int {
	base := strings.TrimSpace(m.DataAnchor)
	if base == "" {
		return nil
	}
	var owed map[string]int
	for name, item := range m.Sources {
		if strings.TrimSpace(item.DataAnchor) == "" {
			continue
		}
		if days, err := demoAnchorDelta(item.DataAnchor, base); err == nil && days != 0 {
			if owed == nil {
				owed = map[string]int{}
			}
			owed[name] = days
		}
	}
	return owed
}
