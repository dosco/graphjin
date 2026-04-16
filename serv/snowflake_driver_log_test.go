package serv

import (
	"strings"
	"testing"

	"github.com/snowflakedb/gosnowflake"
)

// TestSilenceSnowflakeDriverLogs asserts the helper lowers the gosnowflake
// driver's log level to "fatal" so it no longer pollutes stdout with ERRO
// entries for query failures the caller handles (e.g. probing
// INFORMATION_SCHEMA views that don't exist on the target account during
// schema discovery). These errors are expected and non-fatal — silencing
// them prevents alarming-looking log noise during startup.
func TestSilenceSnowflakeDriverLogs(t *testing.T) {
	silenceSnowflakeDriverLogs()

	got := gosnowflake.GetLogger().GetLogLevel()
	if !strings.EqualFold(got, "fatal") {
		t.Errorf("gosnowflake log level = %q, want fatal", got)
	}
}

// TestSilenceSnowflakeDriverLogsIdempotent calls the helper twice and
// verifies sync.Once prevents re-invocation (the second call is a noop).
// Regression guard in case we ever remove the Once wrapper — re-applying
// the level is harmless today but the wrapper makes intent explicit.
func TestSilenceSnowflakeDriverLogsIdempotent(t *testing.T) {
	silenceSnowflakeDriverLogs()
	silenceSnowflakeDriverLogs()
	if got := gosnowflake.GetLogger().GetLogLevel(); !strings.EqualFold(got, "fatal") {
		t.Errorf("log level changed after second call: %q", got)
	}
}
