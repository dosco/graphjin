#!/bin/sh
mkdir -p site
cp $(go env GOROOT)/misc/wasm/wasm_exec.html ./site/index.html
cp $(go env GOROOT)/misc/wasm/wasm_exec.js ./site/
GOOS=js GOARCH=wasm go build -o ./site/test.wasm ../main.go
go run main.go
open http://localhost:8080