#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Redshift tests..."

export GRAPHJIN_REDSHIFT_MOCK="${GRAPHJIN_REDSHIFT_MOCK:-1}"
export GRAPHJIN_REDSHIFT_BACKEND="${GRAPHJIN_REDSHIFT_BACKEND:-duckdb}"
export GRAPHJIN_REDSHIFT_FALLBACK="${GRAPHJIN_REDSHIFT_FALLBACK:-strict}"
export GOCACHE="${GOCACHE:-/tmp/go-build}"

if [ "$GRAPHJIN_REDSHIFT_MOCK" = "1" ]; then
	echo "Using Redshift simulator (${GRAPHJIN_REDSHIFT_BACKEND} backend)"
else
	echo "Using live Redshift is not wired yet; set GRAPHJIN_REDSHIFT_MOCK=1"
	exit 1
fi

go test -v -timeout 30m -race -db=redshift -run '^TestRedshift' "$@" .
