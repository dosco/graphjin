// +build tools

package main

import (
	_ "github.com/git-chglog/git-chglog/cmd/git-chglog"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
	_ "github.com/goreleaser/goreleaser@latest"
	_ "golang.org/x/perf/cmd/benchstat"
	_ "golang.org/x/tools/cmd/stringer"
)
