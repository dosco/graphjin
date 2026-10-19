package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/skill"
)

// newInstallCmd installs a community skill from a git URL. Integrations
// and channels come from solution packs (`openneko pack install`).
func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <git-url>[#<sub-path>]",
		Short: "Install a community skill from a git URL",
		Long: `Install a community skill (a folder with a SKILL.md) from a git URL.

Accepted hosts: github.com, gitlab.com, codeberg.org (https only). Use the
#<sub-path> fragment to point at a skill folder inside the repo.

An admin must enable Git-URL skill installs at /admin/settings/security.
Integrations and channels are installed as solution packs: see
` + "`openneko pack`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			spec := args[0]
			if spec == "" {
				return WithExit(2, errors.New("install: git URL required"))
			}
			errOut := cmd.ErrOrStderr()
			out := cmd.OutOrStdout()

			if !isGitURLSpec(spec) {
				return WithExit(2, fmt.Errorf("install: %q is not a git URL. Plugins were replaced by solution packs; use `openneko pack install <pack>` for integrations", spec))
			}

			// Fetch the deployment-wide install policy. Git-URL skills need
			// an admin to have opted in via /admin/settings/security. If the
			// worker is unreachable, the fetch falls back to the secure
			// default — operators can't turn off the policy by killing the
			// worker.
			pol, source, fetchErr := skill.FetchPolicy(context.Background())
			if fetchErr != nil {
				fmt.Fprintf(errOut, "WARNING: install policy check failed (%s) — defaulting to most-restrictive\n", fetchErr.Error())
			}
			if !pol.AllowGitURLInstalls {
				hint := "ask an admin to enable Git-URL skill installs at /admin/settings/security"
				if source == "unreachable" {
					hint = "start the worker so the policy can be read, or ask an admin to enable Git-URL skill installs at /admin/settings/security"
				}
				return WithExit(2, fmt.Errorf("install: Git-URL skill installs are disabled by deployment policy. %s", hint))
			}
			fmt.Fprintln(errOut, "Installing community skill from Git URL. The SKILL.md will be loaded as-is from the upstream repo; there is no integrity check beyond the agentskills.io spec validation.")
			res, err := skill.RunGitSkill(context.Background(), skill.GitSkillOptions{Spec: spec})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "installed skill %q (git-url, source: %s", res.SkillName, res.SourceURL)
			if res.SourceSubPath != "" {
				fmt.Fprintf(out, "#%s", res.SourceSubPath)
			}
			fmt.Fprintf(out, ")\n  installed at: %s\n", res.SkillInstalledAt)
			return nil
		},
	}
	return cmd
}

// isGitURLSpec returns true when the install spec looks like a clone
// URL. We accept only https against well-known forge hosts.
func isGitURLSpec(spec string) bool {
	return strings.HasPrefix(spec, "https://github.com/") ||
		strings.HasPrefix(spec, "https://gitlab.com/") ||
		strings.HasPrefix(spec, "https://codeberg.org/")
}
