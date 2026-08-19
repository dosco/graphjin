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
	if state == nil || state.ShiftDays == 0 || len(dbs) == 0 {
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
		tables, err := demoTemporalColumns(name)
		if err != nil {
			state.Status.Emit(name, "skipped", fmt.Sprintf("date refresh needs schema DDL: %s", err))
			continue
		}
		n, err := shiftDemoConnDates(ctx, dbs[name], dbType, tables, state.ShiftDays)
		if err != nil {
			state.Status.Emit(name, "failed", fmt.Sprintf("date refresh failed (%s); delete %s to reseed", err, state.Dir))
			continue
		}
		if n > 0 {
			state.Status.Emit(name, "refreshed", fmt.Sprintf("%d date column(s) moved forward %d day(s)", n, state.ShiftDays))
		}
	}
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
func shiftDuckDBFileDates(ctx context.Context, path string, tables map[string][]core.TemporalColumn, days int) (int, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return 0, fmt.Errorf("duckdb driver unavailable: %w", err)
	}
	defer db.Close() //nolint:errcheck
	return shiftDemoConnDates(ctx, db, "duckdb", tables, days)
}
