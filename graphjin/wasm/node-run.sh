#!/bin/sh
cp $(go env GOROOT)/misc/wasm/wasm_exec.js ./site/ && \
GOOS=js GOARCH=wasm go build -o serve.wasm ./driver.go ./main.go
node index.js
# deno run --allow-all --allow-read --allow-net  deno.js