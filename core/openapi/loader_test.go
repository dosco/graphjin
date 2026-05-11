package openapi

import "testing"

func TestExpandSpecConfigDefaultsEnv(t *testing.T) {
	t.Setenv("TEST_OAPI_DEFAULT_REGION", "us-west")

	in := SpecConfig{
		Operations: map[string]OperationOverride{
			"listWidgets": {
				ExposeTopLevel: true,
				Defaults: map[string]string{
					"region":    "${TEST_OAPI_DEFAULT_REGION}",
					"format":    "json",
					"missing":   "${TEST_OAPI_UNSET_VALUE}",
				},
			},
		},
	}

	out := expandSpecConfig(in)
	got := out.Operations["listWidgets"].Defaults
	if got["region"] != "us-west" {
		t.Errorf("region = %q, want us-west", got["region"])
	}
	if got["format"] != "json" {
		t.Errorf("format = %q, want json (literal pass-through)", got["format"])
	}
	if got["missing"] != "" {
		t.Errorf("missing = %q, want empty (unset env → empty, matching auth behaviour)", got["missing"])
	}

	// Caller's input map must be untouched.
	if in.Operations["listWidgets"].Defaults["region"] != "${TEST_OAPI_DEFAULT_REGION}" {
		t.Errorf("expandSpecConfig mutated the caller's Defaults map")
	}
}
