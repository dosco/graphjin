#!/bin/bash
set -e
echo "Running MSSQL tests..."
cd tests
go test -v -timeout 30m -race -db=mssql -tags mssql .
