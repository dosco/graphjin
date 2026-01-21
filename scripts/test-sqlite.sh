#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Building SQLite test container with SpatiaLite..."
docker build -t graphjin-sqlite-test -f Dockerfile.sqlite-test .

echo "Running SQLite tests..."
# Use libsqlite3 tag to link against system SQLite (which supports load_extension)
docker run --rm \
    -e CGO_ENABLED=1 \
    -e GOTOOLCHAIN=auto \
    -w /app/tests \
    graphjin-sqlite-test \
    go test -v -timeout 30m -db=sqlite -tags "sqlite sqlite_fts5 libsqlite3" "$@" .
