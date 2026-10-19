package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Policy mirrors the deployment-wide install policy. Keep this in sync
// with apps/web/src/lib/install-policy-settings.ts (the write side) and
// packages/db/src/install-policy.ts (the read side).
//
// The CLI reads it from the worker's admin port before a git-URL skill
// install. When the worker isn't reachable it falls back to the
// most-restrictive default, so an operator can't turn on the git-URL
// path just by not running the worker.
type Policy struct {
	AllowGitURLInstalls bool `json:"allowGitUrlInstalls"`
}

// DefaultPolicy is the secure-by-default policy applied when the
// worker admin port is unreachable. Mirrors DEFAULT_INSTALL_POLICY on
// the TS side.
var DefaultPolicy = Policy{AllowGitURLInstalls: false}

// workerAdminBase returns the loopback URL of the worker admin port.
// WORKER_ADMIN_URL overrides for tests / non-default deployments.
func workerAdminBase() string {
	if v := os.Getenv("WORKER_ADMIN_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:4100"
}

// FetchPolicy reads the policy from the worker. Returns (policy, source)
// where source is "org" (read from DB), "default" (worker has no row),
// or "unreachable" (worker didn't answer, default applied). The CLI uses
// `source` only for diagnostics; behavior is the same: enforce the
// returned policy.
func FetchPolicy(ctx context.Context) (Policy, string, error) {
	endpoint, err := url.JoinPath(workerAdminBase(), "/admin/install-policy")
	if err != nil {
		return DefaultPolicy, "unreachable", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DefaultPolicy, "unreachable", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return DefaultPolicy, "unreachable", nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return DefaultPolicy, "unreachable", fmt.Errorf("worker /admin/install-policy: HTTP %d", res.StatusCode)
	}
	var body struct {
		Policy Policy `json:"policy"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return DefaultPolicy, "unreachable", err
	}
	source := body.Source
	if source == "" {
		source = "org"
	}
	return body.Policy, source, nil
}
