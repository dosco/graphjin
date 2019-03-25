#!/bin/sh
go test -bench=. -benchmem -memprofile mem_profile.out
go tool pprof mem_profile.out