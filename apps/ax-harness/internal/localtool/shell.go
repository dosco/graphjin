package localtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// Default terminal limits: 180 s default, 600 s maximum, and
// 50,000 characters of combined output. The host can set both defaults.
const (
	defaultShellTimeout = 180
	maxShellTimeout     = 600
	defaultShellOutput  = 50_000
	maxShellOutput      = 1_000_000
)

// Shell runs commands in the run workspace without the host's credentials.
type Shell struct {
	dir       string
	skills    string
	strip     map[string]bool
	env       []string
	executor  Executor
	timeout   int
	maxOutput int
}

// OpenShell checks the workspace and records the variables to remove from
// every command's environment.
func OpenShell(dir string, strip []string) (*Shell, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("workspace path must be absolute")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workspace unavailable")
	}
	set := make(map[string]bool, len(strip))
	for _, name := range strip {
		set[name] = true
	}
	return &Shell{dir: dir, strip: set, timeout: defaultShellTimeout, maxOutput: defaultShellOutput}, nil
}

// SetLimits sets the default command timeout and the output cap.
func (s *Shell) SetLimits(timeoutSeconds, maxOutput int) error {
	if timeoutSeconds < 1 || timeoutSeconds > maxShellTimeout || maxOutput < 1_000 || maxOutput > maxShellOutput {
		return fmt.Errorf("invalid terminal limits")
	}
	s.timeout, s.maxOutput = timeoutSeconds, maxOutput
	return nil
}

// SetEnviron replaces the process environment as the base of every command's
// environment, for a host that runs the harness in-process.
func (s *Shell) SetEnviron(environ []string) {
	s.env = append([]string{}, environ...)
}

func (s *Shell) environ() []string {
	if s.env != nil {
		return s.env
	}
	return os.Environ()
}

// SetSkillsRoot names the staged skills, so a command can find its skill's folder.
func (s *Shell) SetSkillsRoot(dir string) {
	s.skills = dir
}

var skillDirPath = regexp.MustCompile(`\$\{?SKILL_DIR\}?(/[^\s"'$;|&()<>]+)`)

// skillDir returns the one staged skill that holds every $SKILL_DIR path in the command.
// SKILL_DIR differs per skill, so it cannot be set once for the run.
func (s *Shell) skillDir(command string) string {
	if s.skills == "" || envValue(s.environ(), "SKILL_DIR") != "" {
		return ""
	}
	found := ""
	for _, match := range skillDirPath.FindAllStringSubmatch(command, -1) {
		hits, _ := filepath.Glob(filepath.Join(s.skills, "*", filepath.Clean(match[1])))
		dirs := map[string]bool{}
		for _, hit := range hits {
			rel, err := filepath.Rel(s.skills, hit)
			if err == nil && filepath.IsLocal(rel) {
				dirs[filepath.Join(s.skills, strings.SplitN(rel, string(filepath.Separator), 2)[0])] = true
			}
		}
		if len(dirs) != 1 {
			return ""
		}
		for dir := range dirs {
			if found != "" && found != dir {
				return ""
			}
			found = dir
		}
	}
	return found
}

func (s *Shell) Capability() agent.Capability {
	return agent.Capability{Name: "terminal", Version: "1", Origin: "workspace", Effect: "durable",
		Description: fmt.Sprintf("Run a shell command with /bin/sh in the run workspace. Returns the exit code and the combined output, capped at %d characters. Default timeout %d seconds, maximum %d.", s.maxOutput, s.timeout, maxShellTimeout),
		InputSchema: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string","minLength":1,"maxLength":16384},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600}},"additionalProperties":false}`),
		Call:        s.run}
}

// ExecResult is a terminal command's outcome.
type ExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	TimedOut  bool   `json:"timed_out"`
	Truncated bool   `json:"truncated"`
}

// ExecRequest is one command for Exec.
type ExecRequest struct {
	Dir            string
	Command        string
	Env            []string
	TimeoutSeconds int
	MaxOutput      int
	// Credential runs the command as another user; nil runs it as this one.
	Credential *syscall.Credential
}

// Executor runs terminal commands somewhere other than this process, such as
// the sandbox service.
type Executor interface {
	Exec(ctx context.Context, command, skillDir string, timeoutSeconds, maxOutput int) (ExecResult, error)
	Close() error
}

// SetExecutor sends every command to executor instead of running it here.
func (s *Shell) SetExecutor(executor Executor) {
	s.executor = executor
}

// Close ends the executor's run, if commands go to one.
func (s *Shell) Close() error {
	if s.executor == nil {
		return nil
	}
	return s.executor.Close()
}

func (s *Shell) run(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || strings.TrimSpace(input.Command) == "" || len(input.Command) > 16384 ||
		input.TimeoutSeconds < 0 || input.TimeoutSeconds > maxShellTimeout {
		return nil, fmt.Errorf("invalid terminal command")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = s.timeout
	}
	skillDir := s.skillDir(input.Command)
	var result ExecResult
	var err error
	if s.executor != nil {
		result, err = s.executor.Exec(ctx, input.Command, skillDir, input.TimeoutSeconds, s.maxOutput)
	} else {
		var env []string
		for _, entry := range s.environ() {
			name, _, _ := strings.Cut(entry, "=")
			if !s.strip[name] {
				env = append(env, entry)
			}
		}
		if skillDir != "" {
			env = append(env, "SKILL_DIR="+skillDir)
		}
		result, err = Exec(ctx, ExecRequest{Dir: s.dir, Command: input.Command, Env: env, TimeoutSeconds: input.TimeoutSeconds, MaxOutput: s.maxOutput})
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// Exec runs one command with /bin/sh in its own process group and kills the
// group when the command ends, times out or ctx ends.
func Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	cmd := exec.Command("/bin/sh", "-c", req.Command)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: req.Credential}
	output := &capturedOutput{max: req.MaxOutput}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return ExecResult{}, fmt.Errorf("terminal command could not start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(time.Duration(req.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		waitErr = <-done
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return ExecResult{}, ctx.Err()
	}
	// Background children keep the group alive after the shell exits.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	result := ExecResult{TimedOut: timedOut}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		result.ExitCode = -1
	}
	result.Output, result.Truncated = output.text()
	return result, nil
}

// capturedOutput keeps the first and last halves of the output budget.
type capturedOutput struct {
	mu      sync.Mutex
	max     int
	head    []byte
	tail    []byte
	dropped int
}

func (c *capturedOutput) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	outputHalf := c.max / 2
	if room := outputHalf*4 - len(c.head); room > 0 {
		take := min(room, len(p))
		c.head = append(c.head, p[:take]...)
		p = p[take:]
	}
	c.tail = append(c.tail, p...)
	if extra := len(c.tail) - outputHalf*4; extra > 0 {
		c.dropped += extra
		c.tail = append([]byte(nil), c.tail[extra:]...)
	}
	return n, nil
}

// text returns at most max characters, head and tail around a marker.
func (c *capturedOutput) text() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	outputHalf := c.max / 2
	all := strings.ToValidUTF8(string(c.head)+string(c.tail), "�")
	runes := []rune(all)
	if c.dropped == 0 && len(runes) <= c.max {
		return all, false
	}
	return string(runes[:outputHalf]) + "\n[... output truncated ...]\n" + string(runes[len(runes)-outputHalf:]), true
}

func envValue(environ []string, name string) string {
	for _, entry := range environ {
		if key, value, ok := strings.Cut(entry, "="); ok && key == name {
			return value
		}
	}
	return ""
}
