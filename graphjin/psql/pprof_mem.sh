#!/bin/sh
go test -bench=. -benchmem -memprofile mem.out -run=XXX
go tool pprof -cum mem.out