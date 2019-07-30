#!/bin/sh
if brew ls --versions wrk > /dev/null; then
  wrk -t12 -c400 -d30s --timeout 10s --script=query.lua --latency http://localhost:8080/api/v1/graphql
else
  brew install wek
  wrk -t12 -c400 -d30s --timeout 10s --script=query.lua --latency http://localhost:8080/api/v1/graphql
fi