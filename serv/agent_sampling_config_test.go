package serv

import (
	"os"
	"path/filepath"
	"testing"
)

// The sampling settings are pointers because unset and zero are different
// requests: the stack already pins temperature 0 when nothing is configured,
// so nil means "leave it alone" and a zero value means "greedy, deliberately,
// and recorded as such in run provenance".
//
// That distinction only survives if viper actually decodes a pointer field the
// way this assumes. This is the tripwire for that assumption — if it ever
// fails, the fix is a plain float plus a separate "pinned" flag, and nothing
// downstream of the config needs to change.
func TestAgentSamplingConfigDistinguishesUnsetFromZero(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "dev.yml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	const base = "app_name: test\nhost_port: 0.0.0.0:8080\n"

	absent, err := ReadInConfig(write(t, base))
	if err != nil {
		t.Fatal(err)
	}
	if absent.Agent.Temperature != nil || absent.Agent.TopP != nil {
		t.Fatalf("a config that says nothing about sampling must leave it unset: %v %v",
			absent.Agent.Temperature, absent.Agent.TopP)
	}

	pinnedZero, err := ReadInConfig(write(t, base+"agent:\n  temperature: 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if pinnedZero.Agent.Temperature == nil {
		t.Fatal("an explicit zero must be distinguishable from unset")
	}
	if *pinnedZero.Agent.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", *pinnedZero.Agent.Temperature)
	}

	both, err := ReadInConfig(write(t, base+"agent:\n  temperature: 0.8\n  top_p: 0.95\n"))
	if err != nil {
		t.Fatal(err)
	}
	if both.Agent.Temperature == nil || *both.Agent.Temperature != 0.8 {
		t.Fatalf("temperature did not survive: %v", both.Agent.Temperature)
	}
	if both.Agent.TopP == nil || *both.Agent.TopP != 0.95 {
		t.Fatalf("top_p did not survive: %v", both.Agent.TopP)
	}
}

// The environment variable is the path a training loop actually uses, and it
// needs BindEnv: the generic GJ_* mapper only carries keys that already read
// non-nil, so an unbound key set only in the environment is silently dropped.
func TestAgentSamplingReadsTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev.yml")
	if err := os.WriteFile(path, []byte("app_name: test\nhost_port: 0.0.0.0:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GJ_AGENT_TEMPERATURE", "0.8")
	t.Setenv("GJ_AGENT_TOP_P", "0.95")
	conf, err := ReadInConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Agent.Temperature == nil || *conf.Agent.Temperature != 0.8 {
		t.Fatalf("GJ_AGENT_TEMPERATURE did not reach the config: %v", conf.Agent.Temperature)
	}
	if conf.Agent.TopP == nil || *conf.Agent.TopP != 0.95 {
		t.Fatalf("GJ_AGENT_TOP_P did not reach the config: %v", conf.Agent.TopP)
	}
}

// Out-of-range values fail at load rather than at request time, where they
// read as a provider outage instead of a typo.
func TestAgentSamplingIsRangeChecked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev.yml")
	if err := os.WriteFile(path, []byte("app_name: test\nhost_port: 0.0.0.0:8080\nagent:\n  temperature: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInConfig(path); err == nil {
		t.Fatal("a temperature outside the accepted range must be refused at load")
	}
}

// Sampling stays out of the runtime-writable surface, like reasoning: how a
// shared server draws is an operator decision, not a caller's.
func TestAgentSamplingIsNotRuntimeWritable(t *testing.T) {
	for _, field := range []string{"temperature", "top_p"} {
		if agentWritableFields[field] {
			t.Fatalf("%q must not be runtime-writable", field)
		}
	}
}
