#!/bin/sh
GOROOT_PATH=$(go env GOROOT)
if [ -f "$GOROOT_PATH/lib/wasm/wasm_exec.js" ]; then
    cp "$GOROOT_PATH/lib/wasm/wasm_exec.js" ./js/
elif [ -f "$GOROOT_PATH/misc/wasm/wasm_exec.js" ]; then
    cp "$GOROOT_PATH/misc/wasm/wasm_exec.js" ./js/
else
    echo "Error: wasm_exec.js not found in Go installation"
    exit 1
fi
GOOS=js GOARCH=wasm go build -o graphjin.wasm *.go
