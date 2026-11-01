#!/bin/sh
set -eu

action=${1:-bootstrap}
label=${2:-current}
eval_dir=${OPENNEKO_EVAL_DIR:-/eval}
snapshot_dir=${OPENNEKO_EVAL_SNAPSHOT_DIR:-/snapshot}
database=${PGDATABASE:-adventureworks}
reader_user=${OPENNEKO_EVAL_READER_USER:-openneko_eval_reader}
reader_password=${OPENNEKO_EVAL_READER_PASSWORD:-eval-reader-only}

version_file="$snapshot_dir/version"
source_file="$snapshot_dir/source.sha256"
baseline_file="$snapshot_dir/baseline.fingerprint"
dump_file="$snapshot_dir/adventureworks.dump"

fail() {
  echo "[eval-freeze] $*" >&2
  exit 1
}

case "$database" in
  ''|[0-9]*|*[!A-Za-z0-9_]*) fail "invalid database identifier: $database" ;;
  *) ;;
esac
case "$reader_user" in
  ''|[0-9]*|*[!A-Za-z0-9_]*) fail "invalid reader role identifier: $reader_user" ;;
  *) ;;
esac

expected_version=$(sed -n '1p' "$eval_dir/SNAPSHOT_VERSION")
[ -n "$expected_version" ] || fail "SNAPSHOT_VERSION is empty"

snapshot_state() {
  present=0
  missing=0
  for required in "$version_file" "$source_file" "$baseline_file" "$dump_file"; do
    if [ -f "$required" ]; then
      present=$((present + 1))
    else
      missing=$((missing + 1))
    fi
  done

  if [ "$present" -eq 0 ]; then
    printf '%s\n' empty
  elif [ "$missing" -eq 0 ]; then
    printf '%s\n' complete
  else
    printf '%s\n' partial
  fi
}

validate_snapshot_contract() {
  state=$(snapshot_state)
  [ "$state" = complete ] || fail "snapshot is $state; run 'scripts/eval-adventureworks/environment.sh reset --yes' to rebuild only the eval volumes"

  observed_version=$(sed -n '1p' "$version_file")
  [ "$observed_version" = "$expected_version" ] || fail "snapshot version '$observed_version' does not match '$expected_version'; reset is required"
  cmp -s "$eval_dir/source.sha256" "$source_file" || fail "snapshot source manifest does not match the checked-in contract; bump the snapshot version and reset intentionally"
  pg_restore --list "$dump_file" >/dev/null || fail "snapshot dump is unreadable; reset is required"
}

validate_seed_sources() {
  echo "[eval-freeze] verifying pinned seed inputs"
  sha256sum -c "$eval_dir/source.sha256"
}

validate_canonical_seed() {
  observed=$(psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --tuples-only --no-align \
    --dbname "$database" <<'SQL'
SELECT concat_ws('|',
  (
    SELECT count(*)
    FROM information_schema.tables
    WHERE table_type = 'BASE TABLE'
      AND table_schema IN ('person', 'humanresources', 'production', 'purchasing', 'sales')
  ),
  (SELECT count(*) FROM sales.salesorderheader),
  (SELECT count(*) FROM sales.salesorderdetail),
  (SELECT count(*) FROM person.person),
  (SELECT count(*) FROM production.product)
);
SQL
  )
  [ "$observed" = "68|31465|121317|19972|504" ] || fail "canonical seed sentinels differ: expected 68|31465|121317|19972|504, observed $observed"
}

normalize_loader_marker() {
  # The loader records wall-clock completion time. It is operational metadata,
  # not benchmark data, so normalize it once before capture and then include it
  # in the dump and fingerprint like every other user table.
  psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --dbname "$database" <<'SQL'
TRUNCATE TABLE public._openneko_aw_load_complete;
INSERT INTO public._openneko_aw_load_complete(loaded_at)
VALUES (TIMESTAMPTZ '2000-01-01 00:00:00+00');
SQL
}

provision_reader() {
  echo "[eval-freeze] provisioning database-enforced read-only GraphJin role"

  PGDATABASE=postgres psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet \
    --set reader_user="$reader_user" --set reader_password="$reader_password" <<'SQL'
SELECT format('CREATE ROLE %I', :'reader_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'reader_user')
\gexec
SELECT format(
  'ALTER ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
  :'reader_user',
  :'reader_password'
)
\gexec
SELECT format('ALTER ROLE %I SET default_transaction_read_only TO on', :'reader_user')
\gexec
SQL

  psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --dbname "$database" \
    --set reader_user="$reader_user" <<'SQL'
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'reader_user')
\gexec
SELECT format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC', current_database())
\gexec
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
SELECT format('GRANT USAGE ON SCHEMA %I TO %I', nspname, :'reader_user')
FROM pg_namespace
WHERE nspname <> 'information_schema'
  AND nspname !~ '^pg_'
ORDER BY nspname
\gexec
SELECT format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO %I', nspname, :'reader_user')
FROM pg_namespace
WHERE nspname <> 'information_schema'
  AND nspname !~ '^pg_'
ORDER BY nspname
\gexec
SELECT format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO %I', nspname, :'reader_user')
FROM pg_namespace
WHERE nspname <> 'information_schema'
  AND nspname !~ '^pg_'
ORDER BY nspname
\gexec
SQL

  reader_mode=$(PGUSER="$reader_user" PGPASSWORD="$reader_password" \
    psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --tuples-only --no-align \
      --dbname "$database" --command 'SHOW default_transaction_read_only')
  [ "$reader_mode" = on ] || fail "reader role did not inherit default_transaction_read_only=on"

  reader_writes=$(PGUSER="$reader_user" PGPASSWORD="$reader_password" \
    psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --tuples-only --no-align \
      --dbname "$database" --command "SELECT has_table_privilege(current_user, 'sales.salesorderheader', 'INSERT') OR has_table_privilege(current_user, 'sales.salesorderheader', 'UPDATE') OR has_table_privilege(current_user, 'sales.salesorderheader', 'DELETE')")
  [ "$reader_writes" = f ] || fail "reader role unexpectedly has write privileges"
}

