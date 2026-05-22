#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEST_DIR="$ROOT/tests"

RUN_FILTER=""
TIMEOUT="${SNOWFLAKE_CAPTURE_TIMEOUT:-30m}"
RACE_FLAG="-race"
BACKEND="${GRAPHJIN_SNOWFLAKE_BACKEND:-duckdb}"
EXTRA_GO_FLAGS=()

usage() {
	cat <<'USAGE'
Usage: scripts/capture-snowflake-sql.sh [-run REGEX] [--backend capture|duckdb] [--no-race] [-- GO_TEST_FLAGS...]

Runs Snowflake tests/examples one by one against the local mock driver and
captures every SQL statement under tests/.snowflake-capture/<run-id>/.
USAGE
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		-run)
			if [[ $# -lt 2 ]]; then
				echo "-run requires a regex" >&2
				exit 2
			fi
			RUN_FILTER="$2"
			shift 2
			;;
		-timeout)
			if [[ $# -lt 2 ]]; then
				echo "-timeout requires a value" >&2
				exit 2
			fi
			TIMEOUT="$2"
			shift 2
			;;
		--backend)
			if [[ $# -lt 2 ]]; then
				echo "--backend requires capture or duckdb" >&2
				exit 2
			fi
			BACKEND="$2"
			shift 2
			;;
		--no-race)
			RACE_FLAG=""
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		--)
			shift
			EXTRA_GO_FLAGS+=("$@")
			break
			;;
		*)
			EXTRA_GO_FLAGS+=("$1")
			shift
			;;
	esac
done

RUN_ID="${GRAPHJIN_SNOWFLAKE_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
CAPTURE_ROOT="${GRAPHJIN_SNOWFLAKE_CAPTURE_DIR:-$TEST_DIR/.snowflake-capture/$RUN_ID}"
SQL_DIR="$CAPTURE_ROOT/sql"
LOG_DIR="$CAPTURE_ROOT/logs"
MANIFEST="$CAPTURE_ROOT/manifest.jsonl"

mkdir -p "$SQL_DIR" "$LOG_DIR"
: > "$MANIFEST"
export GOCACHE="${GOCACHE:-$CAPTURE_ROOT/go-build-cache}"

json_escape() {
	local s="$1"
	s="${s//\\/\\\\}"
	s="${s//\"/\\\"}"
	s="${s//$'\n'/\\n}"
	printf '%s' "$s"
}

safe_name() {
	printf '%s' "$1" | sed -E 's/[^A-Za-z0-9_.-]+/_/g'
}

cd "$TEST_DIR" || exit 1

echo "Listing Snowflake tests/examples..."
LIST_OUTPUT="$(GRAPHJIN_SNOWFLAKE_MOCK=1 \
	GRAPHJIN_SNOWFLAKE_BACKEND=capture \
	GRAPHJIN_SNOWFLAKE_CAPTURE_DIR= \
	GRAPHJIN_SNOWFLAKE_TEST_NAME=list \
	GRAPHJIN_SNOWFLAKE_RUN_ID="$RUN_ID" \
	go test -list '^(Test|Example)' -db=snowflake . 2>&1)"
LIST_STATUS=$?
if [[ $LIST_STATUS -ne 0 ]]; then
	echo "$LIST_OUTPUT" >&2
	exit "$LIST_STATUS"
fi

NAMES=()
while IFS= read -r name; do
	[[ -n "$name" ]] && NAMES+=("$name")
done <<EOF
$(printf '%s\n' "$LIST_OUTPUT" | awk '/^(Test|Example)[A-Za-z0-9_]*$/ { print $1 }')
EOF

TOTAL=0
PASSED=0
FAILED=0

for name in "${NAMES[@]}"; do
	if [[ -n "$RUN_FILTER" && ! "$name" =~ $RUN_FILTER ]]; then
		continue
	fi

	TOTAL=$((TOTAL + 1))
	safe="$(safe_name "$name")"
	log_path="$LOG_DIR/$safe.log"
	capture_path="$SQL_DIR/$safe.jsonl"
	start_epoch="$(date +%s)"

	echo "==> $name"
	GRAPHJIN_SNOWFLAKE_MOCK=1 \
	GRAPHJIN_SNOWFLAKE_BACKEND="$BACKEND" \
	GRAPHJIN_SNOWFLAKE_CAPTURE_DIR="$SQL_DIR" \
	GRAPHJIN_SNOWFLAKE_TEST_NAME="$name" \
	GRAPHJIN_SNOWFLAKE_RUN_ID="$RUN_ID" \
	go test -v -timeout "$TIMEOUT" ${RACE_FLAG:+"$RACE_FLAG"} -db=snowflake -run "^${name}$" ${EXTRA_GO_FLAGS[@]+"${EXTRA_GO_FLAGS[@]}"} . >"$log_path" 2>&1
	exit_code=$?

	end_epoch="$(date +%s)"
	duration=$((end_epoch - start_epoch))
	status="pass"
	if [[ $exit_code -ne 0 ]]; then
		status="fail"
		FAILED=$((FAILED + 1))
	else
		PASSED=$((PASSED + 1))
	fi

	printf '{"run_id":"%s","test":"%s","status":"%s","exit_code":%d,"duration_seconds":%d,"log_path":"%s","capture_path":"%s"}\n' \
		"$(json_escape "$RUN_ID")" \
		"$(json_escape "$name")" \
		"$status" \
		"$exit_code" \
		"$duration" \
		"$(json_escape "$log_path")" \
		"$(json_escape "$capture_path")" >> "$MANIFEST"
done

echo "Capture complete: $TOTAL run, $PASSED passed, $FAILED failed (backend: $BACKEND)"
echo "Manifest: $MANIFEST"

if [[ $TOTAL -eq 0 ]]; then
	exit 1
fi
