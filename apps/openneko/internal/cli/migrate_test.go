package cli

import (
	"testing"
)

func TestEnvTruthy(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on", " on "} {
		t.Setenv("OPENNEKO_TEST_TRUTHY", value)
		if !envTruthy("OPENNEKO_TEST_TRUTHY") {
			t.Fatalf("%q should be true", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "off", "garbage"} {
		t.Setenv("OPENNEKO_TEST_TRUTHY", value)
		if envTruthy("OPENNEKO_TEST_TRUTHY") {
			t.Fatalf("%q should be false", value)
		}
	}
}