write_fingerprint() {
  output=$1
  fingerprint_label=$2
  PGDATABASE="$database" /bin/sh "$eval_dir/fingerprint.sh" "$fingerprint_label" >"$output"
}

verify_fingerprint() {
  validate_snapshot_contract
  current_file=$(mktemp)
  trap 'rm -f "$current_file"' EXIT HUP INT TERM
  write_fingerprint "$current_file" "$label"

  if ! cmp -s "$baseline_file" "$current_file"; then
    echo "[eval-freeze] dataset drift detected during '$label' verification" >&2
    if command -v diff >/dev/null 2>&1; then
      diff -u "$baseline_file" "$current_file" >&2 || true
    else
      echo "[eval-freeze] expected fingerprint:" >&2
      cat "$baseline_file" >&2
      echo "[eval-freeze] observed fingerprint:" >&2
      cat "$current_file" >&2
    fi
    exit 1
  fi

  observed=$(awk -F '|' '$1 == "fingerprint_sha256" { print $2 }' "$current_file")
  echo "[eval-freeze] $label fingerprint verified: $observed"
  rm -f "$current_file"
  trap - EXIT HUP INT TERM
}

capture_snapshot() {
  [ "$(snapshot_state)" = empty ] || fail "refusing to overwrite a non-empty snapshot; use reset --yes for an intentional rebuild"
  validate_seed_sources
  validate_canonical_seed
  normalize_loader_marker
  provision_reader

  partial_dump="$snapshot_dir/.adventureworks.dump.partial"
  partial_baseline="$snapshot_dir/.baseline.fingerprint.partial"
  partial_source="$snapshot_dir/.source.sha256.partial"
  partial_version="$snapshot_dir/.version.partial"
  rm -f "$partial_dump" "$partial_baseline" "$partial_source" "$partial_version"

  write_fingerprint "$partial_baseline" baseline
  pg_dump --format=custom --compress=9 --no-owner --no-acl \
    --dbname "$database" --file "$partial_dump"
  pg_restore --list "$partial_dump" >/dev/null
  cp "$eval_dir/source.sha256" "$partial_source"
  cp "$eval_dir/SNAPSHOT_VERSION" "$partial_version"

  mv "$partial_dump" "$dump_file"
  mv "$partial_baseline" "$baseline_file"
  mv "$partial_source" "$source_file"
  mv "$partial_version" "$version_file"
  echo "[eval-freeze] captured immutable snapshot contract $expected_version"
  verify_fingerprint
}

restore_snapshot() {
  validate_snapshot_contract
  echo "[eval-freeze] restoring $database from $expected_version"

  PGDATABASE=postgres psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet \
    --set target_database="$database" <<'SQL'
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', :'target_database')
\gexec
SELECT format('CREATE DATABASE %I TEMPLATE template0', :'target_database')
\gexec
SQL

  pg_restore --exit-on-error --no-owner --no-acl \
    --dbname "$database" "$dump_file"
  provision_reader
  verify_fingerprint
}

prepare_seed() {
  [ "$(snapshot_state)" = empty ] || fail "refusing to prepare a seed while a frozen snapshot exists"
  echo "[eval-freeze] removing any interrupted seed database before the pinned load"
  PGDATABASE=postgres psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet \
    --set target_database="$database" <<'SQL'
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', :'target_database')
\gexec
SQL
}

case "$action" in
  bootstrap)
    state=$(snapshot_state)
    case "$state" in
      complete) restore_snapshot ;;
      empty)
        echo "[eval-freeze] no frozen snapshot exists; the pinned seed must be loaded" >&2
        exit 42
        ;;
      partial) fail "snapshot is partial; reset --yes is required" ;;
    esac
    ;;
  prepare-seed) prepare_seed ;;
  capture) capture_snapshot ;;
  restore) restore_snapshot ;;
  verify) verify_fingerprint ;;
  fingerprint)
    write_fingerprint /dev/stdout "$label"
    ;;
  *)
    fail "unknown action '$action' (expected bootstrap, prepare-seed, capture, restore, verify, or fingerprint)"
    ;;
esac
