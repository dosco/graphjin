#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Snowflake tests..."
go test -v -timeout 30m -race -db=snowflake -run '^Test' "$@" .
