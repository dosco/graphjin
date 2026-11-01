#!/bin/sh
set -eu

# Emit a logical, order-independent fingerprint. Base-table rows are hashed as
# a multiset, so physical row order and dump/restore order do not affect the
# result. The freeze step normalizes the loader marker before the first capture,
# allowing every user table to remain inside the fingerprint.

label=${1:-current}
database=${PGDATABASE:-adventureworks}

case "$database" in
  ''|[0-9]*|*[!A-Za-z0-9_]*)
    echo "[eval-fingerprint] invalid database identifier: $database" >&2
    exit 2
    ;;
  *) ;;
esac

scratch_dir=$(mktemp -d)
schema_file="$scratch_dir/schema"
tables_file="$scratch_dir/tables"
body_file="$scratch_dir/body"

cleanup() {
  rm -f "$schema_file" "$tables_file" "$body_file"
  rmdir "$scratch_dir" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --tuples-only --no-align \
  --dbname "$database" >"$schema_file" <<'SQL'
WITH user_namespaces AS (
  SELECT oid, nspname
  FROM pg_namespace
  WHERE nspname <> 'information_schema'
    AND nspname !~ '^pg_'
),
schema_lines AS (
  SELECT format(
    'column|%I.%I|%s|%I|%s|%s|%s|%s',
    c.table_schema,
    c.table_name,
    row_number() OVER (
      PARTITION BY c.table_schema, c.table_name
      ORDER BY c.ordinal_position
    ),
    c.column_name,
    c.data_type,
    c.udt_name,
    c.is_nullable,
    coalesce(replace(replace(c.column_default, E'\n', ' '), E'\r', ' '), '')
  ) AS line
  FROM information_schema.columns AS c
  JOIN user_namespaces AS n ON n.nspname = c.table_schema

  UNION ALL

  SELECT format(
    'constraint|%I.%I|%I|%s|%s',
    n.nspname,
    rel.relname,
    con.conname,
    con.contype,
    replace(replace(pg_get_constraintdef(con.oid, true), E'\n', ' '), E'\r', ' ')
  )
  FROM pg_constraint AS con
  JOIN pg_class AS rel ON rel.oid = con.conrelid
  JOIN user_namespaces AS n ON n.oid = rel.relnamespace

  UNION ALL

  SELECT format(
    'index|%I.%I|%I|%s',
    schemaname,
    tablename,
    indexname,
    replace(replace(indexdef, E'\n', ' '), E'\r', ' ')
  )
  FROM pg_indexes
  WHERE schemaname <> 'information_schema'
    AND schemaname !~ '^pg_'

  UNION ALL

  SELECT format(
    'view|%I.%I|%s',
    n.nspname,
    rel.relname,
    replace(replace(pg_get_viewdef(rel.oid, true), E'\n', ' '), E'\r', ' ')
  )
  FROM pg_class AS rel
  JOIN user_namespaces AS n ON n.oid = rel.relnamespace
  WHERE rel.relkind IN ('v', 'm')

  UNION ALL

  SELECT format(
    'function|%I.%I|%s',
    n.nspname,
    proc.proname,
    replace(replace(pg_get_functiondef(proc.oid), E'\n', ' '), E'\r', ' ')
  )
  FROM pg_proc AS proc
  JOIN user_namespaces AS n ON n.oid = proc.pronamespace
)
SELECT line
FROM schema_lines
ORDER BY line;
SQL

psql -X --no-psqlrc --set ON_ERROR_STOP=1 --quiet --tuples-only --no-align \
  --dbname "$database" >"$tables_file" <<'SQL'
CREATE TEMP TABLE eval_table_fingerprints (
  table_name text PRIMARY KEY,
  row_count bigint NOT NULL,
  content_md5 text NOT NULL
);
CREATE TEMP TABLE eval_sequence_fingerprints (
  sequence_name text PRIMARY KEY,
  last_value text NOT NULL,
  is_called boolean NOT NULL
);

DO $fingerprint$
DECLARE
  target record;
  observed_count bigint;
  observed_hash text;
BEGIN
  FOR target IN
    SELECT n.nspname AS schema_name, c.relname AS table_name
    FROM pg_class AS c
    JOIN pg_namespace AS n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'p')
      AND n.nspname <> 'information_schema'
      AND n.nspname !~ '^pg_'
    ORDER BY n.nspname, c.relname
  LOOP
    EXECUTE format(
      'SELECT count(row_md5), coalesce(md5(string_agg(row_md5, '''' ORDER BY row_md5)), md5('''')) FROM (SELECT md5(row_to_json(source_row)::text) AS row_md5 FROM %I.%I AS source_row) AS row_hashes',
      target.schema_name,
      target.table_name
    ) INTO observed_count, observed_hash;

    INSERT INTO eval_table_fingerprints(table_name, row_count, content_md5)
    VALUES (
      format('%I.%I', target.schema_name, target.table_name),
      observed_count,
      observed_hash
    );
  END LOOP;
END
$fingerprint$;

DO $fingerprint$
DECLARE
  target record;
  observed_last_value text;
  observed_is_called boolean;
BEGIN
  FOR target IN
    SELECT n.nspname AS schema_name, c.relname AS sequence_name
    FROM pg_class AS c
    JOIN pg_namespace AS n ON n.oid = c.relnamespace
    WHERE c.relkind = 'S'
      AND n.nspname <> 'information_schema'
      AND n.nspname !~ '^pg_'
    ORDER BY n.nspname, c.relname
  LOOP
    EXECUTE format(
      'SELECT last_value::text, is_called FROM %I.%I',
      target.schema_name,
      target.sequence_name
    ) INTO observed_last_value, observed_is_called;

    INSERT INTO eval_sequence_fingerprints(sequence_name, last_value, is_called)
    VALUES (
      format('%I.%I', target.schema_name, target.sequence_name),
      observed_last_value,
      observed_is_called
    );
  END LOOP;
END
$fingerprint$;

SELECT line
FROM (
  SELECT format('table|%s|%s|%s', table_name, row_count, content_md5) AS line
  FROM eval_table_fingerprints

  UNION ALL

  SELECT format('sequence|%s|%s|%s', sequence_name, last_value, is_called) AS line
  FROM eval_sequence_fingerprints
) AS fingerprints
ORDER BY line COLLATE "C";
SQL

schema_sha256=$(sha256sum "$schema_file" | awk '{print $1}')
data_sha256=$(sha256sum "$tables_file" | awk '{print $1}')

{
  printf '%s\n' 'format|openneko.eval.dataset-fingerprint/v2'
  printf 'database|%s\n' "$database"
  printf 'schema_sha256|%s\n' "$schema_sha256"
  printf 'data_sha256|%s\n' "$data_sha256"
  cat "$tables_file"
} >"$body_file"

fingerprint_sha256=$(sha256sum "$body_file" | awk '{print $1}')
cat "$body_file"
printf 'fingerprint_sha256|%s\n' "$fingerprint_sha256"
echo "[eval-fingerprint] $label: $fingerprint_sha256" >&2
