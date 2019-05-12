#!/bin/sh
go test -bench=. -benchmem -cpuprofile cpu.out  -run=XXX
go tool pprof -cum cpu.out