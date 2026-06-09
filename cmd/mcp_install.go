package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

const (
	defaultMCPServerURL = "http://localhost:8080"
	graphjinMCPName     = "graphjin"
	claudeMCPServerName = "GraphJin"
)

var (
	lookPathFn                = exec.LookPath
	commandContextFn          = exec.CommandContext
	resolveGraphJinPathForMCP = resolveGraphJinBinaryPath
	mcpProbeHTTPClient        = &http.Client{Timeout: 5 * time.Second}
	startDeviceFlowFn         = startDeviceFlow
	pollDeviceTokenFn         = pollDeviceToken
	saveClientConfigFn        = SaveClientConfig
)

type mcpInstallOptions struct {
	Client      string
	Scope       string
	Server      string
	BaseServer  string
	Mode        string
	Yes         bool
	NoBrowser   bool
	ConfigPath  string
	DeviceStart *deviceStartResp
}

type mcpInstallResolveInput struct {
	Client      string
	ClientSet   bool
	Scope       string
	ScopeSet    bool
	Server      string
	ServerSet   bool
	Global      bool
	Yes         bool
	NoBrowser   bool
	Interactive bool
	ForceClient string
	PromptFn    func(kind, prompt string, options []string, defaultValue string) (string, error)
}

type codexInstallPlan struct {
	UseCLI         bool
	AddArgs        []string
	ConfigPath     string
	ScopeSupported bool
}

type codexServerConfig struct {
	Command string   `toml:"command,omitempty"`
	Args    []string `toml:"args,omitempty"`
	URL     string   `toml:"url,omitempty"`
}

const (
	mcpInstallModeDirect = "direct"
	mcpInstallModeProxy  = "proxy"
)

type mcpProbeKind string

const (
	mcpProbeNoAuth      mcpProbeKind = "no_auth"
	mcpProbeOAuth       mcpProbeKind = "oauth"
	mcpProbeAuthLogin   mcpProbeKind = "auth_login"
	mcpProbeUnsupported mcpProbeKind = "unsupported_auth"
)

type mcpProbeResult struct {
	Kind        mcpProbeKind
	DeviceStart *deviceStartResp
	Reason      string
}

type mcpInstallCommandConfig struct {
	Use        string
	Short      string
	Long       string
	Deprecated string

	ForceClient string
	HideClient  bool
}

func mcpAddCmd() *cobra.Command {
	return newMCPInstallCommand(mcpInstallCommandConfig{
		Use:   "add [client] [server-url]",
		Short: "Add GraphJin to Claude Code or OpenAI Codex",
		Long: `Add GraphJin to an MCP-capable AI client.

Defaults:
  client: codex
  server: http://localhost:8080
  scope:  project

Examples:
  graphjin mcp add
  graphjin mcp add claude
  graphjin mcp add all https://graphjin.example.com
  graphjin mcp add codex http://localhost:8080 --global

The server URL is normalized to /api/v1/mcp. If the server exposes standards
OAuth, the native MCP client handles login. If the server uses GraphJin's
legacy auth_login flow, this command signs in once and installs a credential-free
local proxy config.`,
	})
}

func mcpInstallCmd() *cobra.Command {
	return newMCPInstallCommand(mcpInstallCommandConfig{
		Use:        "install [client] [server-url]",
		Short:      "Alias for `graphjin mcp add`",
		Deprecated: "use `graphjin mcp add` instead",
		Long: `Backward-compatible alias for adding GraphJin to Claude Code, OpenAI Codex, or all supported clients.

Equivalent to:
  graphjin mcp add [client] [server-url]`,
	})
}

