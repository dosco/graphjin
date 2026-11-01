package roles

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gjserv "github.com/dosco/graphjin/serv/v3"
	"github.com/jackc/pgx/v5"
)

// The subscriptions GraphJin serves the worker's workflow subscriptions from
// neko-db. Core features depend on it, so `openneko serve --path` always runs
// it beside the customer GraphJin. It connects as neko_subscriptions, a role
// that may only read workflow_output, with the URL graphjin-secret-init writes
// to subscriptions.url in the GraphJin config folder. Without that file it
// uses subscriptions.password and the neko-db host.
const (
	subscriptionsListen   = "0.0.0.0:8089"
	subscriptionsHost     = "neko-db"
	subscriptionsPort     = 5432
	subscriptionsDBName   = "neko"
	subscriptionsRole     = "neko_subscriptions"
	subscriptionsPassword = "subscriptions.password"
	subscriptionsURL      = "subscriptions.url"
	subscriptionsPoll     = 2 * time.Second
)

// subscriptionsConfig is GraphJin's config for the subscriptions service.
// It serves no MCP, no agent and no auth: only web and the worker reach it,
// on the stack's private network. GraphJin's own reload_on_config_change
// stays off, since it reloads by re-executing the process; serve swaps the
// service in place instead.
const subscriptionsConfig = `app_name: OpenNeko subscriptions
production: false
log_format: plain
log_level: info
http_compress: true
reload_on_config_change: false
auth:
  type: none
auth_fail_block: false
cors_allowed_origins:
  - http://127.0.0.1
  - http://worker
  - http://web
default_limit: 100
enable_tracing: false
mcp:
  disable: true
db_schema_poll_duration: 10s
subs_poll_duration: 5s
databases:
  neko:
    type: postgres
    connection_string: %s
    dbname: %s
    pool_size: 5
    max_retries: 3
`

// subscriptionsServer owns the :8089 listener for the life of serve and
// routes it to the current GraphJin. When the role URL changes it
// builds a new GraphJin, switches requests to it and closes the old one, so
// nothing restarts. New schema reaches GraphJin through its schema poll.
type subscriptionsServer struct {
	dir     string
	handler atomic.Value // http.Handler

	mu      sync.Mutex
	current *gjserv.HttpService
	cleanup func()
	url     string
	lastErr string
}

// startSubscriptions binds the subscriptions listener and keeps a GraphJin
// behind it. Until the first GraphJin is up, requests get 503.
func startSubscriptions(configDir string) error {
	listener, err := net.Listen("tcp", subscriptionsListen)
	if err != nil {
		return fmt.Errorf("subscriptions GraphJin: %w", err)
	}
	s := &subscriptionsServer{dir: configDir}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "openneko serve: subscriptions GraphJin listener stopped: %v\n", err)
			os.Exit(1)
		}
	}()
	go func() {
		for {
			s.refresh()
			time.Sleep(subscriptionsPoll)
		}
	}()
	return nil
}

func (s *subscriptionsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, _ := s.handler.Load().(http.Handler)
	if handler == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"errors":[{"message":"subscriptions GraphJin is starting"}]}`))
		return
	}
	handler.ServeHTTP(w, r)
}

// refresh starts a GraphJin when there is none or the role URL changed.
// Failures are logged once each and retried on the next poll.
func (s *subscriptionsServer) refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	url, err := readSubscriptionsURL(s.dir)
	if err != nil {
		s.report(err)
		return
	}
	if s.current != nil && url == s.url {
		return
	}
	next, cleanup, err := newSubscriptionsGraphJin(url)
	if err != nil {
		s.report(err)
		return
	}
	mux := http.NewServeMux()
	if err := next.Attach(mux); err != nil {
		_ = next.Close()
		cleanup()
		s.report(err)
		return
	}
	s.handler.Store(http.Handler(mux))
	if s.current != nil {
		_ = s.current.Close()
		s.cleanup()
		fmt.Fprintln(os.Stderr, "openneko serve: subscriptions GraphJin switched to the new role URL")
	}
	s.current, s.cleanup, s.url, s.lastErr = next, cleanup, url, ""
}

func (s *subscriptionsServer) report(err error) {
	if msg := err.Error(); msg != s.lastErr {
		s.lastErr = msg
		fmt.Fprintf(os.Stderr, "openneko serve: subscriptions GraphJin unavailable, retrying: %v\n", err)
	}
}

// readSubscriptionsURL reads subscriptions.url, or builds the URL for the
// neko-db host from subscriptions.password when secret-init wrote no URL.
func readSubscriptionsURL(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, subscriptionsURL))
	if err == nil {
		if url := strings.TrimSpace(string(raw)); url != "" {
			return url, nil
		}
		return "", errors.New(subscriptionsURL + " is empty")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw, err = os.ReadFile(filepath.Join(dir, subscriptionsPassword))
	if err != nil {
		return "", err
	}
	password := strings.TrimSpace(string(raw))
	if password == "" {
		return "", errors.New(subscriptionsPassword + " is empty")
	}
	url := neturl.URL{
		Scheme: "postgres",
		User:   neturl.UserPassword(subscriptionsRole, password),
		Host:   net.JoinHostPort(subscriptionsHost, strconv.Itoa(subscriptionsPort)),
		Path:   "/" + subscriptionsDBName,
	}
	return url.String(), nil
}

// newSubscriptionsGraphJin builds a GraphJin for the role URL.
func newSubscriptionsGraphJin(url string) (*gjserv.HttpService, func(), error) {
	config, err := renderSubscriptionsConfig(url)
	if err != nil {
		return nil, nil, err
	}
	// NewConfig reads only this text: GraphJin's GJ_* environment
	// overrides, which some overlays set for the customer GraphJin, would
	// otherwise point this service at the customer database. GraphJin keeps
	// dev-mode files under its config path, so give it a private folder.
	conf, err := gjserv.NewConfig(config, "yaml")
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "openneko-subscriptions-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	conf.ConfigPath = dir
	gj, err := gjserv.NewGraphJinService(conf)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return gj, cleanup, nil
}

// renderSubscriptionsConfig fills in the role URL and its database name.
// String values are JSON-quoted, which YAML reads as plain strings.
func renderSubscriptionsConfig(url string) (string, error) {
	parsed, err := pgx.ParseConfig(url)
	if err != nil {
		return "", fmt.Errorf("%s: %w", subscriptionsURL, err)
	}
	if parsed.User != subscriptionsRole {
		return "", fmt.Errorf("%s must connect as %s", subscriptionsURL, subscriptionsRole)
	}
	if parsed.Database == "" {
		return "", fmt.Errorf("%s names no database", subscriptionsURL)
	}
	quote := func(value string) string {
		raw, _ := json.Marshal(value)
		return string(raw)
	}
	return fmt.Sprintf(subscriptionsConfig, quote(url), quote(parsed.Database)), nil
}
