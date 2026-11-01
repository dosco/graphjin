//go:build integration

// Integration test for the migrator. Spins up real pgvector/pgvector:pg16
// via testcontainers-go and runs every embedded migration against it.
//
// Run with:  go test -tags=integration ./internal/db/...
// Needs docker on the host; CI runs this on ubuntu-latest.
package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/db"
)

const enforcedStorageImage = "pgvector/pgvector:0.8.6-pg16-bookworm@sha256:ccc6e83d6e35e931dc7c5def2022729d5a6c370318d099181995567ff1fb4d6b"

func TestMigratorAppliesAllEmbeddedMigrationsAgainstRealPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pg, err := postgres.Run(ctx,
		"pgvector/pgvector:pg16",
		postgres.WithDatabase("neko"),
		postgres.WithUsername("neko"),
		postgres.WithPassword("secret"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pg.Terminate(ctx); err != nil {
			t.Logf("terminate: %v", err)
		}
	})

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connstring: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	mig := &db.Migrator{FS: assets.MigrationsFS, Dir: "migrations"}
	logged := []string{}
	ran, err := mig.Apply(ctx, conn, func(format string, args ...any) {
		logged = append(logged, format)
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ran == 0 {
		t.Fatal("expected at least one migration to run against a fresh DB")
	}

	// schema_migrations should have one row per .sql file embedded.
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	files, _ := assets.MigrationsFS.ReadDir("migrations")
	wantSQL := 0
	for _, f := range files {
		if !f.IsDir() && len(f.Name()) > 4 && f.Name()[len(f.Name())-4:] == ".sql" {
			wantSQL++
		}
	}
	if count != wantSQL {
		t.Fatalf("schema_migrations has %d rows, want %d (one per embedded .sql)", count, wantSQL)
	}

	// Canonical tables installed by the migrations exist.
	for _, table := range []string{
		"organization",
		"schema_migrations",
		"workflow_schedule_state",
		"workflow_schedule_firing",
		"workflow_scheduler_health",
	} {
		var oid *uint32
		if err := conn.QueryRow(ctx, "SELECT to_regclass($1)::oid", "public."+table).Scan(&oid); err != nil {
			t.Fatalf("regclass(%s): %v", table, err)
		}
		if oid == nil {
			t.Fatalf("expected table %s to exist after migrations", table)
		}
	}

	// Re-running is a no-op.
	ran2, err := mig.Apply(ctx, conn, nil)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if ran2 != 0 {
		t.Fatalf("expected 0 new migrations on second apply, got %d", ran2)
	}
}

func TestMigratorBootstrapAgainstExistingSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pg, err := postgres.Run(ctx,
		"pgvector/pgvector:pg16",
		postgres.WithDatabase("neko"),
		postgres.WithUsername("neko"),
		postgres.WithPassword("secret"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	// Pre-create the canonical bootstrap table so the migrator's "existing
	// schema without tracking" path fires (mirrors what an operator who
	// upgraded from before schema_migrations existed looks like).
	if _, err := conn.Exec(ctx, `CREATE TABLE public.organization (id uuid primary key)`); err != nil {
		t.Fatal(err)
	}

	mig := &db.Migrator{FS: assets.MigrationsFS, Dir: "migrations"}
	ran, err := mig.Apply(ctx, conn, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ran != 0 {
		t.Fatalf("bootstrap path should have marked all migrations applied without running them, ran=%d", ran)
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected schema_migrations to be populated by bootstrap path")
	}
}

func TestStorageReconcileRepairsVolumeAcrossGlibcMuslGlibcUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	volume := fmt.Sprintf("openneko-storage-upgrade-%d", time.Now().UnixNano())
	start := func(image string, readyOccurrences int) *postgres.PostgresContainer {
		container, err := postgres.Run(ctx,
			image,
			postgres.WithDatabase("neko"),
			postgres.WithUsername("neko"),
			postgres.WithPassword("secret"),
			testcontainers.WithMounts(testcontainers.VolumeMount(volume, "/var/lib/postgresql/data")),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(readyOccurrences).
					WithStartupTimeout(2*time.Minute),
			),
		)
		if err != nil {
			t.Fatalf("start %s: %v", image, err)
		}
		return container
	}
	connect := func(container *postgres.PostgresContainer) *pgx.Conn {
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}

	// The pre-lean runtime was Debian/glibc and recorded its collation version.
	glibcBefore := start(enforcedStorageImage, 2)
	conn := connect(glibcBefore)
	if _, err := conn.Exec(ctx, `
CREATE SCHEMA pgboss;
CREATE TABLE pgboss.queue (name text PRIMARY KEY);
CREATE TABLE pgboss.job (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL REFERENCES pgboss.queue(name)
);
INSERT INTO pgboss.queue(name) VALUES ('__pgboss__send-it');
CREATE TABLE public.records_probe (name text PRIMARY KEY);
INSERT INTO public.records_probe(name) VALUES ('alpha'), ('Zulu'), ('éclair')`); err != nil {
		t.Fatalf("seed glibc volume: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := glibcBefore.Terminate(ctx); err != nil {
		t.Fatalf("stop initial glibc container: %v", err)
	}

	// v2.29 switched the same persistent volume to Alpine/musl. PostgreSQL can
	// no longer report the recorded glibc collation's actual version, while
	// application writes continue and leave indexes built by two runtimes.
	musl := start("postgres:16-alpine", 1)
	conn = connect(musl)
	var muslActual *string
	if err := conn.QueryRow(ctx, `
SELECT pg_database_collation_actual_version(oid)
FROM pg_database WHERE datname = current_database()`).Scan(&muslActual); err != nil {
		t.Fatal(err)
	}
	if muslActual != nil {
		t.Fatalf("Alpine unexpectedly reported collation version %q; test no longer exercises the provider transition", *muslActual)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO public.records_probe(name) VALUES ('beta')`); err != nil {
		t.Fatalf("write through musl runtime: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := musl.Terminate(ctx); err != nil {
		t.Fatalf("stop musl container: %v", err)
	}

	// The candidate release restores the enforced glibc ABI and must reconcile
	// the inherited volume before any application service starts.
	glibcAfter := start(enforcedStorageImage, 1)
	t.Cleanup(func() {
		if err := glibcAfter.Terminate(context.Background(), testcontainers.RemoveVolumes(volume)); err != nil {
			t.Logf("cleanup upgraded storage volume: %v", err)
		}
	})
	conn = connect(glibcAfter)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	repaired, err := db.ReconcileStorage(ctx, conn, t.Logf)
	if err != nil {
		t.Fatalf("reconcile inherited volume: %v", err)
	}
	if !repaired {
		t.Fatal("inherited volume must be repaired on first contract-1 boot")
	}

	var contract int
	var recorded, actual *string
	if err := conn.QueryRow(ctx, `SELECT contract_version FROM openneko_internal.storage_contract WHERE singleton = 1`).Scan(&contract); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
SELECT datcollversion, pg_database_collation_actual_version(oid)
FROM pg_database WHERE datname = current_database()`).Scan(&recorded, &actual); err != nil {
		t.Fatal(err)
	}
	if contract != db.StorageContractVersion || recorded == nil || actual == nil || *recorded != *actual || *actual != db.StorageCollationVersion {
		t.Fatalf("storage verification failed: contract=%d recorded=%v actual=%v", contract, recorded, actual)
	}

	// This is the production failure mode: an indexed equality lookup for the
	// pg-boss parent row must agree with the persisted row, and its FK insert
	// must succeed after reconciliation.
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var queueFound bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pgboss.queue WHERE name = '__pgboss__send-it')`).Scan(&queueFound); err != nil {
		t.Fatal(err)
	}
	if !queueFound {
		t.Fatal("pg-boss send-it queue disappeared from indexed equality lookup")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO pgboss.job(name) VALUES ('__pgboss__send-it')`); err != nil {
		t.Fatalf("pg-boss FK insert after reconciliation: %v", err)
	}

	repairedAgain, err := db.ReconcileStorage(ctx, conn, t.Logf)
	if err != nil {
		t.Fatalf("second reconciliation: %v", err)
	}
	if repairedAgain {
		t.Fatal("second reconciliation should be a contract no-op")
	}
}

