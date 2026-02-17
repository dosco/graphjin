#!/usr/bin/env bash
#
# Run database integration test suites in parallel with concurrency limiting.
# Each suite is an independent go test process with its own Docker container.
#
# Usage:
#   bash scripts/test-parallel.sh
#   PARALLEL_DBS="postgres mysql" bash scripts/test-parallel.sh
#   MAX_PARALLEL=2 bash scripts/test-parallel.sh
#

set -u

# All supported database suites
ALL_DBS="postgres mysql mariadb sqlite oracle mssql mongodb"

# Allow caller to select a subset via env var
DBS="${PARALLEL_DBS:-$ALL_DBS}"

# Max number of suites to run concurrently (default 4)
MAX_PARALLEL="${MAX_PARALLEL:-2}"

# Temp directory for per-suite output
TMPDIR_BASE=$(mktemp -d "${TMPDIR:-/tmp}/graphjin-test.XXXXXX")

# Track background PIDs for cleanup
PIDS=""

cleanup() {
    if [ -n "$PIDS" ]; then
        for pid in $PIDS; do
            kill "$pid" 2>/dev/null
            wait "$pid" 2>/dev/null
        done
    fi
    rm -rf "$TMPDIR_BASE"
}

trap cleanup EXIT INT TERM

# Build the go test command for a given DB
db_test_cmd() {
    local db="$1"
    local tags=""
    case "$db" in
        mysql)   tags="-tags mysql" ;;
        mariadb) tags="-tags mariadb" ;;
        sqlite)  tags="-tags \"sqlite fts5\"" ;;
        mssql)   tags="-tags mssql" ;;
    esac
    echo "cd tests && go test -v -timeout 30m -race -db=$db $tags ."
}

# Convert space-separated list to array
DB_ARRAY=($DBS)
DB_COUNT=${#DB_ARRAY[@]}

echo "==> Starting parallel DB test suites: $DBS"
echo "==> Concurrency limit: $MAX_PARALLEL (total suites: $DB_COUNT)"
echo ""

# Collect results across all batches
FAILED=""
PASSED=""
RESULTS=""

# Process DBs in batches of MAX_PARALLEL
i=0
while [ "$i" -lt "$DB_COUNT" ]; do
    BATCH_PIDS=""
    BATCH_DBS=""
    j=0

    # Launch up to MAX_PARALLEL suites
    while [ "$j" -lt "$MAX_PARALLEL" ] && [ $((i + j)) -lt "$DB_COUNT" ]; do
        idx=$((i + j))
        db="${DB_ARRAY[$idx]}"
        outfile="$TMPDIR_BASE/$db.out"
        cmd=$(db_test_cmd "$db")
        (
            eval "$cmd"
        ) > "$outfile" 2>&1 &
        pid=$!
        PIDS="$PIDS $pid"
        BATCH_PIDS="$BATCH_PIDS $pid"
        BATCH_DBS="$BATCH_DBS $db"
        eval "PID_${db}=$pid"
        eval "OUT_${db}=$outfile"
        echo "  Started $db (pid $pid)"
        j=$((j + 1))
    done

    echo ""
    echo "==> Waiting for batch to finish:$BATCH_DBS"
    echo ""

    # Wait for this batch and collect results
    for db in $BATCH_DBS; do
        eval "pid=\$PID_${db}"
        if wait "$pid"; then
            PASSED="$PASSED $db"
            RESULTS="$RESULTS
  PASS  $db"
        else
            FAILED="$FAILED $db"
            RESULTS="$RESULTS
  FAIL  $db"
        fi
    done

    i=$((i + j))
done

# Print output for failed suites
if [ -n "$FAILED" ]; then
    for db in $FAILED; do
        eval "outfile=\$OUT_${db}"
        echo ""
        echo "=========================================="
        echo "FAIL: $db — full output"
        echo "=========================================="
        cat "$outfile"
    done
fi

# Summary
echo ""
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo "$RESULTS"
echo ""

if [ -n "$FAILED" ]; then
    echo "FAIL"
    exit 1
else
    echo "PASS (all suites)"
    exit 0
fi
