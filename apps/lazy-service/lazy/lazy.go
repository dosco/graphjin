// Package lazy runs a small HTTP listener that owns a lazy child process. No
// Docker socket or host privileges: the child and listener share the service
// container's limits. The lazy-service binary and the openneko binary's
// lazy-service role both call Main.
package lazy

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type service struct {
	mu            sync.Mutex
	command       []string
	upstream      string
	startup, idle time.Duration
	cmd           *exec.Cmd
	done          chan struct{}
	active        int
	lastUsed      time.Time
	starts        int
	failed        bool
	client        *http.Client
	shutdown      <-chan struct{}
}

func (s *service) probe(path string, idle bool) bool {
	response, err := s.client.Get(s.upstream + path)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	if !idle {
		return true
	}
	var body struct {
		Idle bool `json:"idle"`
	}
	return json.NewDecoder(response.Body).Decode(&body) == nil && body.Idle
}

// Called under mu: concurrent callers share startup, and idle shutdown cannot
// race a newly acquired request. Polls and termination have bounded waits.
func (s *service) acquire(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.cmd != nil {
		select {
		case <-s.done:
			s.stop()
		default:
		}
	}
	if s.cmd == nil {
		s.cmd = exec.Command(s.command[0], s.command[1:]...)
		s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		s.cmd.Stdout, s.cmd.Stderr = os.Stdout, os.Stderr
		if err := s.cmd.Start(); err != nil {
			s.cmd = nil
			s.failed = true
			return err
		}
		s.starts++
		s.done = make(chan struct{})
		cmd, done := s.cmd, s.done
		go func() { _ = cmd.Wait(); close(done) }()
		log.Printf("starting processing child pid=%d", cmd.Process.Pid)
		deadline := time.Now().Add(s.startup)
		for {
			select {
			case <-s.shutdown:
				s.stop()
				return errors.New("service is shutting down")
			case <-done:
				s.stop()
				s.failed = true
				return errors.New("processing child exited during startup")
			default:
			}
			if s.probe("/health/ready", false) {
				break
			}
			if time.Now().After(deadline) {
				s.stop()
				s.failed = true
				return errors.New("processing child readiness timed out")
			}
			time.Sleep(50 * time.Millisecond)
		}
		s.failed = false
	}
	s.lastUsed = time.Now()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.active++
	return nil
}

func (s *service) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	s.lastUsed = time.Now()
}

func (s *service) stop() {
	if s.cmd == nil {
		return
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	// Also remove tool descendants if the parent exited before they did.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	<-s.done
	s.cmd = nil
	log.Printf("stopped processing child pid=%d", pid)
}

func (s *service) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.active != 0 || time.Since(s.lastUsed) < s.idle {
		return
	}
	select {
	case <-s.done:
		s.stop()
		return
	default:
	}
	// An HTTP 202 can leave queued work behind. A failed idle probe never
	// authorizes terminating it.
	if s.probe("/health/idle", true) {
		s.stop()
	}
}

func (s *service) handler() http.Handler {
	target, _ := url.Parse(s.upstream)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 2 * time.Minute}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "processing service unavailable", http.StatusBadGateway)
	}
	// Bound waiting requests as well as active backend work.
	slots := make(chan struct{}, 32)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/ready" {
			state, starts, healthy := "starting", 0, true
			if s.mu.TryLock() {
				state, starts, healthy = "sleeping", s.starts, !s.failed
				if s.cmd != nil {
					state = "running"
					select {
					case <-s.done:
						state, healthy = "stopped", false
					default:
					}
				}
				s.mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			if !healthy {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": healthy, "state": state, "starts": starts})
			return
		}
		if len(r.URL.Path) < 4 || r.URL.Path[:4] != "/v1/" {
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "service request queue is full", http.StatusTooManyRequests)
			return
		}
		if err := s.acquire(r.Context()); err != nil {
			log.Printf("processing startup failed: %v", err)
			http.Error(w, "processing service unavailable", http.StatusServiceUnavailable)
			return
		}
		defer s.release()
		r.Body = http.MaxBytesReader(w, r.Body, 110<<20)
		proxy.ServeHTTP(w, r)
	})
}

// Main runs the listener with the given command-line arguments, without the
// program name, until SIGINT or SIGTERM.
func Main(args []string) {
	flags := flag.NewFlagSet("lazy-service", flag.ExitOnError)
	listen := flags.String("listen", ":5001", "listener address")
	upstream := flags.String("upstream", "http://127.0.0.1:5002", "child HTTP endpoint")
	idle := flags.Duration("idle", 5*time.Minute, "idle time before stopping child")
	startup := flags.Duration("startup", 120*time.Second, "child readiness timeout")
	_ = flags.Parse(args)
	if raw := os.Getenv("OPENNEKO_SERVICE_IDLE_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatal(err)
		}
		*idle = value
	}
	target, err := url.Parse(*upstream)
	if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.Path != "" {
		log.Fatal("upstream must be an HTTP endpoint on 127.0.0.1")
	}
	if flags.NArg() == 0 || *idle <= 0 || *startup <= 0 {
		log.Fatal("positive timeouts and a child command are required")
	}
	if _, err := exec.LookPath(flags.Arg(0)); err != nil {
		log.Fatal(err)
	}
	s := &service{command: flags.Args(), upstream: *upstream, idle: *idle, startup: *startup, client: &http.Client{Timeout: time.Second}}
	server := &http.Server{Addr: *listen, Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	s.shutdown = ctx.Done()
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.expire()
			}
		}
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("lazy processing listener ready")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
	s.mu.Lock()
	s.stop()
	s.mu.Unlock()
}
