#!/bin/sh
# Ramp VUs from 0 to 100 over 10s, stay there for 60s, then 10s down to 0.
k6 run -u 0 -s 10s:100 -s 60s:0 -s 10s:0  query.js