func newMCPInstallCommand(cfg mcpInstallCommandConfig) *cobra.Command {
	var client string
	var scope string
	var server string
	var global bool
	var yes bool
	var noBrowser bool

	c := &cobra.Command{
		Use:        cfg.Use,
		Short:      cfg.Short,
		Long:       cfg.Long,
		Deprecated: cfg.Deprecated,
		Args:       cobra.MaximumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			absConfigPath, err := filepath.Abs(cpath)
			if err != nil {
				log.Fatalf("failed to get absolute config path: %s", err)
			}

			posClient := ""
			posServer := ""
			if len(args) > 0 {
				if looksLikeURLish(args[0]) {
					posServer = args[0]
				} else {
					posClient = args[0]
				}
			}
			if len(args) > 1 {
				posServer = args[1]
			}
			if posClient != "" && !cmd.Flags().Changed("client") {
				client = posClient
			}
			if posServer != "" && !cmd.Flags().Changed("server") {
				server = posServer
			}
			serverSet := cmd.Flags().Changed("server") || posServer != ""

			interactive := isInteractiveTTY() && !yes
			promptFn := promptChoiceFn(nil)
			if interactive {
				promptFn = promptChoiceFn(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()))
			}

			opts, err := resolveInstallOptions(mcpInstallResolveInput{
				Client:      client,
				ClientSet:   cmd.Flags().Changed("client"),
				Scope:       scope,
				ScopeSet:    cmd.Flags().Changed("scope"),
				Server:      server,
				ServerSet:   serverSet,
				Global:      global,
				Yes:         yes,
				NoBrowser:   noBrowser,
				Interactive: interactive,
				ForceClient: cfg.ForceClient,
				PromptFn:    promptFn,
			})
			if err != nil {
				log.Fatalf("%s", err)
			}

			opts.ConfigPath = absConfigPath

			ctx := cmd.Context()
			probe, err := probeMCPServer(ctx, opts.Server, opts.BaseServer)
			if err != nil {
				log.Fatalf("%s", err)
			}
			if err := applyMCPProbeResult(ctx, cmd, &opts, probe); err != nil {
				log.Fatalf("%s", err)
			}

			if err := validateInstallPrereqs(opts); err != nil {
				log.Fatalf("%s", err)
			}

			var codexPlan codexInstallPlan
			if usesCodex(opts.Client) {
				codexPlan, err = buildCodexInstallPlan(cmd, opts)
				if err != nil {
					log.Fatalf("failed to build codex install plan: %s", err)
				}
			}

			if interactive {
				printInstallPreview(cmd.OutOrStdout(), opts)
				ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()),
					"Proceed with MCP add?", false)
				if err != nil {
					log.Fatalf("failed to read confirmation: %s", err)
				}
				if !ok {
					log.Infof("Aborted")
					return
				}
			}

			if usesClaude(opts.Client) {
				if err := runClaudeInstall(cmd, opts); err != nil {
					log.Fatalf("Claude install failed: %s", err)
				}
			}

			if usesCodex(opts.Client) {
				if err := runCodexInstall(cmd, opts, codexPlan); err != nil {
					log.Fatalf("Codex install failed: %s", err)
				}
			}

			printPostInstallGuide(cmd.OutOrStdout(), opts, codexPlan)
		},
	}

	c.Flags().StringVar(&client, "client", "", "Target client: claude, codex, or all")
	c.Flags().StringVar(&scope, "scope", "", "Install scope: project, global, or local")
	c.Flags().StringVar(&server, "server", "", "GraphJin server URL (default http://localhost:8080)")
	c.Flags().BoolVar(&global, "global", false, "Install globally (shortcut for --scope global)")
	c.Flags().BoolVar(&yes, "yes", false, "Skip interactive prompts and confirmation")
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not attempt to open the verification URL during legacy auth_login")

	if cfg.HideClient {
		c.Flags().MarkHidden("client") //nolint:errcheck
	}

	return c
}

