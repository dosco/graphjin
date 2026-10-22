package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestClassifyStack(t *testing.T) {
	tests := []struct {
		name     string
		services []composeService
		want     health
	}{
		{"empty is down", nil, down},
		{"all up + one-shot migrate done", []composeService{
			{Service: "web", State: "running", Health: "healthy"},
			{Service: "worker", State: "running", Health: "healthy"},
			{Service: "openneko-serve", State: "running", Health: "healthy"},
			{Service: "neko-db", State: "running", Health: "healthy"},
			{Service: "storage-reconcile", State: "exited", ExitCode: 0},
			{Service: "neko-migrate", State: "exited", ExitCode: 0},
			{Service: "records-graphjin-config-init", State: "exited", ExitCode: 0},
		}, serving},
		{"running without a healthcheck counts as serving", []composeService{
			{Service: "web", State: "running", Health: ""},
		}, serving},
		{"unhealthy is degraded, not down", []composeService{
			{Service: "web", State: "running", Health: "healthy"},
			{Service: "openneko-serve", State: "running", Health: "unhealthy"},
		}, degraded},
		{"starting is degraded", []composeService{
			{Service: "worker", State: "running", Health: "starting"},
		}, degraded},
		{"a long-running service exited non-zero is down", []composeService{
			{Service: "worker", State: "exited", ExitCode: 1},
		}, down},
		{"down outranks degraded", []composeService{
			{Service: "web", State: "running", Health: "starting"},
			{Service: "worker", State: "exited", ExitCode: 1},
		}, down},
		{"a failed one-shot setup job is degraded, not down", []composeService{
			{Service: "web", State: "running", Health: "healthy"},
			{Service: "worker", State: "running", Health: "healthy"},
			{Service: "openneko-serve", State: "running", Health: "healthy"},
			{Service: "neko-db", State: "running", Health: "healthy"},
			{Service: "neko-adventureworks-seed", State: "exited", ExitCode: 1},
		}, degraded},
		{"a downed long-running service still outranks a failed one-shot job", []composeService{
			{Service: "neko-adventureworks-seed", State: "exited", ExitCode: 1},
			{Service: "worker", State: "exited", ExitCode: 1},
		}, down},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, lines := classifyStack(tc.services)
			if got != tc.want {
				t.Fatalf("classifyStack = %v, want %v", got, tc.want)
			}
			if len(tc.services) > 0 && len(lines) != len(tc.services) {
				t.Fatalf("expected one line per service, got %d for %d", len(lines), len(tc.services))
			}
		})
	}
}

func TestParseComposePs(t *testing.T) {
	arr := `[{"Service":"web","State":"running","Health":"healthy"},{"Service":"worker","State":"exited","ExitCode":0}]`
	if svcs, err := parseComposePs([]byte(arr)); err != nil || len(svcs) != 2 {
		t.Fatalf("array form: got %v err %v", svcs, err)
	}

	nd := `{"Service":"web","State":"running","Health":"healthy"}` + "\n" +
		`{"Service":"worker","State":"running","Health":"starting"}`
	svcs, err := parseComposePs([]byte(nd))
	if err != nil || len(svcs) != 2 || svcs[1].Health != "starting" {
		t.Fatalf("ndjson form: got %+v err %v", svcs, err)
	}

	if svcs, err := parseComposePs([]byte("   ")); err != nil || svcs != nil {
		t.Fatalf("empty: got %v err %v", svcs, err)
	}
}

func TestWriteStatusEmitsVerdictWord(t *testing.T) {
	for state, want := range map[health]string{serving: "serving", degraded: "degraded", down: "down"} {
		var b bytes.Buffer
		writeStatus(&b, state, []string{"  ✅ web — serving"}, "")
		if !strings.Contains(b.String(), want) {
			t.Fatalf("state %v output missing %q: %s", state, want, b.String())
		}
	}
}
