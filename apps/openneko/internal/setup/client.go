// Package setup drives OpenNeko's first-run admin onboarding from the terminal
// by calling the same web endpoints the browser wizard uses
// (apps/web/src/app/settings/SetupWizard.tsx). The web app is the single source
// of truth — this package adds no config-writing logic of its own, so the CLI
// and browser paths can never drift. Choosing "finish in the browser" simply
// skips the calls and prints the URL.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin typed wrapper over the web setup API at BaseURL.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient targets the local web app. The timeout is generous because the
// provider/data-source test endpoints make live upstream calls.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// ----- payload shapes (mirror SetupWizard.tsx) -----

type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Kind        string `json:"kind"` // text | secret | url
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

type ProviderOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type ProviderConfig struct {
	Scope    string         `json:"scope"`
	Source   string         `json:"source"` // org | env | default
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Enabled  bool           `json:"enabled"`
	Config   map[string]any `json:"config"`
}

type ProviderSettings struct {
	Primary  ProviderConfig `json:"primary"`
	Research ProviderConfig `json:"research"`
	Options  struct {
		Primary  []ProviderOption `json:"primary"`
		Research []ProviderOption `json:"research"`
	} `json:"options"`
	Defaults struct {
		Primary  map[string]string `json:"primary"`
		Research map[string]string `json:"research"`
	} `json:"defaults"`
	Fields struct {
		Primary  map[string][]Field `json:"primary"`
		Research map[string][]Field `json:"research"`
	} `json:"fields"`
}

type AgentSettings struct {
	Agent struct {
		Source    string `json:"source"`
		GlobalCap int    `json:"globalCap"`
	} `json:"agent"`
	Defaults struct {
		GlobalCap int `json:"globalCap"`
	} `json:"defaults"`
}

type DataSource struct {
	Source     string `json:"source"` // org | unset
	Kind       string `json:"kind"`
	GraphqlURL string `json:"graphqlUrl"`
	McpURL     string `json:"mcpUrl"`
	Label      string `json:"label"`
}

// ProviderDraft is the body for provider test + save (primary or research).
type ProviderDraft struct {
	Scope    string            `json:"scope"` // primary | research
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Enabled  bool              `json:"enabled"`
	Config   map[string]string `json:"config"`
	Secrets  map[string]string `json:"secrets"`
}

// ----- readiness -----