func resolveInstallOptions(in mcpInstallResolveInput) (mcpInstallOptions, error) {
	var opts mcpInstallOptions
	opts.Yes = in.Yes
	opts.NoBrowser = in.NoBrowser

	clientValue := in.Client
	if in.ForceClient != "" {
		clientValue = in.ForceClient
	} else if !in.ClientSet {
		if in.Interactive && in.PromptFn != nil {
			v, err := in.PromptFn(
				"client",
				"Select MCP target client",
				[]string{"codex", "claude", "all"},
				"codex",
			)
			if err != nil {
				return opts, err
			}
			clientValue = v
		} else {
			clientValue = "codex"
		}
	}

	scopeValue := in.Scope
	if in.Global {
		scopeValue = "global"
		in.ScopeSet = true
	}
	if !in.ScopeSet {
		if in.Interactive && in.PromptFn != nil {
			v, err := in.PromptFn(
				"scope",
				"Select install scope",
				[]string{"project", "global", "local"},
				"project",
			)
			if err != nil {
				return opts, err
			}
			scopeValue = v
		} else {
			scopeValue = "project"
		}
	}

	serverValue := in.Server
	if !in.ServerSet {
		serverValue = defaultMCPServerURL
	}
	if serverValue == "" {
		serverValue = defaultMCPServerURL
	}

	client, err := normalizeInstallClient(clientValue)
	if err != nil {
		return opts, err
	}

	scope, err := normalizeInstallScope(scopeValue)
	if err != nil {
		return opts, err
	}

	mcpURL, err := normalizeMCPAddURL(serverValue)
	if err != nil {
		return opts, fmt.Errorf("invalid server URL %q: %w", serverValue, err)
	}
	baseURL, err := baseServerURLForMCP(mcpURL)
	if err != nil {
		return opts, err
	}

	opts.Client = client
	opts.Scope = scope
	opts.Server = mcpURL
	opts.BaseServer = baseURL
	opts.Mode = mcpInstallModeDirect

	return opts, nil
}

func looksLikeURLish(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "://") ||
		strings.HasPrefix(v, "localhost") ||
		strings.HasPrefix(v, "127.") ||
		strings.HasPrefix(v, "[::1]") ||
		strings.HasPrefix(v, "::1") ||
		strings.Contains(v, ".")
}

func normalizeMCPAddURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		input = defaultMCPServerURL
	}
	if !strings.Contains(input, "://") {
		input = "http://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch {
	case path == "":
		u.Path = routeMCPPath
	case path == "/api/v1":
		u.Path = routeMCPPath
	case path == routeMCPMessagePath:
		u.Path = routeMCPPath
	case path == routeMCPPath:
		u.Path = routeMCPPath
	default:
		u.Path = path
	}
	return u.String(), nil
}

const (
	routeMCPPath        = "/api/v1/mcp"
	routeMCPMessagePath = "/api/v1/mcp/message"
)

