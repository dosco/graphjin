// Package sandbox runs the agent's terminal commands away from the process
// that holds model keys. The openneko binary's sandbox role calls Main.
//
// The sandbox holds no secrets. Each run gets its own user and its own home
// folder; a command runs in the run's workspace with only PATH, HOME, TMPDIR
// and SKILL_DIR set. Serve reaches it over a Unix socket:
//
//	POST   /v1/runs/{run}/exec  {"workspace","command","skill_dir","timeout_seconds","max_output"}
//	DELETE /v1/runs/{run}       kills the run's commands and removes its home
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-neko/openneko/apps/ax-harness/internal/localtool"
)

// DefaultSocket is where the sandbox listens unless told otherwise.
const DefaultSocket = "/run/openneko/sandbox.sock"

const (
	defaultBaseUID = 20000
	defaultUsers   = 256
	maxTimeout     = 600
	maxOutput      = 1_000_000
)

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ExecRequest is the body of an exec call.
type ExecRequest struct {
	Workspace      string `json:"workspace"`
	Command        string `json:"command"`
	SkillDir       string `json:"skill_dir,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxOutput      int    `json:"max_output"`
}

// Server runs commands for runs.
type Server struct {
	// Workspaces is the shared folder every run workspace sits under.
	Workspaces string
	// Skills is the shared folder of staged skills, readable by runs.
	Skills string
	// Homes holds each run's home folder.
	Homes string
	// BaseUID and Users bound the user IDs given to runs. GID is the group
	// that shares workspaces with serve and the worker.
	BaseUID, Users, GID int
	// SwitchUsers runs each command as its run's user; it needs root.
	SwitchUsers bool
	// PATH is the command search path.
	PATH string

	mu   sync.Mutex
	runs map[string]int // run → uid
	used map[int]bool
}

// Main parses args and serves until SIGINT or SIGTERM.
func Main(args []string) {
	flags := flag.NewFlagSet("sandbox", flag.ExitOnError)
	socket := flags.String("socket", envOr("OPENNEKO_SANDBOX_SOCKET", DefaultSocket), "Unix socket to listen on")
	_ = flags.Parse(args)
	server := &Server{
		Workspaces:  envOr("OPENNEKO_SANDBOX_WORKSPACES", "/var/lib/openneko/workspaces"),
		Skills:      os.Getenv("OPENNEKO_SANDBOX_SKILLS"),
		Homes:       envOr("OPENNEKO_SANDBOX_HOMES", "/var/lib/openneko/sandbox"),
		BaseUID:     intEnv("OPENNEKO_SANDBOX_BASE_UID", defaultBaseUID),
		Users:       intEnv("OPENNEKO_SANDBOX_USERS", defaultUsers),
		GID:         intEnv("OPENNEKO_SANDBOX_GID", os.Getgid()),
		SwitchUsers: os.Geteuid() == 0,
		PATH:        envOr("PATH", "/usr/local/bin:/usr/bin:/bin"),
	}
	if !server.SwitchUsers {
		fmt.Fprintln(os.Stderr, "openneko sandbox: not root, so commands run as this user")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.ListenAndServe(ctx, *socket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ListenAndServe serves on a Unix socket at path until ctx ends.
func (s *Server) ListenAndServe(ctx context.Context, path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(path)
	}
	old := syscall.Umask(0o117)
	listener, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return fmt.Errorf("sandbox socket unavailable: %w", err)
	}
	// Serve runs as another user in the shared group; as root, hand the
	// socket to that group so serve can connect (mode stays 0660).
	if os.Geteuid() == 0 {
		if err := os.Chown(path, -1, s.GID); err != nil {
			_ = listener.Close()
			return fmt.Errorf("sandbox socket unavailable: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "openneko sandbox: listening on %s\n", path)
	return s.Serve(ctx, listener)
}

// Serve serves on listener until ctx ends.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler is the sandbox API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/runs/{run}/exec", s.exec)
	mux.HandleFunc("DELETE /v1/runs/{run}", s.end)
	return mux
}

func (s *Server) exec(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	var req ExecRequest
	if !runIDPattern.MatchString(run) || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil ||
		strings.TrimSpace(req.Command) == "" || len(req.Command) > 16384 ||
		req.TimeoutSeconds < 1 || req.TimeoutSeconds > maxTimeout || req.MaxOutput < 1_000 || req.MaxOutput > maxOutput {
		http.Error(w, "invalid terminal command", http.StatusBadRequest)
		return
	}
	workspace, ok := within(s.Workspaces, req.Workspace)
	if !ok {
		http.Error(w, "workspace is outside the sandbox", http.StatusForbidden)
		return
	}
	uid, err := s.user(run)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	home, err := s.home(run, uid)
	if err != nil {
		http.Error(w, "run home unavailable", http.StatusInternalServerError)
		return
	}
	env := []string{"PATH=" + s.PATH, "HOME=" + home, "TMPDIR=" + filepath.Join(home, "tmp"), "LANG=C.UTF-8"}
	if dir, ok := within(s.Skills, req.SkillDir); ok && req.SkillDir != "" && s.Skills != "" {
		env = append(env, "SKILL_DIR="+dir)
	}
	exec := localtool.ExecRequest{
		Dir: workspace,
		// Files the run writes stay writable by the group that shares the workspace.
		Command:        "umask 002\n" + req.Command,
		Env:            env,
		TimeoutSeconds: req.TimeoutSeconds,
		MaxOutput:      req.MaxOutput,
	}
	if s.SwitchUsers {
		exec.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(s.GID), NoSetGroups: true}
	}
	result, err := localtool.Exec(r.Context(), exec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) end(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	if !runIDPattern.MatchString(run) {
		http.Error(w, "invalid run", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	uid, ok := s.runs[run]
	delete(s.runs, run)
	s.mu.Unlock()
	if ok && s.SwitchUsers {
		killUser(uid)
	}
	_ = os.RemoveAll(filepath.Join(s.Homes, run))
	s.mu.Lock()
	delete(s.used, uid)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// user gives run a user ID of its own, the same one for every command.
func (s *Server) user(run string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs == nil {
		s.runs, s.used = map[string]int{}, map[int]bool{}
	}
	if uid, ok := s.runs[run]; ok {
		return uid, nil
	}
	users := s.Users
	if users <= 0 {
		users = defaultUsers
	}
	for i := 0; i < users; i++ {
		uid := s.BaseUID + i
		if !s.used[uid] {
			s.used[uid] = true
			s.runs[run] = uid
			return uid, nil
		}
	}
	return 0, errors.New("the sandbox has no free run user")
}

func (s *Server) home(run string, uid int) (string, error) {
	home := filepath.Join(s.Homes, run)
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0o700); err != nil {
		return "", err
	}
	if s.SwitchUsers {
		for _, dir := range []string{home, filepath.Join(home, "tmp")} {
			if err := os.Chown(dir, uid, s.GID); err != nil {
				return "", err
			}
		}
	}
	return home, nil
}

// within resolves path and reports whether it is root or under it.
func within(root, path string) (string, bool) {
	if root == "" || !filepath.IsAbs(path) {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", false
	}
	return resolved, true
}

// killUser kills every process the run's user owns.
func killUser(uid int) {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		var stat syscall.Stat_t
		if syscall.Stat(filepath.Join("/proc", entry.Name()), &stat) == nil && int(stat.Uid) == uid {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
