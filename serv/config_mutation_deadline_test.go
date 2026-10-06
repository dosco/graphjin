package serv

import (
	"testing"
	"time"
)

func TestExtendDeadlineForConfigMutation(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		extend bool
	}{
		{"config preview", `mutation { gj_config(id: "current", update: { mode: "preview" }) { valid preview_id } }`, true},
		{"named config apply with variables", "mutation Apply($u: JSON) {\n  gj_config(id: \"current\", update: $u) { applied }\n}", true},
		{"config read", `query { gj_config(id: "current") { catalog_revision } }`, false},
		{"data mutation", `mutation { users(insert: { name: "a" }) { id } }`, false},
		{"field named like the root", `mutation { gj_config_audit(insert: { note: "a" }) { id } }`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &deadlineCaptureWriter{}
			before := time.Now()
			extendDeadlineForConfigMutation(w, nil, tc.query)
			if !tc.extend {
				if w.writeDeadlineCalls != 0 || w.readDeadlineCalls != 0 {
					t.Fatalf("deadline changed for %q", tc.query)
				}
				return
			}
			if w.writeDeadlineCalls != 1 || w.readDeadlineCalls != 1 {
				t.Fatalf("write calls=%d read calls=%d, want one each", w.writeDeadlineCalls, w.readDeadlineCalls)
			}
			if got := w.writeDeadline.Sub(before); got < defaultConfigUpdateTimeout-time.Second {
				t.Fatalf("write deadline %v after the request, want about %v", got, defaultConfigUpdateTimeout)
			}
		})
	}
}

func TestConfigUpdateTimeoutFollowsConfig(t *testing.T) {
	if got := configUpdateTimeout(nil); got != defaultConfigUpdateTimeout {
		t.Fatalf("default = %v, want %v", got, defaultConfigUpdateTimeout)
	}
	conf := &Config{}
	conf.MCP.ConfigUpdateTimeout = 120
	if got := configUpdateTimeout(conf); got != 2*time.Minute {
		t.Fatalf("configured = %v, want 2m", got)
	}
	w := &deadlineCaptureWriter{}
	before := time.Now()
	extendDeadlineForConfigMutation(w, conf, `mutation { gj_config(id: "current", update: { mode: "preview" }) { valid } }`)
	if got := w.writeDeadline.Sub(before); got < 2*time.Minute-time.Second || got > 2*time.Minute+time.Second {
		t.Fatalf("write deadline %v after the request, want about 2m", got)
	}
}
