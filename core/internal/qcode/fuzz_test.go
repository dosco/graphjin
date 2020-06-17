// +build gofuzz

package qcode

import (
	"testing"
)

var ret int

func TestFuzzCrashers(t *testing.T) {
	var crashers = []string{
		"390625...ˋ�#w\"�" + "�",
		"00:.ދ",
		"0000000000000:.ދ",
	}

	for _, f := range crashers {
		ret = Fuzz([]byte(f))
	}
}
