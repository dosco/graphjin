#!/bin/sh
cp $(go env GOROOT)/misc/wasm/wasm_exec.js ./js/ && \
GOOS=js GOARCH=wasm go build -o graphjin.wasm *.go
