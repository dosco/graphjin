package eval

import (
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Three gemini-flash runs on the frozen suite died at 85, 131 and 60 episodes,
// each abandoning a fully paid half-run. The cause was retry sizing: rate and
// quota limits are windows that refill on the order of a minute, and the fixed
// two-second delay meant the single transient retry fired into the same closed
// window every time.
func TestRetryDelayForCode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured time.Duration
		code       string
		want       time.Duration
	}{
		{"rate limit waits out the window", 0, gjagent.ErrorCodeProviderRateLimit, 75 * time.Second},
		{"quota waits out the window", 0, gjagent.ErrorCodeProviderQuota, 75 * time.Second},
		{"auth blip waits out billing propagation", 0, gjagent.ErrorCodeProviderAuth, 75 * time.Second},
		{"transport blip stays snappy", 0, gjagent.ErrorCodeProviderTransport, 2 * time.Second},
		{"server error stays snappy", 0, gjagent.ErrorCodeProviderServer, 2 * time.Second},
		{"configured delay survives for blips", 5 * time.Second, gjagent.ErrorCodeProviderTransport, 5 * time.Second},
		{"configured delay above the window wins", 2 * time.Minute, gjagent.ErrorCodeProviderQuota, 2 * time.Minute},
		// Deliberate configuration wins downward too: tests set nanosecond delays.
		{"configured delay below the window wins", time.Nanosecond, gjagent.ErrorCodeProviderAuth, time.Nanosecond},
	} {
		if got := retryDelayForCode(tc.configured, tc.code); got != tc.want {
			t.Errorf("%s: retryDelayForCode(%v, %s) = %v, want %v", tc.name, tc.configured, tc.code, got, tc.want)
		}
	}
}
