#!/bin/sh
go test -bench=. -benchmem -cpuprofile cpu.out
go tool pprof -cum cpu.out