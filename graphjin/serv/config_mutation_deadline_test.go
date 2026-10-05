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
			extendDeadlineForConfigMutation(w, tc.query)
			if !tc.extend {
				if w.writeDeadlineCalls != 0 || w.readDeadlineCalls != 0 {
					t.Fatalf("deadline changed for %q", tc.query)
				}
				return
			}
			if w.writeDeadlineCalls != 1 || w.readDeadlineCalls != 1 {
				t.Fatalf("write calls=%d read calls=%d, want one each", w.writeDeadlineCalls, w.readDeadlineCalls)
			}
			if got := w.writeDeadline.Sub(before); got < configMutationDeadline-time.Second {
				t.Fatalf("write deadline %v after the request, want about %v", got, configMutationDeadline)
			}
		})
	}
}
