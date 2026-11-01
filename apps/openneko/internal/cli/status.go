package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
)

// composeService is the subset of `docker compose ps --format json` we read.
type composeService struct {
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

// health is the single honest answer status gives: is the stack actually
// usable right now, partly usable, or not.
type health int

const (
	serving  health = iota // everything a user needs is up
	degraded               // up but something is starting/unhealthy
	down                   // a required service is not running
)

// oneShotJobs are the run-to-completion services in the embedded compose files
// (those with `restart: "no"`). They exit by design, so a non-zero exit is a
// failed setup step — degraded, not a downed stack. A genuinely fatal one (e.g.
// neko-migrate) still surfaces as down: its dependents never start, and those
// missing long-running services drive the verdict. Keep in sync with the
// `restart: "no"` services in assets/compose/{core,demo}.yml.
var oneShotJobs = map[string]bool{
	"neko-migrate":             true,
	"storage-reconcile":        true,
	"adventureworks-init":      true,
	"graphjin-config-init":     true,
	"neko-adventureworks-seed": true,
}

func newStatusCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Is the stack serving, degraded, or down?",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sup := compose.New(assets.ComposeFS)
			files, err := sup.Materialize(compose.ModeDemo)
			if err != nil {
				return err
			}
			project, err := sup.ProjectName("")
			if err != nil {
				return err
			}
			if verbose {
				code, err := sup.Run(context.Background(), project, files, []string{"ps"}, os.Stdout, os.Stderr)
				if err != nil {
					return err
				}
				if code != 0 {
					return WithExit(code, nil)
				}
				return nil
			}
			services, err := composePs(project, files)
			if err != nil {
				return err
			}
			state, lines := classifyStack(services)
			writeStatus(cmd.OutOrStdout(), state, lines, probeWeb())
			if state == serving {
				return nil
			}
			return WithExit(1, nil)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show raw `docker compose ps` output")
	return cmd
}

func composePs(project string, files []string) ([]composeService, error) {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "-p", project)
	}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "ps", "-a", "--format", "json")
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps failed: %w", err)
	}
	return parseComposePs(out)
}

// parseComposePs handles both shapes docker compose emits: a JSON array
// (newer) and newline-delimited objects (older / single service).
func parseComposePs(out []byte) ([]composeService, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var arr []composeService
	if json.Unmarshal([]byte(trimmed), &arr) == nil {
		return arr, nil
	}
	var svcs []composeService
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s composeService
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("parse `docker compose ps` json: %w", err)
		}
		svcs = append(svcs, s)
	}
	return svcs, nil
}

// classifyStack reduces raw service states to one honest verdict plus a
// human-readable line per service. A one-shot job that exited 0 (e.g.
// neko-migrate) is a success, not a failure — that distinction is the whole
// point of this command.
func classifyStack(services []composeService) (health, []string) {
	if len(services) == 0 {
		return down, []string{"no services are running — start them with `openneko start`"}
	}
	worst := serving
	var lines []string
	for _, s := range services {
		state := strings.ToLower(strings.TrimSpace(s.State))
		hl := strings.ToLower(strings.TrimSpace(s.Health))
		switch {
		case state == "running" && (hl == "" || hl == "healthy"):
			lines = append(lines, fmt.Sprintf("  ✅ %s — serving", s.Service))
		case state == "running" && hl == "starting":
			lines = append(lines, fmt.Sprintf("  ⏳ %s — starting up", s.Service))
			if worst < degraded {
				worst = degraded
			}
		case state == "running" && hl == "unhealthy":
			lines = append(lines, fmt.Sprintf("  ⚠️  %s — running but failing its health check", s.Service))
			if worst < degraded {
				worst = degraded
			}
		case state == "exited" && s.ExitCode == 0:
			lines = append(lines, fmt.Sprintf("  ✅ %s — completed", s.Service))
		case state == "exited" && oneShotJobs[s.Service]:
			lines = append(lines, fmt.Sprintf("  ⚠️  %s — setup step failed (exit %d); the core stack is still up — see `openneko logs %s`", s.Service, s.ExitCode, s.Service))
			if worst < degraded {
				worst = degraded
			}
		default:
			lines = append(lines, fmt.Sprintf("  ❌ %s — %s", s.Service, describeState(s)))
			worst = down
		}
	}
	return worst, lines
}

func describeState(s composeService) string {
	state := strings.TrimSpace(s.State)
	if strings.EqualFold(state, "exited") {
		return fmt.Sprintf("exited (code %d)", s.ExitCode)
	}
	if state == "" {
		return "not running"
	}
	return state
}

// probeWeb is a best-effort "is the front door open?" check against the
// published web port. It never fails status on its own — it's an extra
// human signal alongside the container verdict.
func probeWeb() string {
	baseURL := webBaseURL()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(baseURL + "/")
	if err != nil {
		return fmt.Sprintf("  ·  web front door (%s): not answering yet", strings.TrimPrefix(baseURL, "http://"))
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return fmt.Sprintf("  ·  web front door (%s): answering (HTTP %d)", strings.TrimPrefix(baseURL, "http://"), resp.StatusCode)
}

func writeStatus(w io.Writer, state health, lines []string, webLine string) {
	switch state {
	case serving:
		fmt.Fprintln(w, "✅ serving — the stack is up and usable")
	case degraded:
		fmt.Fprintln(w, "⚠️  degraded — up, but something is still coming up or unhealthy")
	case down:
		fmt.Fprintln(w, "❌ down — a required service is not running")
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	if webLine != "" {
		fmt.Fprintln(w, webLine)
	}
	switch state {
	case degraded:
		fmt.Fprintln(w, "\nGive it a moment, then re-run `openneko status`. Details: `openneko status -v` / `openneko logs`.")
	case down:
		fmt.Fprintln(w, "\nSee why with `openneko logs`, or (re)start with `openneko start`.")
	}
}
