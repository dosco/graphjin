package serv

import "testing"

func TestFuzzCrashers(t *testing.T) {
	var crashers = []string{
		"query",
		"q",
		"que",
	}

	for _, f := range crashers {
		gqlName(f)
		gqlHash(f, nil, "")
	}
}
