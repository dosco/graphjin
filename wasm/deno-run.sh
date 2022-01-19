#!/bin/sh
GOOS=js GOARCH=wasm go build -o serve.wasm ./main.go
deno run --allow-all --allow-read --allow-net  deno.js