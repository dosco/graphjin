#!/bin/sh
go test -bench=. -benchmem -cpuprofile cpu_profile.out
go tool pprof cpu_profile.out