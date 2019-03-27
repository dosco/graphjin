#!/bin/sh
go test -bench=. -benchmem -memprofile mem.out
go tool pprof -cum mem.out