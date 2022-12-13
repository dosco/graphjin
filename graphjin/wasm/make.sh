#!/bin/sh
cp $(go env GOROOT)/misc/wasm/wasm_exec.js ./runtime/ && \
GOOS=js GOARCH=wasm go build -o graphjin.wasm *.go
