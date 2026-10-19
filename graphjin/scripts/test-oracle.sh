#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Oracle tests..."
go test -v -timeout 30m -race -count=1 -db=oracle "$@" .
