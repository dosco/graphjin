#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Snowflake tests..."

MAX_RETRIES=${SNOWFLAKE_TEST_RETRIES:-3}

for attempt in $(seq 1 "$MAX_RETRIES"); do
	if go test -v -timeout 30m -race -db=snowflake -run '^Test' "$@" .; then
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
