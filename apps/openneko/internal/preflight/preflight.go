// Package preflight runs the host readiness checks shared by `openneko doctor`
// and `openneko setup`: is this OS/arch supported, is the Docker daemon up, are
// the host ports free, and is there a single openneko on PATH. Each check
// returns a Result so callers can either print it (doctor) or gate on it
// (setup).
package preflight

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/open-neko/neko/apps/openneko/internal/host"
)

type Level int

const (
	Pass Level = iota
	Warn
	Fail
)

type Result struct {
	Name        string // short label, e.g. "host", "docker", "port 3000 (web)"
	Level       Level
	Detail      string // version, triple, or what's wrong
	Remediation string // how to fix; set when Warn/Fail
}

// OK is false only for a hard failure — a Warn is still OK to proceed past.
func (r Result) OK() bool { return r.Level != Fail }

// Host reports whether this OS/arch can run the OpenNeko stack,
// independent of docker (Docker covers that with a sharper message).
func Host() Result {
	h := host.Platform()
	if h.Supported {
		return Result{Name: "host", Level: Pass, Detail: h.Triple + " (supported)"}
	}
	return Result{Name: "host", Level: Fail, Detail: h.Triple, Remediation: h.Reason}
}

// Docker reports whether the docker CLI is installed and its daemon answering.
// `docker info --format {{.ServerVersion}}` does both: it only succeeds when the
// daemon is reachable, and returns the server version for the detail line.
func Docker() Result {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return Result{
			Name:        "docker",
			Level:       Fail,
			Detail:      "not found on PATH",
			Remediation: "Install Docker Desktop (macOS) or Docker Engine (Linux): https://docs.docker.com/get-docker/",
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil {
		return Result{
			Name:        "docker",
			Level:       Fail,
			Detail:      "installed, but the daemon isn't responding",
			Remediation: "Start Docker Desktop (macOS) or `sudo systemctl start docker` (Linux), then re-run.",
		}
	}
	ver := strings.TrimSpace(string(out))
	detail := "daemon running"
	if ver != "" {
		detail = "daemon running (server " + ver + ")"
	}
	return Result{Name: "docker", Level: Pass, Detail: detail}
}

// PortSpec is a host port the stack publishes, with the env var that overrides
// it (matching start.go's envInt lookups).
type PortSpec struct {
	Label       string
	EnvVar      string
	Def         int
	BindEnvVar  string
	DefaultBind string
}

// DefaultPorts are the only host ports a packaged bring-up publishes. Web is
// the intentional server boundary; databases, GraphJin, brokers and the agent
// runtime's sockets stay private.
var DefaultPorts = []PortSpec{
	{Label: "web", EnvVar: "OPENNEKO_PORT", Def: 3000, BindEnvVar: "OPENNEKO_WEB_BIND_ADDRESS", DefaultBind: "0.0.0.0"},
}

// Port resolves a spec's effective port from its env override.
func (s PortSpec) Port() int {
	if v := strings.TrimSpace(os.Getenv(s.EnvVar)); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			return p
		}
	}
	return s.Def
}

func (s PortSpec) BindAddress() string {
	if s.BindEnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(s.BindEnvVar)); value != "" {
			return value
		}
	}
	if s.DefaultBind != "" {
		return s.DefaultBind
	}
	return "127.0.0.1"
}

// Ports checks each spec's port on its configured bind address. A bound port means something already holds it —
// including a previous OpenNeko, so callers that might be re-running against a
// live stack should skip this (see setup).
func Ports(specs []PortSpec) []Result {
	out := make([]Result, 0, len(specs))
	for _, s := range specs {
		port := s.Port()
		name := fmt.Sprintf("port %d (%s)", port, s.Label)
		bindAddress := s.BindAddress()
		ln, err := net.Listen("tcp", net.JoinHostPort(bindAddress, strconv.Itoa(port)))
		if err != nil {
			out = append(out, Result{
				Name:        name,
				Level:       Fail,
				Detail:      fmt.Sprintf("cannot bind %s", net.JoinHostPort(bindAddress, strconv.Itoa(port))),
				Remediation: fmt.Sprintf("Free it, or pick another port with %s=<port>.", s.EnvVar),
			})
			continue
		}
		_ = ln.Close()
		out = append(out, Result{Name: name, Level: Pass, Detail: "free"})
	}
	return out
}

// DuplicateBinary warns when a second openneko shadows or competes with the
// running one on PATH — common while operators migrate off the old npm CLI.
func DuplicateBinary() Result {
	self, err := os.Executable()
	if err != nil {
		return Result{Name: "path", Level: Pass, Detail: "single openneko on PATH"}
	}
	bin, lookErr := exec.LookPath("openneko")
	return duplicateBinaryWith(self, bin, lookErr)
}

func duplicateBinaryWith(self, found string, lookErr error) Result {
	if lookErr != nil || found == self {
		return Result{Name: "path", Level: Pass, Detail: "single openneko on PATH"}
	}
	return Result{
		Name:        "path",
		Level:       Warn,
		Detail:      "duplicate openneko at " + found,
		Remediation: "Remove the older one to avoid version confusion.",
	}
}
