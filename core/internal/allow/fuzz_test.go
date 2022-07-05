package allow

import (
	"testing"

	"github.com/dosco/graphjin/core/internal/graph"
)

func TestFuzzCrashers(t *testing.T) {
	var crashers = []string{
		"query",
		"q",
		"que",
	}

	for _, f := range crashers {
		_, _ = graph.FastParse(f)
	}
}
