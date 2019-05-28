#!/bin/sh
go test -bench=. -benchmem -memprofile mem.out -run=XXX
go tool pprof --alloc_space psql.test mem.out
