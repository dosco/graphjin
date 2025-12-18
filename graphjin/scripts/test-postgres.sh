#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Postgres tests..."
go test -v -timeout 30m -race -db=postgres "$@" .
