package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	setupNoBrowser bool
	setupForce     bool
)

// setupCmd returns a fresh `setup` cobra.Command tree. Called once for
// `graphjin cli setup` and once for `graphjin mcp setup` so each subcommand
// has an independent parent — cobra requires a Command instance to have a
// single parent.
func setupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "setup [server-url]",
		Short: "Sign in and save server + token to ~/.config/graphjin/client.json",
		Long: `Point this CLI at a GraphJin server and (if the server has built-in login
enabled) sign in via OIDC. The resulting JWT and the server URL are persisted
to ~/.config/graphjin/client.json. Every subsequent ` + "`graphjin cli ...`" + ` and
` + "`graphjin mcp ...`" + ` invocation reads from this file — there is no --server
or --token flag anywhere else.

If the server has no built-in login (auth_login disabled), only the URL is
saved and an empty token is stored — useful for local development and for
servers protected by network-level auth.

The positional <server-url> argument is required on first setup. On subsequent
runs (refresh / re-sign-in) it can be omitted and the previously-saved URL
is reused.

Examples:
  graphjin cli setup http://localhost:8080
  graphjin cli setup https://graphjin.example.com
  graphjin cli setup                                 # refresh token, same server
  graphjin mcp setup https://graphjin.example.com    # alias for cli setup`,
		Args: cobra.MaximumNArgs(1),
		Run:  runSetup,
	}
	c.Flags().BoolVar(&setupNoBrowser, "no-browser", false,
		"Do not attempt to open the verification URL in a browser")
	c.Flags().BoolVar(&setupForce, "force", false,
		"Overwrite an existing client.json without confirmation")

	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the saved client config (token redacted)",
		Run:   runSetupShow,
	})
	c.AddCommand(&cobra.Command{
		Use:   "logout",
		Short: "Delete the saved client.json",
		Run:   runSetupLogout,
	})
	return c
}

// resolveSetupServer picks the server URL from the positional arg or the
// previously-saved client.json. Errors out if neither is available.
func resolveSetupServer(args []string) (string, error) {
	if len(args) == 1 {
		s := strings.TrimRight(strings.TrimSpace(args[0]), "/")
		if _, err := url.ParseRequestURI(s); err != nil {
			return "", fmt.Errorf("invalid server URL %q: %w", args[0], err)
		}
		return s, nil
	}
	if cc, _ := LoadClientConfig(); cc != nil && cc.Server != "" {
		return strings.TrimRight(cc.Server, "/"), nil
	}
	return "", errors.New("no server URL: pass it as the first argument, e.g. `graphjin cli setup http://localhost:8080`")
}