// Ready does one short-timeout probe (GET change-password is config-file
// backed, so it answers as soon as the web process is serving). 401/403
// count as ready too: an SSO-gated install answers exactly that for an
// unauthenticated CLI, and treating it as "down" made re-running setup
// burn the whole WaitReady budget against a perfectly healthy stack and
// then fail preflight with a misleading port-in-use error.
func (c *Client) Ready(ctx context.Context) bool {
	probe := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/admin/change-password", nil)
	resp, err := probe.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

// WaitReady polls Ready until the web app answers. Bring-up has already waited
// for neko-db + migrations, so once web answers the DB-backed endpoints work
// too.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if c.Ready(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("web app at %s did not become ready within %s", c.BaseURL, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// ----- state reads -----

func (c *Client) PasswordChanged(ctx context.Context) (bool, error) {
	var out struct {
		Changed bool `json:"changed"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/admin/change-password", nil, &out); err != nil {
		return false, err
	}
	return out.Changed, nil
}

func (c *Client) GetProvider(ctx context.Context) (*ProviderSettings, error) {
	var out ProviderSettings
	if err := c.do(ctx, http.MethodGet, "/api/settings/provider", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetAgent(ctx context.Context) (*AgentSettings, error) {
	var out AgentSettings
	if err := c.do(ctx, http.MethodGet, "/api/settings/agent", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetDataSource(ctx context.Context) (*DataSource, error) {
	var out DataSource
	if err := c.do(ctx, http.MethodGet, "/api/settings/data-source", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ----- mutations + gates -----

func (c *Client) ChangePassword(ctx context.Context, password string) error {
	return c.do(ctx, http.MethodPost, "/api/admin/change-password",
		map[string]string{"password": password}, nil)
}

// TestDataSource runs the same connectivity gate the wizard does. It returns
// mcpOk so callers can note an unreachable MCP (fine for the agent path)
// without treating it as failure.
func (c *Client) TestDataSource(ctx context.Context, graphqlURL, mcpURL string) (mcpOK bool, err error) {
	var out struct {
		MCPOk *bool `json:"mcpOk"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/settings/data-source/test",
		map[string]string{"graphqlUrl": graphqlURL, "mcpUrl": mcpURL}, &out); err != nil {
		return false, err
	}
	return out.MCPOk == nil || *out.MCPOk, nil
}

func (c *Client) SaveDataSource(ctx context.Context, graphqlURL, mcpURL, label string) error {
	return c.do(ctx, http.MethodPut, "/api/settings/data-source",
		map[string]string{"graphqlUrl": graphqlURL, "mcpUrl": mcpURL, "label": label}, nil)
}

// TestProvider validates a provider key with a real one-shot call — the same
// gate the wizard runs before saving.
func (c *Client) TestProvider(ctx context.Context, draft ProviderDraft) error {
	return c.do(ctx, http.MethodPost, "/api/settings/provider/test", draft, nil)
}

func (c *Client) SaveAgent(ctx context.Context, globalCap int) error {
	return c.do(ctx, http.MethodPut, "/api/settings/agent",
		map[string]any{"globalCap": globalCap}, nil)
}

func (c *Client) SaveProvider(ctx context.Context, draft ProviderDraft) error {
	return c.do(ctx, http.MethodPut, "/api/settings/provider", draft, nil)
}

func (c *Client) Finish(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/settings/finish", nil, nil)
}

// do issues a JSON request and decodes a JSON response. Non-2xx responses
// carry a {"error": "..."} body which is surfaced verbatim.
//
// It retries transient transport errors (EOF / connection refused) and 502-504
// for a short window: changing the admin password makes the web container
// restart (OPENNEKO_RESTART_WEB_ON_PASSWORD_CHANGE), so a request issued right
// after — e.g. the live provider-key test — can land mid-restart. Every endpoint
// here is idempotent, so retrying is safe.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = b
	}
	deadline := time.Now().Add(45 * time.Second)
	for attempt := 0; ; attempt++ {
		var rdr io.Reader
		if bodyBytes != nil {
			rdr = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
		if err != nil {
			return err
		}
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if time.Now().Before(deadline) && ctx.Err() == nil {
				if waitErr := sleepCtx(ctx, time.Second); waitErr != nil {
					return waitErr
				}
				continue // transport error (web likely restarting) — retry
			}
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 502 && resp.StatusCode <= 504 && time.Now().Before(deadline) {
			if waitErr := sleepCtx(ctx, time.Second); waitErr != nil {
				return waitErr
			}
			continue // transient gateway error during restart — retry
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var e struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(data, &e)
			if e.Error != "" {
				return errors.New(e.Error)
			}
			return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
		}
		if out != nil && len(data) > 0 {
			return json.Unmarshal(data, out)
		}
		return nil
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ----- endpoint derivation (ported from SetupWizard.tsx deriveRoot/Endpoints) -----

const (
	graphqlSuffix = "/api/v1/graphql"
	mcpSuffix     = "/api/v1/mcp"
)

// DeriveEndpoints accepts a bare root, a trailing-slash root, or a full
// GraphQL/MCP URL and returns the canonical graphql + mcp endpoints.
func DeriveEndpoints(rootURL string) (graphqlURL, mcpURL string) {
	root := deriveRoot(rootURL)
	return root + graphqlSuffix, root + mcpSuffix
}

func deriveRoot(input string) string {
	s := strings.TrimRight(strings.TrimSpace(input), "/")
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, graphqlSuffix):
		s = s[:len(s)-len(graphqlSuffix)]
	case strings.HasSuffix(lower, mcpSuffix):
		s = s[:len(s)-len(mcpSuffix)]
	}
	return strings.TrimRight(s, "/")
}