func TestStorageReconcileInitializesAlpineCreatedVolume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	volume := fmt.Sprintf("openneko-storage-alpine-origin-%d", time.Now().UnixNano())
	start := func(image string, readyOccurrences int) *postgres.PostgresContainer {
		container, err := postgres.Run(ctx,
			image,
			postgres.WithDatabase("neko"),
			postgres.WithUsername("neko"),
			postgres.WithPassword("secret"),
			testcontainers.WithMounts(testcontainers.VolumeMount(volume, "/var/lib/postgresql/data")),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(readyOccurrences).
					WithStartupTimeout(2*time.Minute),
			),
		)
		if err != nil {
			t.Fatalf("start %s: %v", image, err)
		}
		return container
	}
	connect := func(container *postgres.PostgresContainer) *pgx.Conn {
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}

	// Releases that first created their volume under Alpine/musl left the
	// database collation version NULL because musl exposes no version string.
	alpine := start("postgres:16-alpine", 2)
	conn := connect(alpine)
	if _, err := conn.Exec(ctx, `
CREATE TABLE public.legacy_probe (name text PRIMARY KEY);
INSERT INTO public.legacy_probe(name) VALUES ('alpha'), ('Zulu'), ('eclair')`); err != nil {
		t.Fatalf("seed Alpine-created volume: %v", err)
	}
	var recorded, actual *string
	if err := conn.QueryRow(ctx, `
SELECT datcollversion, pg_database_collation_actual_version(oid)
FROM pg_database WHERE datname = current_database()`).Scan(&recorded, &actual); err != nil {
		t.Fatal(err)
	}
	if recorded != nil || actual != nil {
		t.Fatalf("Alpine origin must have no collation version: recorded=%v actual=%v", recorded, actual)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := alpine.Terminate(ctx); err != nil {
		t.Fatalf("stop Alpine container: %v", err)
	}

	glibc := start(enforcedStorageImage, 1)
	t.Cleanup(func() {
		if err := glibc.Terminate(context.Background(), testcontainers.RemoveVolumes(volume)); err != nil {
			t.Logf("cleanup upgraded storage volume: %v", err)
		}
	})
	conn = connect(glibc)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if err := conn.QueryRow(ctx, `
SELECT datcollversion, pg_database_collation_actual_version(oid)
FROM pg_database WHERE datname = current_database()`).Scan(&recorded, &actual); err != nil {
		t.Fatal(err)
	}
	if recorded != nil || actual == nil || *actual != db.StorageCollationVersion {
		t.Fatalf("test must exercise NULL-to-glibc transition: recorded=%v actual=%v", recorded, actual)
	}

	repaired, err := db.ReconcileStorage(ctx, conn, t.Logf)
	if err != nil {
		t.Fatalf("reconcile Alpine-created volume: %v", err)
	}
	if !repaired {
		t.Fatal("Alpine-created volume must be repaired")
	}
	if err := conn.QueryRow(ctx, `
SELECT datcollversion, pg_database_collation_actual_version(oid)
FROM pg_database WHERE datname = current_database()`).Scan(&recorded, &actual); err != nil {
		t.Fatal(err)
	}
	if recorded == nil || actual == nil || *recorded != *actual || *actual != db.StorageCollationVersion {
		t.Fatalf("collation version was not initialized: recorded=%v actual=%v", recorded, actual)
	}

	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var found bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.legacy_probe WHERE name = 'alpha')`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("indexed lookup failed after Alpine-origin reconciliation")
	}
}
