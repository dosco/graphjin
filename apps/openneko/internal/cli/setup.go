package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/preflight"
	"github.com/open-neko/neko/apps/openneko/internal/prompt"
	"github.com/open-neko/neko/apps/openneko/internal/setup"
	"github.com/open-neko/neko/apps/openneko/internal/ui"
)

func newSetupCmd() *cobra.Command {
	var (
		mode           string
		verbose        bool
		skipOnboarding bool
		webPort        int
		openShellPort  int
		webBindAddress string
		dockerSubnet   string

		// Headless onboarding overrides. Setting any credential flag opts the
		// run into headless mode (no prompts), so CI and the demo-install skill
		// can configure without a browser.
		adminPassword    string
		legacyBackend    string
		provider         string
		providerKey      string
		model            string
		dataURL          string
		researchProvider string
		researchKey      string
		noResearch       bool

		skipPlugins bool
		pluginsCSV  string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Guided install: preflight, bring up the stack, then configure",
		Long: `Guided first-run install.

  1. Preflight — Docker daemon up, host supported, required ports free.
  2. Bring up the stack (same staged flow as ` + "`openneko start`" + `).
  3. Configure — admin password, data source, agent + provider, research.

Step 3 runs in the terminal, or pass --skip-onboarding (or just choose "browser"
at the prompt) to finish at the web UI. Credential flags
(--admin-password/--provider/--provider-key/…) run step 3 headless for CI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateLegacyAgentBackend(legacyBackend); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			settings, err := prepareSetupInstallation(ctx, cmd, mode, setupHostOptions{
				webPort:             webPort,
				webPortChanged:      cmd.Flags().Changed("port"),
				openShellPort:       openShellPort,
				openShellChanged:    cmd.Flags().Changed("openshell-port"),
				webBindAddress:      webBindAddress,
				webBindChanged:      cmd.Flags().Changed("bind-address"),
				dockerSubnet:        dockerSubnet,
				dockerSubnetChanged: cmd.Flags().Changed("docker-subnet"),
			})
			if err != nil {
				return err
			}
			m := compose.Mode(settings.Mode)
			baseURL := webBaseURL()
			client := setup.NewClient(baseURL)
			alreadyUp := client.Ready(ctx)

			subtitle := string(m) + " mode"
			if settings.Instance != "" {
				subtitle = settings.Instance + " · " + subtitle
			}
			if alreadyUp {
				subtitle += " · stack already running"
			}
			ui.Banner(out, subtitle)

			// 1 + 2. Preflight then bring-up — skipped if the stack already
			// answers (re-running setup against a live install jumps straight
			// to configuration). Port checks only run for a fresh bring-up,
			// since a live OpenNeko legitimately holds those ports.
			if alreadyUp {
				ui.Info(out, "Existing OpenNeko detected — skipping preflight and bring-up.")
				// The wizard configures WHATEVER stack answers on this port.
				// `setup --mode demo` next to a live prod stack silently
				// configured prod with demo defaults; refuse the mismatch.
				project := runningWebProject(strconv.Itoa(settings.WebPort))
				if settings.Instance != "" && project == "" {
					return fmt.Errorf("%s is already answering, but it is not the web container for instance %q; choose another --port", baseURL, settings.Instance)
				}
				if project != "" {
					detectedMode, detectedInstance, ok := compose.IdentityFromProjectName(project)
					if project == "openneko" {
						detectedMode, detectedInstance, ok = compose.ModeProd, "", true
					}
					if !ok && settings.Instance != "" {
						return fmt.Errorf("the stack answering %s has unrecognized Compose project %q", baseURL, project)
					}
					if ok && (detectedMode != m || detectedInstance != settings.Instance) {
						return fmt.Errorf(
							"the stack answering %s is %q (instance %q, mode %s), not requested instance %q in mode %s",
							baseURL, project, detectedInstance, detectedMode, settings.Instance, m,
						)
					}
				}
			} else {
				if err := runPreflight(out); err != nil {
					return err
				}
				ui.Info(out, "Bringing up the stack…")
				if err := bringUpStack(ctx, cmd, m, bringUpOptions{detach: true, quiet: !verbose}); err != nil {
					return err
				}
				if err := ui.Spin("Waiting for the web app", func() error { return client.WaitReady(ctx, 120*time.Second) }); err != nil {
					return err
				}
			}
			ui.Success(out, "web app ready")

			// 3. Onboarding.
			interactive := prompt.IsInteractive()
			headless := adminPassword != "" || provider != "" || providerKey != "" ||
				legacyBackend != "" || model != "" || researchProvider != "" || researchKey != ""

			if skipOnboarding {
				ui.Info(out, "Stack is up. Finish setup in your browser:")
				fmt.Fprintln(out, "  "+baseURL)
				return nil
			}
			if !interactive && !headless {
				ui.Info(out, "No TTY and no setup flags — finish setup in your browser:")
				fmt.Fprintln(out, "  "+baseURL)
				return nil
			}

			cfg := setup.Config{
				Mode:             string(m),
				BaseURL:          baseURL,
				Headless:         headless || !interactive,
				AdminPassword:    adminPassword,
				Provider:         provider,
				ProviderKey:      providerKey,
				Model:            model,
				DataURL:          dataURL,
				ResearchProvider: researchProvider,
				ResearchKey:      researchKey,
				NoResearch:       noResearch,
			}
			outcome, runErr := setup.Run(ctx, client, out, cfg)

			// Persist the rotated password to the host config (the source the
			// CLI's own `neko` connection reads on later `start`/`migrate`
			// runs). The web wizard writes it only to the in-container config
			// volume, so without this the next host-side `neko` connection would
			// fall back to the stale bootstrap default. This must happen even
			// when a LATER wizard step failed — the remote rotation has already
			// committed by then, and dropping it here locked the host CLI out.
			// The gateway itself is unaffected by the rotation — it runs on its
			// dedicated `openshell` role (see ensureOpenShellGatewayRole), so no
			// gateway restart is needed.
			if outcome.PasswordSet != "" {
				if err := config.WriteLocalDatabasePasswords("", outcome.PasswordSet, outcome.PasswordSet); err != nil {
					ui.Info(out, "warning: couldn't persist the DB password to the host config: %v", err)
				}
			}
			if runErr != nil {
				return runErr
			}

			if outcome.Configured {
				ui.CompletionBox(out,
					ui.OK()+" Setup complete.",
					"",
					"Next: open "+baseURL+"/onboarding",
					"      to describe your business.",
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "prod", "Stack mode: prod|dev|demo")
	cmd.Flags().IntVar(&webPort, "port", 0, "Host port for the web app (named instances auto-select when omitted)")
	cmd.Flags().IntVar(&openShellPort, "openshell-port", 0, "Loopback host port for the OpenShell gateway (named instances auto-select when omitted)")
	cmd.Flags().StringVar(&webBindAddress, "bind-address", "", "IPv4 address for the web listener (default 0.0.0.0; use 127.0.0.1 behind a reverse proxy)")
	cmd.Flags().StringVar(&dockerSubnet, "docker-subnet", "", "Private IPv4 /24 for this instance (named instances auto-select when omitted)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Stream full image-pull output during bring-up")
	cmd.Flags().BoolVar(&skipOnboarding, "skip-onboarding", false, "Bring up the stack only; finish configuration in the browser")
	cmd.Flags().StringVar(&adminPassword, "admin-password", "", "Headless: admin database password")
	cmd.Flags().StringVar(&legacyBackend, "backend", "", "Deprecated: Ax is the only agent runtime")
	_ = cmd.Flags().MarkDeprecated("backend", "Ax is now the only agent runtime")
	cmd.Flags().StringVar(&provider, "provider", "", "Headless: primary model provider")
	cmd.Flags().StringVar(&providerKey, "provider-key", "", "Headless: primary provider API key")
	cmd.Flags().StringVar(&model, "model", "", "Headless: primary model (default: provider default)")
	cmd.Flags().StringVar(&dataURL, "data-url", "", "GraphJin base URL (default: per-mode)")
	cmd.Flags().StringVar(&researchProvider, "research-provider", "", "Headless: research provider")
	cmd.Flags().StringVar(&researchKey, "research-key", "", "Headless: research provider API key")
	cmd.Flags().BoolVar(&noResearch, "no-research", false, "Headless: leave industry research disabled")
	// Plugins were replaced by solution packs. Keep the old flags as no-ops
	// so existing automation that passes them doesn't break.
	cmd.Flags().BoolVar(&skipPlugins, "skip-plugins", false, "Deprecated: setup no longer installs plugins")
	_ = cmd.Flags().MarkDeprecated("skip-plugins", "plugins were replaced by solution packs; install packs from /admin/settings/packs")
	cmd.Flags().StringVar(&pluginsCSV, "plugins", "", "Deprecated: setup no longer installs plugins")
	_ = cmd.Flags().MarkDeprecated("plugins", "plugins were replaced by solution packs; install packs from /admin/settings/packs")
	return cmd
}

func validateLegacyAgentBackend(value string) error {
	switch strings.TrimSpace(value) {
	case "", "ax":
		return nil
	default:
		return fmt.Errorf("unsupported --backend %q: Ax is the only agent runtime", value)
	}
}

// webBaseURL resolves the local web app URL from the published port (matching
// status.go's probeWeb).
// runningWebProject returns the compose project of the running web
// container, or "" when none is found / docker is unreachable. Used to
// detect which stack actually owns the port setup is about to configure.
func runningWebProject(port string) string {
	out, err := exec.Command(
		"docker", "ps",
		"--filter", "status=running",
		"--filter", "label=com.docker.compose.service=web",
		"--format", `{{.Label "com.docker.compose.project"}}|{{.Ports}}`,
	).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		project, ports, ok := strings.Cut(strings.TrimSpace(line), "|")
		if ok && project != "" && strings.Contains(ports, ":"+port+"->8080/tcp") {
			return project
		}
	}
	return ""
}

func webBaseURL() string {
	port := strings.TrimSpace(os.Getenv("OPENNEKO_PORT"))
	if port == "" {
		port = "3000"
	}
	host := strings.TrimSpace(os.Getenv("OPENNEKO_WEB_BIND_ADDRESS"))
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// runPreflight runs the host readiness checks and prints them, returning a
// non-nil (exit-coded) error if any hard check fails. Host failure exits 3 to
// match root.go's code map; other failures exit 1.
func runPreflight(out io.Writer) error {
	fmt.Fprintln(out, ui.Heading("Preflight"))
	checks := []preflight.Result{preflight.Host(), preflight.Docker()}
	checks = append(checks, preflight.Ports(preflight.DefaultPorts)...)
	checks = append(checks, preflight.DuplicateBinary())

	code := 0
	for _, c := range checks {
		switch c.Level {
		case preflight.Pass:
			ui.Success(out, "%s: %s", c.Name, c.Detail)
		case preflight.Warn:
			ui.Info(out, "! %s: %s", c.Name, c.Detail)
			if c.Remediation != "" {
				ui.Info(out, "    %s", c.Remediation)
			}
		case preflight.Fail:
			ui.Failure(out, "%s: %s", c.Name, c.Detail)
			if c.Remediation != "" {
				ui.Info(out, "    %s", c.Remediation)
			}
			if c.Name == "host" {
				code = 3
			} else if code == 0 {
				code = 1
			}
		}
	}
	if code != 0 {
		return WithExit(code, fmt.Errorf("preflight failed — fix the above and re-run `openneko setup`"))
	}
	return nil
}
