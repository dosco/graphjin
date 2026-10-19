#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running MySQL tests..."
go test -v -timeout 30m -race -db=mysql -tags mysql "$@" .
