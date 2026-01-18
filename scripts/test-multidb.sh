#!/bin/bash
set -e
cd "$(dirname "$0")/.."

echo "Running Multi-Database Support Tests..."
echo "========================================"

echo ""
echo "Core multi-DB unit tests..."
go test -v ./core/... -run "CacheKey|MultiDB|DatabaseJoin|MergeRoot|GroupSelects|IsMultiDB|CountDatabase" "$@"

echo ""
echo "QCode multi-DB tests..."
go test -v ./core/internal/qcode/... -run "Database|SkipTypeDatabaseJoin" "$@"

echo ""
echo "Schema (sdata) multi-DB tests..."
go test -v ./core/internal/sdata/... -run "Database|CrossDatabase|RelDatabaseJoin|RelType" "$@"

echo ""
echo "========================================"
echo "Running Multi-DB Integration Tests (Example_)..."
echo "========================================"

# Run Example_ tests in multi-DB mode
# This starts PostgreSQL, SQLite, and MongoDB containers in parallel
echo ""
echo "Starting multi-database containers and running Example_ tests..."
echo "(This may take a minute to start all containers)"
go test -v -timeout 30m ./tests/... -db=multidb -run "Example_multiDB" "$@"

echo ""
echo "========================================"
echo "All multi-database tests passed!"
