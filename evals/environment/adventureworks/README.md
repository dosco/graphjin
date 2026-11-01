# Frozen AdventureWorks eval environment

This environment is the frozen data plane for the AdventureWorks data eval
(evals/adventureworks), which runs on GraphJin's eval engine.
It combines a read-only AdventureWorks database with a stateless selection API
fixture and is separate from the interactive product demo:

- Compose project: `openneko-backend-eval`
- PostgreSQL: `127.0.0.1:15433`
- GraphJin: `127.0.0.1:18080`
- volumes and network: fixed `openneko-backend-eval-*` names
- GraphJin config and OpenAPI spec: bind-mounted read-only from this directory;
  managed artifacts, watches, and tasks are disabled because the default
  AdventureWorks source cannot and must not host their mutable SQL store
- database role: `openneko_eval_reader`, with SELECT grants and
  `default_transaction_read_only=on`
- GraphJin database policy: immutable `read_only: true`
- GraphJin API policy: only the checked-in `selectFulfillmentRoute` POST is
  exposed as a mutation; its fixture response is deterministic and stateless
- GraphJin's built-in Ax agent: disabled; only primitive GraphJin tools are in
  the backend track

The Compose file contains no AdventureWorks simulator or scenario injector.
Its small API service exists solely to distinguish an allowed API `call`
mutation from a denied database mutation.
It does not extend the normal OpenNeko Compose files, and it never mounts the
demo database or GraphJin config volumes.

## Frozen seed contract

The first `up` reuses the production-compatible `adventureworks-loader` image
target and the existing install SQL. Before accepting the result, the freeze
step verifies all four seed inputs in `source.sha256`:

- Microsoft's `AdventureWorks-oltp-install-script.zip` release asset;
- `db/seeds/dev/adventureworks-install.sql`;
- `apps/worker/scripts/load-adventureworks.ts`;
- this environment's retry-safe `seed.sh` wrapper.

The pinned archive is the 17,486,641-byte asset downloaded by the loader from
`https://github.com/Microsoft/sql-server-samples/releases/download/adventureworks/AdventureWorks-oltp-install-script.zip`; its SHA-256 is recorded
in `source.sha256`. The release URL is mutable upstream, so a changed payload
must fail instead of becoming a new baseline implicitly.

The pinned archive itself currently contains sales-order dates from
`2022-05-30` through `2025-06-29`. Those dates are source data, not an eval-time
rewrite. The eval seed path never runs `advance-dates.sql`, `aw-sim-tick.sql`,
or `scenario-injector.sh`; the static contract rejects those references as well
as the simulator and scenario-injector Compose services.

The database and snapshot tools use the same multi-platform digest for
PostgreSQL `16.15-alpine3.24`; GraphJin builds from `graphjin/` at the same commit.

It also checks canonical seed sentinels (68 business tables, 31,465 sales
orders, 121,317 sales lines, 19,972 people, and 504 products). Only then does
it create a custom-format PostgreSQL dump and logical baseline fingerprint in
the dedicated snapshot volume. The contract version is in
`SNAPSHOT_VERSION`.

Before that first load, the wrapper drops any incomplete AdventureWorks
database inside the dedicated eval volume. `seed.sh` preserves only a
checksum-matching source ZIP and re-extracts the CSVs, because the shared
loader converts those files in place. An interrupted initial load is therefore
safe to retry without double-converting cached data.

Every later `up` restores that dump before GraphJin starts. A partial,
unreadable, version-mismatched, or source-mismatched snapshot fails closed; it
is never silently replaced. The logical fingerprint covers user schema
columns, constraints, indexes, views, functions, and every base-table row as
an order-independent multiset, plus sequence state. Column positions are
normalized across active columns, so PostgreSQL's internal holes left by
dropped columns do not make a logically equivalent dump/restore look changed.
The loader's wall-clock completion marker is normalized to
`2000-01-01T00:00:00Z` before capture so it is deterministic and can remain
covered by the fingerprint.

## Usage

Use the wrapper from the repository root. Direct `docker compose up` is not a
supported lifecycle because the wrapper serializes GraphJin shutdown,
snapshot restore, and pre/post checks.

The data eval itself runs from evals/adventureworks (see its README). The
commands below are the frozen data-plane lifecycle.

```sh
scripts/eval-adventureworks/environment.sh config
scripts/eval-adventureworks/check-static.sh
scripts/eval-adventureworks/environment.sh up
scripts/eval-adventureworks/environment.sh verify-pre
```

The data eval config (`evals/adventureworks/dev.yml`) reads the snapshot on
`127.0.0.1:15433`. Keep a benchmark on these isolated ports. The demo's
`8080`/`5433` endpoints hold mutable dev data and invalidate a run.

Run an explicit preflight immediately before a cohort and a postflight after
the final attempt. Both commands exit non-zero and print a per-table diff if
the database moved from the baseline:

```sh
scripts/eval-adventureworks/environment.sh verify-pre
# run the cohort
scripts/eval-adventureworks/environment.sh verify-post
```

The normal shutdown performs the postflight again and preserves all three
volumes:

```sh
scripts/eval-adventureworks/environment.sh down
```

Port overrides retain loopback-only binding and do not change resource
ownership:

```sh
OPENNEKO_EVAL_AW_DB_PORT=25433 \
OPENNEKO_EVAL_GRAPHJIN_PORT=28080 \
scripts/eval-adventureworks/environment.sh up
```

If the reader password is overridden, use the same
`OPENNEKO_EVAL_AW_READER_PASSWORD` value for every lifecycle command so
GraphJin and the reconciled database role remain aligned.

## Recovery

If a postflight detects data drift, the command leaves the project running so
the failure can be inspected. Restore only the eval database, then verify and
shut down:

```sh
scripts/eval-adventureworks/environment.sh fingerprint
scripts/eval-adventureworks/environment.sh restore
scripts/eval-adventureworks/environment.sh verify-post
scripts/eval-adventureworks/environment.sh down
```

If the snapshot itself is partial or corrupt, deliberately delete the three
dedicated eval volumes and reseed from the pinned inputs:

```sh
scripts/eval-adventureworks/environment.sh reset --yes
scripts/eval-adventureworks/environment.sh up
```

`reset --yes` is destructive for the eval project only. It does not address
or remove the active OpenNeko demo project's containers, networks, or volumes.
If a checked-in seed input changes intentionally, update `source.sha256`, bump
`SNAPSHOT_VERSION`, review the new logical baseline, and then reset. If the
pinned upstream ZIP is no longer downloadable, restore it into the dedicated
cache volume from an approved artifact rather than accepting a different
checksum.
