// Package db runs OpenNeko's SQL migrations against the operator's Postgres.
// Mirrors the bootstrap behaviour of the legacy packages/db/src/migrate.mjs:
// if a tracking table is absent but the canonical `public.organization`
// table exists, mark every embedded migration as already applied.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ConnConfig struct {
	// URL, when set, is the whole connection and the other fields are unused.
	URL      string
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

func (c ConnConfig) DSN() string {
	if c.URL != "" {
		return c.URL
	}
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	port := c.Port
	if port == 0 {
		port = 5432
	}
	user := c.User
	if user == "" {
		user = "neko"
	}
	password := c.Password
	if password == "" {
		password = "secret"
	}
	database := c.Database
	if database == "" {
		database = "neko"
	}
	ssl := c.SSLMode
	if ssl == "" {
		ssl = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, ssl)
}

// Migrator applies SQL files from an embedded fs in lexicographic order.
type Migrator struct {
	FS  fs.FS
	Dir string // directory inside FS (e.g. "migrations")
}

// Conn is the minimal pgx surface we need; lets tests stub.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	Close(ctx context.Context) error
}

// migrateAdvisoryLockKey serializes concurrent migrators. Both web's and
// worker's container entrypoints call `openneko migrate` at boot; whichever
// gets the lock first runs the SQL, the other blocks at pg_advisory_lock and
// then no-ops because every file is already recorded in schema_migrations.
// The value is an arbitrary 64-bit constant pinned for this codebase — only
// other things calling pg_advisory_lock with this same key need to coordinate
// with the migrator (currently nothing does).
const migrateAdvisoryLockKey int64 = 7283264971838383631

// Apply runs every pending *.sql migration. Returns the number applied.
// Safe to call concurrently from multiple processes against the same DB —
// callers serialize on a pg_advisory_lock so only one runs the SQL while
// the others wait, then observe an empty pending set.
func (m *Migrator) Apply(ctx context.Context, conn Conn, logf func(string, ...any)) (int, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateAdvisoryLockKey); err != nil {
		return 0, fmt.Errorf("acquire migrate lock: %w", err)
	}
	defer func() {
		// Best-effort release. Postgres also drops session-level advisory locks
		// when the connection closes, so a missed unlock here is recoverable
		// by the caller closing the conn (which start.go does).
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateAdvisoryLockKey)
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text primary key, applied_at timestamptz not null default now())`); err != nil {
		return 0, err
	}
	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return 0, err
	}
	files, err := m.listFiles()
	if err != nil {
		return 0, err
	}

	if len(applied) == 0 {
		var existsOID *uint32
		if err := conn.QueryRow(ctx, `SELECT to_regclass('public.organization')::oid`).Scan(&existsOID); err != nil {
			// "to_regclass" returns null when missing; pgx treats nullable
			// scan-target as a pointer. A null result lands as nil pointer
			// and is not an error.
			if !errors.Is(err, pgx.ErrNoRows) {
				return 0, err
			}
		}
		if existsOID != nil {
			logf("[migrate] existing schema detected without tracking — seeding schema_migrations as fully applied")
			for _, f := range files {
				if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1) ON CONFLICT DO NOTHING`, f); err != nil {
					return 0, err
				}
				applied[f] = true
			}
		}
	}

	ran := 0
	for _, f := range files {
		if applied[f] {
			continue
		}
		raw, err := fs.ReadFile(m.FS, m.Dir+"/"+f)
		if err != nil {
			return ran, err
		}
		logf("[migrate] applying %s", f)
		if err := applyOne(ctx, conn, f, string(raw)); err != nil {
			return ran, fmt.Errorf("[migrate] FAILED on %s: %w", f, err)
		}
		applied[f] = true
		ran++
	}
	logf("[migrate] done — %d new, %d already applied", ran, len(files)-ran)
	return ran, nil
}

func (m *Migrator) listFiles() ([]string, error) {
	entries, err := fs.ReadDir(m.FS, m.Dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

func loadApplied(ctx context.Context, conn Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// shouldRunInTransaction mirrors packages/db/src/migrate-helpers.mjs: files
// suffixed `_no_tx.sql` opt out of the wrapping transaction, needed for
// statements like REINDEX DATABASE / CREATE INDEX CONCURRENTLY / VACUUM that
// Postgres rejects inside a transaction block.
func shouldRunInTransaction(filename string) bool {
	if filename == "" {
		return true
	}
	return !strings.HasSuffix(filename, "_no_tx.sql")
}

func applyOne(ctx context.Context, conn Conn, name, sql string) error {
	if !shouldRunInTransaction(name) {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1) ON CONFLICT DO NOTHING`, name); err != nil {
			return err
		}
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1) ON CONFLICT DO NOTHING`, name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
