package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/host"
	"github.com/open-neko/neko/apps/openneko/internal/preflight"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check whether this host can run OpenNeko and report state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			h := host.Check()
			supportedTag := "supported"
			if !h.Supported {
				supportedTag = "UNSUPPORTED"
			}
			fmt.Fprintf(out, "host: %s (%s)\n", h.Triple, supportedTag)
			if !h.Supported && h.Reason != "" {
				fmt.Fprintf(out, "  reason: %s\n", h.Reason)
			}
			docker := preflight.Docker()
			fmt.Fprintf(out, "docker:   %s\n", docker.Detail)
			if docker.Level == preflight.Fail && docker.Remediation != "" {
				fmt.Fprintf(out, "  fix: %s\n", docker.Remediation)
			}

			if dup := preflight.DuplicateBinary(); dup.Level == preflight.Warn {
				fmt.Fprintf(out, "warning:  %s — %s\n", dup.Detail, dup.Remediation)
			}

			operationalFailed := false
			if docker.Level == preflight.Pass {
				failed, err := runOperationalDoctor(cmd.Context(), out)
				if err != nil {
					fmt.Fprintf(out, "operations: FAIL — %v\n", err)
					operationalFailed = true
				} else {
					operationalFailed = failed
				}
			}

			if !h.Supported || operationalFailed {
				return WithExit(1, nil)
			}
			return nil
		},
	}
}

func runOperationalDoctor(ctx context.Context, out io.Writer) (bool, error) {
	sup, _, project, files, err := currentStackCompose()
	if err != nil {
		return false, err
	}
	if dir, err := backupDirectory(sup, project); err != nil {
		fmt.Fprintf(out, "backup:   warning — %v\n", err)
	} else if latest, err := latestBackup(dir); err != nil {
		fmt.Fprintf(out, "backup:   warning — cannot read %s: %v\n", dir, err)
	} else {
		fmt.Fprintf(out, "backup:   %s\n", backupDoctorLine(latest, dir, time.Now()))
	}
	services, err := composePs(project, files)
	if err != nil {
		return false, err
	}
	running := map[string]bool{}
	for _, service := range services {
		running[service.Service] = service.State == "running"
	}
	if len(services) == 0 {
		fmt.Fprintln(out, "operations: stack not running (database checks skipped)")
		return false, nil
	}

	if !running["neko-db"] {
		fmt.Fprintln(out, "database: FAIL — neko-db is not running")
		return true, nil
	}
	if _, err := composeCommandOutput(ctx, project, files,
		"exec", "-T", "neko-db", "sh", "-ec",
		`pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`); err != nil {
		fmt.Fprintf(out, "database: FAIL — %v\n", err)
		return true, nil
	}
	fmt.Fprintln(out, "database: reachable")
	maintenance, err := composeCommandOutput(ctx, project, files,
		"exec", "-T", "neko-db", "sh", "-ec",
		`psql -X -Atq -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select current_setting('autovacuum') || '|' || coalesce(round(100 * max(n_dead_tup::numeric / nullif(n_live_tup + n_dead_tup, 0)), 1), 0)::text from pg_stat_user_tables"`)
	if err != nil {
		fmt.Fprintf(out, "maintenance: warning — %v\n", err)
	} else {
		parts := strings.Split(strings.TrimSpace(string(maintenance)), "|")
		if len(parts) == 2 {
			fmt.Fprintf(out, "maintenance: autovacuum %s; worst dead-row ratio %s%%\n", parts[0], parts[1])
		}
	}
	return false, nil
}

func composeCommandOutput(ctx context.Context, project string, files []string, command ...string) ([]byte, error) {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "-p", project)
	}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, command...)
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return output, nil
}