func baseServerURLForMCP(mcpURL string) (string, error) {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch path {
	case routeMCPPath:
		u.Path = strings.TrimSuffix(path, routeMCPPath)
	case routeMCPMessagePath:
		u.Path = strings.TrimSuffix(path, routeMCPMessagePath)
	default:
		u.Path = ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func probeMCPServer(ctx context.Context, mcpURL, baseServer string) (mcpProbeResult, error) {
	reqBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"graphjin-cli","version":"0.0.0"}}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
	if err != nil {
		return mcpProbeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := mcpProbeHTTPClient.Do(req)
	if err != nil {
		return mcpProbeResult{}, fmt.Errorf("could not reach %s: %w\nstart GraphJin and re-run `graphjin mcp add`, or pass the hosted server URL", mcpURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if hasStandardsOAuthChallenge(resp.Header.Get("WWW-Authenticate")) {
			return mcpProbeResult{Kind: mcpProbeOAuth}, nil
		}
		ds, hasAuth, err := startDeviceFlowFn(ctx, baseServer)
		if err != nil {
			return mcpProbeResult{}, fmt.Errorf("MCP endpoint requires auth, but GraphJin auth_login probe failed: %w", err)
		}
		if hasAuth {
			return mcpProbeResult{Kind: mcpProbeAuthLogin, DeviceStart: ds}, nil
		}
		return mcpProbeResult{
			Kind:   mcpProbeUnsupported,
			Reason: strings.TrimSpace(string(body)),
		}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return mcpProbeResult{}, fmt.Errorf("MCP endpoint not found at %s; GraphJin expects /api/v1/mcp", mcpURL)
	}
	if resp.StatusCode >= 500 {
		return mcpProbeResult{}, fmt.Errorf("MCP endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return mcpProbeResult{Kind: mcpProbeNoAuth}, nil
}

func hasStandardsOAuthChallenge(v string) bool {
	v = strings.ToLower(v)
	return strings.Contains(v, "resource_metadata=") || strings.Contains(v, "resource_metadata=\"")
}

func applyMCPProbeResult(ctx context.Context, cmd *cobra.Command, opts *mcpInstallOptions, probe mcpProbeResult) error {
	opts.DeviceStart = probe.DeviceStart
	switch probe.Kind {
	case mcpProbeNoAuth, mcpProbeOAuth:
		opts.Mode = mcpInstallModeDirect
		return nil
	case mcpProbeAuthLogin:
		opts.Mode = mcpInstallModeProxy
		return completeMCPDeviceLogin(ctx, cmd, opts)
	case mcpProbeUnsupported:
		msg := "MCP endpoint requires authentication, but it did not advertise OAuth and GraphJin auth_login is not enabled."
		if probe.Reason != "" {
			msg += " Server said: " + probe.Reason
		}
		return errors.New(msg + "\nEnable mcp.oauth for hosted OAuth, enable auth_login for the GraphJin helper, or configure the AI client manually with the required headers.")
	default:
		return fmt.Errorf("unknown MCP probe result %q", probe.Kind)
	}
}

func completeMCPDeviceLogin(ctx context.Context, cmd *cobra.Command, opts *mcpInstallOptions) error {
	ds := opts.DeviceStart
	if ds == nil {
		var hasAuth bool
		var err error
		ds, hasAuth, err = startDeviceFlowFn(ctx, opts.BaseServer)
		if err != nil {
			return fmt.Errorf("start device login: %w", err)
		}
		if !hasAuth {
			return errors.New("auth_login disappeared while starting device login")
		}
	}

	completeURL := ds.VerificationURIComplete
	if completeURL == "" {
		completeURL = ds.VerificationURI
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintln(out, "GraphJin requires sign-in for this MCP endpoint.")
	fmt.Fprintln(out, "Open this URL in your browser and confirm the code below:")
	fmt.Fprintf(out, "  URL:  %s\n", completeURL)
	fmt.Fprintf(out, "  Code: %s\n", ds.UserCode)
	fmt.Fprintln(out)

	if !opts.NoBrowser {
		_ = tryOpenBrowser(completeURL)
	}

	interval := time.Duration(ds.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresAt := time.Now().Add(time.Duration(ds.ExpiresIn) * time.Second)
	if ds.ExpiresIn <= 0 {
		expiresAt = time.Now().Add(10 * time.Minute)
	}

	for {
		if time.Now().After(expiresAt) {
			return errors.New("device code expired; re-run `graphjin mcp add`")
		}
		time.Sleep(interval)
		tr, done, err := pollDeviceTokenFn(ctx, opts.BaseServer, ds.DeviceCode)
		if err != nil {
			return err
		}
		if !done {
			continue
		}

		cc := &ClientConfig{
			Server:    opts.BaseServer,
			Token:     tr.Token,
			ExpiresAt: time.Unix(tr.ExpiresAt, 0),
			Issuer:    tr.Issuer,
			Email:     tr.Email,
		}
		if err := saveClientConfigFn(cc); err != nil {
			return fmt.Errorf("save client config: %w", err)
		}
		if tr.Email != "" {
			fmt.Fprintf(out, "Signed in as %s\n", tr.Email)
		}
		fmt.Fprintf(out, "Saved %s for token refresh.\n", mustPath())
		return nil
	}
}

func normalizeInstallClient(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "claude", "codex":
		return v, nil
	case "all", "both":
		return "all", nil
	default:
		return "", fmt.Errorf("invalid --client %q (valid: claude, codex, all)", v)
	}
}

func normalizeInstallScope(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "project", "global", "local":
		return v, nil
	case "user":
		return "global", nil
	default:
		return "", fmt.Errorf("invalid --scope %q (valid: project, global, local)", v)
	}
}

func validateInstallPrereqs(opts mcpInstallOptions) error {
	if usesClaude(opts.Client) {
		if _, err := lookPathFn("claude"); err != nil {
			return errors.New("Claude CLI not found in PATH. Install Claude Code CLI or use --client codex")
		}
	}

	return nil
}

func buildCodexInstallPlan(cmd *cobra.Command, opts mcpInstallOptions) (codexInstallPlan, error) {
	supportsScope, err := detectCodexScopeSupport(cmd)
	if err != nil {
		// For compatibility with older CLIs or unusual output, continue with no-scope fallback.
		log.Infof("Codex scope detection failed, using compatibility mode: %s", err)
		supportsScope = false
	}

	wd, err := os.Getwd()
	if err != nil {
		return codexInstallPlan{}, err
	}

	if supportsScope {
		return codexInstallPlan{
			UseCLI:         true,
			ScopeSupported: supportsScope,
			AddArgs:        buildCodexAddArgs(opts, supportsScope),
		}, nil
	}

	targetPath, err := codexConfigTargetPath(opts.Scope, wd)
	if err != nil {
		return codexInstallPlan{}, err
	}

	return codexInstallPlan{
		UseCLI:         false,
		ScopeSupported: supportsScope,
		ConfigPath:     targetPath,
	}, nil
}

func detectCodexScopeSupport(cmd *cobra.Command) (bool, error) {
	out, err := runExternalCommandOutput(cmd, "codex", "mcp", "add", "--help")
	if err != nil && strings.TrimSpace(out) == "" {
		return false, err
	}
	return codexHelpHasScope(out), nil
}

func codexHelpHasScope(helpText string) bool {
	return strings.Contains(helpText, "--scope")
}

func buildCodexAddArgs(opts mcpInstallOptions, includeScope bool) []string {
	args := []string{"mcp", "add", graphjinMCPName}
	if includeScope {
		args = append(args, "--scope", codexScopeValue(opts.Scope))
	}

	if opts.Mode == mcpInstallModeProxy {
		args = append(args, "--", graphjinCommandForMCP(), "mcp")
	} else {
		args = append(args, "--url", opts.Server)
	}
	return args
}

func buildCodexRemoveArgs(opts mcpInstallOptions, includeScope bool) []string {
	args := []string{"mcp", "remove", graphjinMCPName}
	if includeScope {
		args = append(args, "--scope", codexScopeValue(opts.Scope))
	}
	return args
}

func codexScopeValue(scope string) string {
	switch scope {
	case "global":
		return "user"
	default:
		return scope
	}
}

func codexConfigTargetPath(scope, wd string) (string, error) {
	switch scope {
	case "project", "local":
		return filepath.Join(wd, ".codex", "config.toml"), nil
	case "global":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".codex", "config.toml"), nil
	default:
		return "", fmt.Errorf("unsupported fallback scope %q", scope)
	}
}

func runClaudeInstall(cmd *cobra.Command, opts mcpInstallOptions) error {
	if opts.Mode == mcpInstallModeDirect {
		return runClaudeMCPAddURLInstall(cmd, opts)
	}
	return runClaudeMCPAddProxyInstall(cmd, opts)
}

func runClaudeMCPAddURLInstall(cmd *cobra.Command, opts mcpInstallOptions) error {
	claudeScope := normalizeClaudeScope(opts.Scope)
	_ = runExternalCommand(cmd, "claude", "mcp", "remove", "--scope", claudeScope, claudeMCPServerName)
	return runExternalCommand(cmd, "claude", "mcp", "add", "--transport", "http", "--scope", claudeScope, claudeMCPServerName, opts.Server)
}

func runClaudeMCPAddProxyInstall(cmd *cobra.Command, opts mcpInstallOptions) error {
	graphjinPath, err := resolveGraphJinPathForMCP()
	if err != nil {
		return err
	}

	claudeScope := normalizeClaudeScope(opts.Scope)
	// Best effort: remove existing config to allow deterministic updates.
	_ = runExternalCommand(cmd, "claude", "mcp", "remove", "--scope", claudeScope, claudeMCPServerName)

	addArgs := []string{"mcp", "add", "--scope", claudeScope, claudeMCPServerName, "--", graphjinPath}
	addArgs = append(addArgs, buildClaudeMCPServerArgs(opts)...)
	return runExternalCommand(cmd, "claude", addArgs...)
}

func resolveGraphJinBinaryPath() (string, error) {
	if p, err := lookPathFn("graphjin"); err == nil {
		return p, nil
	}

	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to locate graphjin executable: %w", err)
	}
	return p, nil
}

// buildClaudeMCPServerArgs returns the args Claude should use to spawn
// `graphjin mcp`. The server URL is intentionally NOT included — `graphjin
// mcp` reads it from ~/.config/graphjin/client.json, so the MCP client
// config we write here stays credential-free and survives token rotation
// (re-running `graphjin mcp setup` is enough; no MCP-client edits needed).
func buildClaudeMCPServerArgs(opts mcpInstallOptions) []string {
	_ = opts
	return []string{"mcp"}
}

func normalizeClaudeScope(scope string) string {
	switch scope {
	case "global":
		return "user"
	default:
		return scope
	}
}

func runCodexInstall(cmd *cobra.Command, opts mcpInstallOptions, plan codexInstallPlan) error {
	if plan.UseCLI {
		// Best effort: remove existing config entry so re-runs update instead of duplicate.
		_ = runExternalCommand(cmd, "codex", buildCodexRemoveArgs(opts, plan.ScopeSupported)...)
		return runExternalCommand(cmd, "codex", plan.AddArgs...)
	}

	entry := codexServerConfigFromOptions(opts)
	return writeCodexConfig(plan.ConfigPath, graphjinMCPName, entry)
}

func codexServerConfigFromOptions(opts mcpInstallOptions) codexServerConfig {
	if opts.Mode == mcpInstallModeProxy {
		return codexServerConfig{Command: graphjinCommandForMCP(), Args: []string{"mcp"}}
	}
	return codexServerConfig{URL: opts.Server}
}

func graphjinCommandForMCP() string {
	p, err := resolveGraphJinPathForMCP()
	if err != nil || p == "" {
		return "graphjin"
	}
	return p
}

func writeCodexConfig(path, serverName string, cfg codexServerConfig) error {
	var current []byte
	if b, err := os.ReadFile(path); err == nil {
		current = b
	} else if !os.IsNotExist(err) {
		return err
	}

	updated, err := upsertCodexConfig(current, serverName, cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o600)
}

func upsertCodexConfig(data []byte, serverName string, cfg codexServerConfig) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return nil, err
		}
	}

	mcpServers := toStringAnyMap(root["mcp_servers"])
	if mcpServers == nil {
		mcpServers = map[string]any{}
	}

	server := map[string]any{}
	if cfg.Command != "" {
		server["command"] = cfg.Command
	}
	if len(cfg.Args) != 0 {
		server["args"] = cfg.Args
	}
	if cfg.URL != "" {
		server["url"] = cfg.URL
	}
	mcpServers[serverName] = server
	root["mcp_servers"] = mcpServers

	return toml.Marshal(root)
}

func toStringAnyMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func usesClaude(client string) bool {
	return client == "claude" || client == "all"
}

func usesCodex(client string) bool {
	return client == "codex" || client == "all"
}

func printInstallPreview(w io.Writer, opts mcpInstallOptions) {
	fmt.Fprintf(w, "Install target: %s\n", opts.Client)
	fmt.Fprintf(w, "Scope: %s\n", opts.Scope)
	fmt.Fprintf(w, "Server: %s\n\n", opts.Server)
	if opts.Mode == mcpInstallModeProxy {
		fmt.Fprintf(w, "Mode: local GraphJin proxy (legacy auth_login token)\n\n")
	} else {
		fmt.Fprintf(w, "Mode: native MCP URL\n\n")
	}
}

func printPostInstallGuide(w io.Writer, opts mcpInstallOptions, codexPlan codexInstallPlan) {
	fmt.Fprintf(w, "GraphJin MCP connection added.\n")
	fmt.Fprintf(w, "Server: %s\n", opts.Server)

	if usesClaude(opts.Client) {
		fmt.Fprintf(w, "\nClaude Desktop / Claude Code\n")
		fmt.Fprintf(w, "  1) Restart Claude Desktop.\n")
		fmt.Fprintf(w, "  2) Verify with: claude mcp list\n")
	}

	if usesCodex(opts.Client) {
		fmt.Fprintf(w, "\nOpenAI Codex\n")
		fmt.Fprintf(w, "  1) Start a new Codex session.\n")
		fmt.Fprintf(w, "  2) Verify with: codex mcp list\n")
		if !codexPlan.UseCLI {
			fmt.Fprintf(w, "  3) Config written to: %s\n", codexPlan.ConfigPath)
		}
	}

	fmt.Fprintln(w)
}

