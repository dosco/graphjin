#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running BigQuery tests..."

export GRAPHJIN_BIGQUERY_MOCK="${GRAPHJIN_BIGQUERY_MOCK:-1}"
if [ "$GRAPHJIN_BIGQUERY_MOCK" = "1" ]; then
	export GRAPHJIN_BIGQUERY_BACKEND="${GRAPHJIN_BIGQUERY_BACKEND:-duckdb}"
	export GRAPHJIN_BIGQUERY_FALLBACK="${GRAPHJIN_BIGQUERY_FALLBACK:-strict}"
	echo "Using BigQuery simulator (${GRAPHJIN_BIGQUERY_BACKEND} backend)"
else
	if [ -z "${GRAPHJIN_BIGQUERY_PROJECT:-}" ] && command -v gcloud >/dev/null 2>&1; then
		export GRAPHJIN_BIGQUERY_PROJECT="$(gcloud config get-value project 2>/dev/null || true)"
	fi
	echo "Using live BigQuery project ${GRAPHJIN_BIGQUERY_PROJECT:-<unset>}"
fi

MAX_RETRIES=${BIGQUERY_TEST_RETRIES:-3}

for attempt in $(seq 1 "$MAX_RETRIES"); do
	if go test -v -timeout 30m -race -db=bigquery -run '^Test' "$@" .; then
		exit 0
	fi

	if [ "$attempt" -lt "$MAX_RETRIES" ]; then
		echo ""
		echo "==> Attempt $attempt/$MAX_RETRIES failed, retrying in 5s..."
		sleep 5
	fi
done

echo "==> All $MAX_RETRIES attempts failed."
exit 1
