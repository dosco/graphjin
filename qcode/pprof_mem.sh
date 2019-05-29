#!/bin/sh
go test -bench=Compile$ -benchmem -memprofile mem.out -run=XXX
go tool pprof --alloc_space qcode.test mem.out
