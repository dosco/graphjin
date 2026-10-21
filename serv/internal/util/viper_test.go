package util

import (
	"github.com/spf13/viper"
	"testing"
)

func TestSetKeyValueEnvPrefixesAndNestedUnderscores(t *testing.T) {
	for _, prefix := range []string{"GJ_", "SG_", "SJ_"} {
		for _, tc := range []struct{ env, key string }{
			{"LOG_LEVEL", "log_level"},
			{"DATABASE_HOST", "database.host"},
			{"RATE_LIMITER_RATE", "rate_limiter.rate"},
			{"DATABASES_READ_REPLICA_HOST", "databases.read_replica.host"},
			{"RATE_LIMITER_IP_HEADER", "rate_limiter.ip_header"},
		} {
			t.Run(prefix+tc.env, func(t *testing.T) {
				vi := viper.New()
				vi.SetDefault(tc.key, "before")
				if !SetKeyValue(vi, prefix+tc.env, "after") {
					t.Fatal("configured key was not recognized")
				}
				if got := vi.GetString(tc.key); got != "after" {
					t.Fatalf("%s = %q, want after", tc.key, got)
				}
			})
		}
	}
}

func TestSetKeyValueDoesNotCreateUnknownSettings(t *testing.T) {
	vi := viper.New()
	vi.SetDefault("log_level", "info")
	if SetKeyValue(vi, "GJ_UNKNOWN_SETTING", "value") {
		t.Fatal("unknown key was recognized")
	}
	if len(vi.AllKeys()) != 1 {
		t.Fatalf("unknown setting was added: %v", vi.AllKeys())
	}
}

func TestSetKeyValuePreservesExactKeyPrecedence(t *testing.T) {
	vi := viper.New()
	vi.SetDefault("a_b", "literal")
	vi.SetDefault("a.b", "nested")
	if !SetKeyValue(vi, "GJ_A_B", "after") {
		t.Fatal("exact key was not recognized")
	}
	if got := vi.GetString("a_b"); got != "after" {
		t.Fatalf("a_b = %q", got)
	}
	if got := vi.GetString("a.b"); got != "nested" {
		t.Fatalf("a.b = %q, want nested", got)
	}
}

func TestSetKeyValueRejectsAmbiguousFallback(t *testing.T) {
	vi := viper.New()
	vi.SetDefault("a_b.c_d", "first")
	vi.SetDefault("a_b_c.d", "second")
	if SetKeyValue(vi, "GJ_A_B_C_D", "after") {
		t.Fatal("ambiguous fallback was accepted")
	}
	if vi.GetString("a_b.c_d") != "first" || vi.GetString("a_b_c.d") != "second" {
		t.Fatal("ambiguous fallback changed settings")
	}
}
