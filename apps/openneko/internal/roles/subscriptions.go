package roles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gjserv "github.com/dosco/graphjin/serv/v3"
)

// The subscriptions GraphJin serves workflow subscriptions to the worker
// from neko-db. `openneko serve` runs it beside the customer GraphJin when
// OPENNEKO_SUBSCRIPTIONS_DB names the database as host:port/dbname. It
// connects as neko_subscriptions, a role that may only read
// workflow_output, with the password graphjin-secret-init writes to
// subscriptions.password in the GraphJin config folder.
const (
	subscriptionsDBEnv     = "OPENNEKO_SUBSCRIPTIONS_DB"
	subscriptionsListenEnv = "OPENNEKO_SUBSCRIPTIONS_LISTEN"
	subscriptionsPassword  = "subscriptions.password"
	subscriptionsRole      = "neko_subscriptions"
	subscriptionsRetry     = 5 * time.Second
)

// subscriptionsConfig is GraphJin's config for the subscriptions service.
// It serves no MCP, no agent and no auth: only web and the worker reach it,
// on the stack's private network. reload_on_config_change stays off, since
// GraphJin reloads by re-executing the process, which would end every turn
// serve is running.
const subscriptionsConfig = `app_name: OpenNeko subscriptions
host_port: %s
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
    host: %s
    port: %d
    dbname: %s
    user: %s
    password: %s
    pool_size: 5
    max_retries: 3
`

// startSubscriptions starts the subscriptions GraphJin in the background
// when it is configured. It keeps retrying until neko-db accepts the role,
// so a slow database delays subscriptions but never the agent runtime.
func startSubscriptions(configDir string) {
	target := strings.TrimSpace(os.Getenv(subscriptionsDBEnv))
	if target == "" {
		return
	}
	listen := strings.TrimSpace(os.Getenv(subscriptionsListenEnv))
	if listen == "" {
		listen = "0.0.0.0:8089"
	}
	go func() {
		for {
			err := runSubscriptions(configDir, target, listen)
			fmt.Fprintf(os.Stderr, "openneko serve: subscriptions GraphJin unavailable, retrying: %v\n", err)
			time.Sleep(subscriptionsRetry)
		}
	}()
}

// runSubscriptions serves until GraphJin stops or fails to start.
func runSubscriptions(configDir, target, listen string) error {
	password, err := os.ReadFile(filepath.Join(configDir, subscriptionsPassword))
	if err != nil {
		return err
	}
	config, err := renderSubscriptionsConfig(target, listen, strings.TrimSpace(string(password)))
	if err != nil {
		return err
	}
	// NewConfig reads only this text: GraphJin's GJ_* environment
	// overrides, which some overlays set for the customer GraphJin, would
	// otherwise point this service at the customer database. GraphJin keeps
	// dev-mode files under its config path, so give it a private folder.
	conf, err := gjserv.NewConfig(config, "yaml")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "openneko-subscriptions-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) //nolint:errcheck
	conf.ConfigPath = dir
	gj, err := gjserv.NewGraphJinService(conf)
	if err != nil {
		return err
	}
	// Start blocks while GraphJin serves and returns once it is shut down.
	if err := gj.Start(); err != nil {
		return err
	}
	return errors.New("stopped")
}

// renderSubscriptionsConfig fills the config from host:port/dbname. String
// values are JSON-quoted, which YAML reads as plain strings.
func renderSubscriptionsConfig(target, listen, password string) (string, error) {
	if password == "" {
		return "", errors.New(subscriptionsPassword + " is empty")
	}
	hostPort, dbname, ok := strings.Cut(target, "/")
	host, port, okPort := strings.Cut(hostPort, ":")
	portNumber, err := strconv.Atoi(port)
	if !ok || !okPort || host == "" || err != nil || portNumber <= 0 || dbname == "" {
		return "", fmt.Errorf("%s=%q, want host:port/dbname", subscriptionsDBEnv, target)
	}
	quote := func(value string) string {
		raw, _ := json.Marshal(value)
		return string(raw)
	}
	return fmt.Sprintf(subscriptionsConfig, quote(listen), quote(host), portNumber, quote(dbname), quote(subscriptionsRole), quote(password)), nil
}
