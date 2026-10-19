package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/open-neko/openneko/apps/ax-harness/internal/localtool"
)

// Client sends one run's terminal commands to the sandbox.
type Client struct {
	http      *http.Client
	run       string
	workspace string
}

// NewClient returns a client for a new run whose commands run in workspace.
func NewClient(socket, workspace string) *Client {
	var id [12]byte
	_, _ = rand.Read(id[:])
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{http: &http.Client{Transport: transport}, run: "run-" + hex.EncodeToString(id[:]), workspace: workspace}
}

// Exec runs one command in the sandbox.
func (c *Client) Exec(ctx context.Context, command, skillDir string, env map[string]string, timeoutSeconds, maxOutput int) (localtool.ExecResult, error) {
	body, _ := json.Marshal(ExecRequest{Workspace: c.workspace, Command: command, SkillDir: skillDir, Env: env, TimeoutSeconds: timeoutSeconds, MaxOutput: maxOutput})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://sandbox/v1/runs/"+url.PathEscape(c.run)+"/exec", bytes.NewReader(body))
	if err != nil {
		return localtool.ExecResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return localtool.ExecResult{}, ctx.Err()
		}
		return localtool.ExecResult{}, fmt.Errorf("terminal unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return localtool.ExecResult{}, fmt.Errorf("terminal refused the command: %s", bytes.TrimSpace(msg))
	}
	var result localtool.ExecResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return localtool.ExecResult{}, fmt.Errorf("terminal answered badly: %w", err)
	}
	return result, nil
}

// Close ends the run: the sandbox kills its commands and removes its home.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://sandbox/v1/runs/"+url.PathEscape(c.run), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	c.http.CloseIdleConnections()
	return nil
}