func runSetup(cmd *cobra.Command, args []string) {
	server, err := resolveSetupServer(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	// Refuse to overwrite a config bound to a *different* server unless
	// --force is given. Same-server "refresh" is fine: that's the normal
	// way to renew an expired token.
	if existing, _ := LoadClientConfig(); existing != nil && existing.Server != "" && existing.Server != server && !setupForce {
		fmt.Fprintf(os.Stderr, "warning: %s is already bound to %s; pass --force to switch to %s\n",
			mustPath(), existing.Server, server)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	ds, hasAuth, err := startDeviceFlow(ctx, server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: start device flow: %s\n", err)
		os.Exit(1)
	}

	// No-auth server: just save the URL and exit.
	if !hasAuth {
		cc := &ClientConfig{Server: server}
		if err := SaveClientConfig(cc); err != nil {
			fmt.Fprintf(os.Stderr, "error: save config: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("Server has no built-in login (auth_login disabled).\n")
		fmt.Printf("Saved %s with server URL only (no token).\n", mustPath())
		return
	}

	completeURL := ds.VerificationURIComplete
	if completeURL == "" {
		completeURL = ds.VerificationURI
	}

	fmt.Println()
	fmt.Println("  Open this URL in your browser and confirm the code below:")
	fmt.Printf("    URL:  %s\n", completeURL)
	fmt.Printf("    Code: %s\n", ds.UserCode)
	fmt.Println()

	if !setupNoBrowser {
		_ = tryOpenBrowser(completeURL)
	}

	interval := time.Duration(ds.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresAt := time.Now().Add(time.Duration(ds.ExpiresIn) * time.Second)

	for {
		if time.Now().After(expiresAt) {
			fmt.Fprintln(os.Stderr, "error: device code expired; please re-run setup")
			os.Exit(1)
		}
		time.Sleep(interval)
		tr, done, err := pollDeviceToken(ctx, server, ds.DeviceCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			os.Exit(1)
		}
		if !done {
			continue
		}

		cc := &ClientConfig{
			Server:    server,
			Token:     tr.Token,
			ExpiresAt: time.Unix(tr.ExpiresAt, 0),
			Issuer:    tr.Issuer,
			Email:     tr.Email,
		}
		if err := SaveClientConfig(cc); err != nil {
			fmt.Fprintf(os.Stderr, "error: save config: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("Signed in as %s\n", tr.Email)
		fmt.Printf("Saved %s (expires %s)\n", mustPath(), cc.ExpiresAt.Format(time.RFC1123))
		return
	}
}

func runSetupShow(cmd *cobra.Command, args []string) {
	cc, err := LoadClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	if cc == nil {
		fmt.Println("(no client.json — run `graphjin cli setup <server-url>`)")
		return
	}
	redacted := *cc
	if len(redacted.Token) > 12 {
		redacted.Token = redacted.Token[:8] + "…" + redacted.Token[len(redacted.Token)-4:]
	} else if redacted.Token != "" {
		redacted.Token = "…"
	}
	b, _ := json.MarshalIndent(redacted, "", "  ")
	fmt.Println(string(b))
	fmt.Printf("\nPath: %s\n", mustPath())
}

func runSetupLogout(cmd *cobra.Command, args []string) {
	if err := DeleteClientConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("Logged out.")
}

// ---- device-code HTTP client ----

type deviceStartResp struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceTokenResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Email     string `json:"email"`
	Issuer    string `json:"issuer"`
	Error     string `json:"error,omitempty"`
	ErrorDesc string `json:"error_description,omitempty"`
}

// startDeviceFlow returns (deviceStartResp, hasAuth, error). hasAuth is
// false when the server's auth_login is disabled — detected via 404 / 405 on
// the device endpoint, in which case the caller should save the server URL
// alone (no token).
func startDeviceFlow(ctx context.Context, server string) (*deviceStartResp, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/v1/auth/device", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		// When auth_login is disabled the device route isn't registered, but
		// the SPA fallback handler may serve the GraphQL editor's index.html
		// at this path — a 200 with text/html. Treat any non-JSON 200 as
		// "no built-in login" rather than failing to decode.
		if !isJSONResponse(resp, b) {
			return nil, false, nil
		}
		var ds deviceStartResp
		if err := json.Unmarshal(b, &ds); err != nil {
			return nil, false, fmt.Errorf("decode response: %w", err)
		}
		return &ds, true, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		// auth_login isn't enabled on this server. The mux returns 404 when
		// the route was never registered; some intermediaries may rewrite
		// to 405. Either way, treat it as "no built-in login".
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
}

// isJSONResponse reports whether the response is JSON, by Content-Type or
// by sniffing the first non-whitespace byte of the body. The body sniff
// catches misconfigured servers/proxies that omit or lie about Content-Type.
func isJSONResponse(resp *http.Response, body []byte) bool {
	ct := resp.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if strings.EqualFold(strings.TrimSpace(ct), "application/json") {
		return true
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

// pollDeviceToken polls once. Returns (token, true, nil) when the user has
// completed sign-in, (nil, false, nil) to keep polling, and (nil, false, err)
// on terminal failure.
func pollDeviceToken(ctx context.Context, server, deviceCode string) (*deviceTokenResp, bool, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/v1/auth/device/token", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var tr deviceTokenResp
	_ = json.Unmarshal(raw, &tr)

	switch {
	case resp.StatusCode == http.StatusOK && tr.Token != "":
		return &tr, true, nil
	case tr.Error == "authorization_pending":
		return nil, false, nil
	case tr.Error == "slow_down":
		return nil, false, nil
	case tr.Error == "expired_token":
		return nil, false, errors.New("device code expired; please re-run setup")
	case tr.Error == "access_denied":
		desc := tr.ErrorDesc
		if desc == "" {
			desc = "access denied"
		}
		return nil, false, fmt.Errorf("%s", desc)
	case tr.Error != "":
		return nil, false, fmt.Errorf("%s", tr.Error)
	default:
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

// tryOpenBrowser best-effort opens a URL in the user's default browser.
func tryOpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func mustPath() string {
	p, err := ClientConfigPath()
	if err != nil {
		return "~/.config/graphjin/client.json"
	}
	return p
}
