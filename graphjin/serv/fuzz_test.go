package serv

import "testing"

func TestFuzzCrashers(t *testing.T) {
	var crashers = []string{
		"query",
		"q",
		"que",
	}

	for _, f := range crashers {
		isMutation(f)
		gqlHash(f, nil, "")
	}
}
