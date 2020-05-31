// +build gofuzz

package psql

import (
	"testing"
)

var ret int

func TestFuzzCrashers(t *testing.T) {
	var crashers = []string{
		"{\"connect\":{}}",
		"q(q{q{q{q{q{q{q{q{",
	}

	for _, f := range crashers {
		ret = Fuzz([]byte(f))
	}
}
