package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// StorageContractVersion identifies the on-disk PostgreSQL ABI shipped by
// OpenNeko. It is intentionally compiled into the supervisor and mirrored by
// org.openneko.storage-contract on both database images; it is not an operator
// override. Bump it whenever PostgreSQL's libc/collation provider changes.
const StorageContractVersion = 1

// StorageCollationVersion is the actual version reported by the pinned
// Debian Bookworm/glibc database image. Checking it makes an accidental switch
// back to a provider such as Alpine/musl fail closed before application writers
// start. If this value changes, StorageContractVersion must also change so
// persisted indexes are rebuilt once under the new provider version.
const StorageCollationVersion = "2.36"

const storageReconcileAdvisoryLockKey int64 = 6243119227618659491

// StorageConn is the non-transactional pgx surface required by storage
// reconciliation. REINDEX DATABASE is deliberately executed outside a
// transaction block.
type StorageConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type storageCollationState struct {
	database string
	provider string
	recorded *string
	actual   *string
}

// ReconcileStorage repairs all persisted indexes when a database volume moves
// to the enforced storage ABI. It records completion in a numeric singleton
// row rather than schema_migrations: schema migration ledgers use text indexes
// (the thing being repaired) and can be bootstrapped as already applied.
//
// The function is idempotent. If a process dies after REINDEX but before the
// marker update, the next run safely repeats the repair.
func ReconcileStorage(ctx context.Context, conn StorageConn, logf func(string, ...any)) (bool, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, storageReconcileAdvisoryLockKey); err != nil {
		return false, fmt.Errorf("acquire storage reconciliation lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, storageReconcileAdvisoryLockKey)
	}()

	state, err := readStorageCollationState(ctx, conn)
	if err != nil {
		return false, err
	}
	if state.provider != "c" {
		return false, fmt.Errorf(
			"database %q uses collation provider %q; OpenNeko storage contract %d requires the pinned glibc provider",
			state.database, state.provider, StorageContractVersion,
		)
	}
	if state.actual == nil {
		return false, fmt.Errorf(
			"database %q cannot report an actual collation version; OpenNeko storage contract %d requires glibc %s (an Alpine/musl database image is not supported)",
			state.database, StorageContractVersion, StorageCollationVersion,
		)
	}
	if *state.actual != StorageCollationVersion {
		return false, fmt.Errorf(
			"database %q reports collation version %q; OpenNeko storage contract %d requires %q",
			state.database, *state.actual, StorageContractVersion, StorageCollationVersion,
		)
	}

	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS openneko_internal`); err != nil {
		return false, fmt.Errorf("create storage contract schema in %q: %w", state.database, err)
	}
	if _, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS openneko_internal.storage_contract (
  singleton smallint PRIMARY KEY CHECK (singleton = 1),
  contract_version integer NOT NULL CHECK (contract_version > 0),
  reconciled_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return false, fmt.Errorf("create storage contract marker in %q: %w", state.database, err)
	}

	current, err := readStorageContractVersion(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("read storage contract marker in %q: %w", state.database, err)
	}
	if current != nil && *current > StorageContractVersion {
		return false, fmt.Errorf(
			"database %q uses newer storage contract %d; this OpenNeko binary only supports contract %d",
			state.database, *current, StorageContractVersion,
		)
	}

	versionsMatch := state.recorded != nil && *state.recorded == *state.actual
	if current != nil && *current == StorageContractVersion && versionsMatch {
		logf("[storage] %s already satisfies contract %d (collation %s)", state.database, StorageContractVersion, *state.actual)
		return false, nil
	}

	quotedDatabase := pgx.Identifier{state.database}.Sanitize()
	logf("[storage] reconciling %s to contract %d; rebuilding persisted indexes", state.database, StorageContractVersion)
	if _, err := conn.Exec(ctx, "REINDEX DATABASE "+quotedDatabase); err != nil {
		return false, fmt.Errorf("reindex database %q: %w", state.database, err)
	}
	if state.recorded == nil {
		// PostgreSQL deliberately rejects REFRESH COLLATION VERSION when the
		// recorded version is NULL but the current provider reports a version.
		// Databases originally created under Alpine/musl have exactly that state
		// when first opened by the enforced glibc image. After rebuilding every
		// index above, initialize the catalog marker to the already-validated
		// runtime version and verify it below before recording our own contract.
		if _, err := conn.Exec(ctx, `
UPDATE pg_catalog.pg_database
SET datcollversion = $1
WHERE datname = current_database()
  AND datcollversion IS NULL`, *state.actual); err != nil {
			return false, fmt.Errorf("initialize collation version for database %q: %w", state.database, err)
		}
	} else if _, err := conn.Exec(ctx, "ALTER DATABASE "+quotedDatabase+" REFRESH COLLATION VERSION"); err != nil {
		return false, fmt.Errorf("refresh collation version for database %q: %w", state.database, err)
	}

	verified, err := readStorageCollationState(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("verify storage contract for database %q: %w", state.database, err)
	}
	if verified.actual == nil || verified.recorded == nil || *verified.actual != *verified.recorded || *verified.actual != StorageCollationVersion {
		return false, fmt.Errorf(
			"database %q collation verification failed after reindex: recorded=%s actual=%s expected=%s",
			state.database, nullableString(verified.recorded), nullableString(verified.actual), StorageCollationVersion,
		)
	}
	if _, err := conn.Exec(ctx, `
INSERT INTO openneko_internal.storage_contract (singleton, contract_version, reconciled_at)
VALUES (1, $1, now())
ON CONFLICT (singleton) DO UPDATE
SET contract_version = EXCLUDED.contract_version,
    reconciled_at = EXCLUDED.reconciled_at`, StorageContractVersion); err != nil {
		return false, fmt.Errorf("record storage contract for database %q: %w", state.database, err)
	}
	logf("[storage] %s now satisfies contract %d (collation %s)", state.database, StorageContractVersion, *verified.actual)
	return true, nil
}

func readStorageCollationState(ctx context.Context, conn StorageConn) (storageCollationState, error) {
	var state storageCollationState
	err := conn.QueryRow(ctx, `
SELECT current_database(), d.datlocprovider::text, d.datcollversion,
       pg_database_collation_actual_version(d.oid)
FROM pg_database AS d
WHERE d.datname = current_database()`).Scan(
		&state.database,
		&state.provider,
		&state.recorded,
		&state.actual,
	)
	if err != nil {
		return storageCollationState{}, fmt.Errorf("inspect database collation runtime: %w", err)
	}
	return state, nil
}

func readStorageContractVersion(ctx context.Context, conn StorageConn) (*int, error) {
	var version int
	err := conn.QueryRow(ctx, `
SELECT contract_version
FROM openneko_internal.storage_contract
LIMIT 1`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func nullableString(value *string) string {
	if value == nil {
		return "<none>"
	}
	return fmt.Sprintf("%q", *value)
}
