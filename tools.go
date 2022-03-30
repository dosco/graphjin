//go:build tools
// +build tools

package main

import (
	_ "github.com/git-chglog/git-chglog/cmd/git-chglog"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "golang.org/x/perf/cmd/benchstat"
	_ "golang.org/x/tools/cmd/stringer"
)