func runExternalCommand(cmd *cobra.Command, name string, args ...string) error {
	c := commandContextFn(cmd.Context(), name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runExternalCommandOutput(cmd *cobra.Command, name string, args ...string) (string, error) {
	c := commandContextFn(cmd.Context(), name, args...)
	b, err := c.CombinedOutput()
	return string(b), err
}

type promptIO struct {
	in  *bufio.Reader
	out io.Writer
}

func newPromptIO(in io.Reader, out io.Writer) *promptIO {
	return &promptIO{
		in:  bufio.NewReader(in),
		out: out,
	}
}

func promptChoiceFn(pio *promptIO) func(kind, prompt string, options []string, defaultValue string) (string, error) {
	if pio == nil {
		return nil
	}

	return func(kind, prompt string, options []string, defaultValue string) (string, error) {
		return promptChoice(pio, prompt, options, defaultValue)
	}
}

func promptChoice(pio *promptIO, prompt string, options []string, defaultValue string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("prompt options cannot be empty")
	}

	var defaultIndex int
	for i, option := range options {
		if option == defaultValue {
			defaultIndex = i
			break
		}
	}

	for {
		fmt.Fprintf(pio.out, "%s\n", prompt)
		for i, option := range options {
			marker := " "
			if i == defaultIndex {
				marker = "*"
			}
			fmt.Fprintf(pio.out, "  %d) [%s] %s\n", i+1, marker, option)
		}
		fmt.Fprintf(pio.out, "Select option (default %d): ", defaultIndex+1)

		line, err := pio.in.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return options[defaultIndex], nil
		}

		if index, err := strconv.Atoi(line); err == nil {
			if index > 0 && index <= len(options) {
				return options[index-1], nil
			}
		}

		for _, option := range options {
			if strings.EqualFold(line, option) {
				return option, nil
			}
		}

		fmt.Fprintln(pio.out, "Invalid selection, try again.")
	}
}

func promptConfirm(pio *promptIO, prompt string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}

	fmt.Fprintf(pio.out, "%s [%s]: ", prompt, defaultLabel)
	line, err := pio.in.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return defaultYes, nil
	}
	return line == "y" || line == "yes", nil
}

func isInteractiveTTY() bool {
	si, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	so, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (si.Mode()&os.ModeCharDevice) != 0 && (so.Mode()&os.ModeCharDevice) != 0
}
