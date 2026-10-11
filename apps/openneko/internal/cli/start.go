package cli

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/db"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
	"github.com/open-neko/neko/apps/openneko/internal/setup"
	"github.com/open-neko/neko/apps/openneko/internal/version"
)

func newStartCmd() *cobra.Command {
	var mode string
	var detach bool
	var skipMigrate bool
	var pullPolicy string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Bring up the OpenNeko stack",
		Long: `Bring up the OpenNeko stack via docker compose.

Modes:
  prod  Core services only (default; production stack)
  dev   Developer defaults; source checkouts should use pnpm dev:setup + pnpm dev
  demo  Core + AdventureWorks trial bundle`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			selectedMode, err := modeForStart(cmd, mode)
			if err != nil {
				return err
			}
			// Same host checks setup runs; `start` used to skip them and race
			// straight into an opaque compose failure (port conflict with a
			// neighboring stack, docker gone away). A stack already answering
			// legitimately holds the ports, so only a fresh bring-up checks.
			if !setup.NewClient(webBaseURL()).Ready(ctx) {
				if err := runPreflight(cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			return bringUpStack(ctx, cmd, selectedMode, bringUpOptions{
				detach:      detach,
				skipMigrate: skipMigrate,
				pullPolicy:  pullPolicy,
			})
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "prod", "Stack mode: prod|dev|demo")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run in the background after services start")
	cmd.Flags().BoolVar(&skipMigrate, "skip-migrate", false, "Skip running migrations on start (advanced)")
	cmd.Flags().StringVar(&pullPolicy, "pull", "", "Override compose pull policy: always|missing|never (default: compose decides)")
	return cmd
}

// bringUpOptions controls a stack bring-up. `start` and `setup` share the same
// staged flow; setup sets quiet to trim the install chatter into clean step
// lines (it adds --quiet-pull and skips the verbose pre-pull banner).
type bringUpOptions struct {
	detach      bool
	skipMigrate bool
	pullPolicy  string
	quiet       bool
}

// bringUpStack runs the staged bring-up shared by `start` and `setup`:
// reconcile both database volumes → private neko-migrate container → pre-pull
// the agent image → compose up the rest. Extracted from newStartCmd so setup
// reuses the exact same path.
func bringUpStack(ctx context.Context, cmd *cobra.Command, mode compose.Mode, opts bringUpOptions) error {
	m := mode
	if m == "" {
		m = compose.ModeProd
	}
	sup := compose.New(assets.ComposeFS)
	project, err := sup.ProjectName(m)
	if err != nil {
		return err
	}
	if err := configureBackupEnvironment(project); err != nil {
		return fmt.Errorf("configure backup repository: %w", err)
	}
	if warning := backupFailureDomainWarning(); warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
	// Pin compose's image tags to this binary's version unless the caller has
	// already set OPENNEKO_VERSION or an install-level image version marker.
	// That lets the smoke workflow (which builds openneko fresh from source, so
	// its embedded version is "0.0.0-dev") test against a real release tag, and
	// lets `openneko upgrade --version ...` keep using the requested tag.
	if os.Getenv("OPENNEKO_VERSION") == "" {
		imageVersion, err := sup.ImageVersion()
		if err != nil {
			return err
		}
		if imageVersion == "" {
			imageVersion = "v" + version.Version
		}
		_ = os.Setenv("OPENNEKO_VERSION", imageVersion)
	}
	if err := configurePinnedLibrarianImage(os.Getenv("OPENNEKO_VERSION")); err != nil {
		return err
	}
	if err := configureLibrarianCPULimit(runtime.NumCPU()); err != nil {
		return err
	}

	// SEC9: OpenShell is the only agent runtime.
	if err := configureOpenShellStateDir(); err != nil {
		return err
	}
	if err := configureOpenShellNetwork(m); err != nil {
		return err
	}

	files, err := sup.Materialize(m)
	if err != nil {
		return err
	}
	reapBeforePull(ctx)

	// Derive both the gateway URL and the dedicated role password before
	// compose interpolates the one-shot migration service's environment.
	configureOpenShellDBURL()
	previousSkipMigrate, hadSkipMigrate := os.LookupEnv("OPENNEKO_SKIP_MIGRATE")
	if opts.skipMigrate {
		_ = os.Setenv("OPENNEKO_SKIP_MIGRATE", "1")
	} else {
		_ = os.Unsetenv("OPENNEKO_SKIP_MIGRATE")
	}
	defer func() {
		if hadSkipMigrate {
			_ = os.Setenv("OPENNEKO_SKIP_MIGRATE", previousSkipMigrate)
		} else {
			_ = os.Unsetenv("OPENNEKO_SKIP_MIGRATE")
		}
	}()

	pullFlag := []string{}
	if opts.pullPolicy != "" {
		switch opts.pullPolicy {
		case "always", "missing", "never":
			pullFlag = []string{"--pull", opts.pullPolicy}
		default:
			return fmt.Errorf("--pull must be one of: always, missing, never (got %q)", opts.pullPolicy)
		}
	}
	quietPull := []string{}
	if opts.quiet {
		quietPull = []string{"--quiet-pull"}
	}

	// Stage 1: reconcile the two persisted database volumes before any
	// application container can read or write them. `run --rm` starts both
	// database services, waits for their healthchecks, and returns the storage
	// gate's exit status directly. This step is never bypassed by --skip-migrate:
	// schema changes may be operator-controlled, but an incompatible storage ABI
	// must always fail closed.
	reconcileArgs := append([]string{"run", "--rm", "-T"}, pullFlag...)
	reconcileArgs = append(reconcileArgs, quietPull...)
	reconcileArgs = append(reconcileArgs, "storage-reconcile")
	if code, err := sup.Run(ctx, project, files, reconcileArgs, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("database storage reconciliation failed"))
	}

	// Stage 2: run the one-shot schema migration to completion and capture its
	// exit code directly. Storage and database dependencies are already ready,
	// so --no-deps prevents Compose from launching a duplicate reconciliation.
	//
	// We deliberately do NOT use `up -d neko-migrate` + a separate
	// `compose wait`: on a re-deploy against an already-migrated database the
	// migration is a fast no-op that exits before the separate `wait` can
	// observe it, so `compose wait` reports "no containers for project" and
	// exits non-zero — surfacing as a spurious "database preparation failed".
	// A foreground `run` has no such race. The same command provisions the
	// gateway's dedicated DB role even when --skip-migrate skips schema changes.
	// No database port needs to cross the Docker network boundary.
	migrateArgs := append([]string{"run", "--rm", "-T", "--no-deps"}, pullFlag...)
	migrateArgs = append(migrateArgs, quietPull...)
	migrateArgs = append(migrateArgs, "neko-migrate")
	if code, err := sup.Run(ctx, project, files, migrateArgs, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("database preparation failed"))
	}

	// Pre-pull the sandbox images at install time so the gateway's first
	// sandbox-create (the user's first chat, or the first plugin RPC) never
	// blocks on a large pull inside the create timeout. `upgrade` already
	// pulled both; `start`/`setup` used to pull only the agent image, so a
	// fresh install's first plugin call raced a lazy multi-GB pull.
	// Best-effort: a failure just falls back to a lazy pull.
	for _, img := range []string{
		agentImageRef(os.Getenv("OPENNEKO_AGENT_IMAGE"), os.Getenv("OPENNEKO_VERSION")),
		pluginBaseImageRef(os.Getenv("OPENNEKO_PLUGIN_BASE_IMAGE"), os.Getenv("OPENNEKO_VERSION")),
		supervisorImageRef(os.Getenv("OPENNEKO_SUPERVISOR_IMAGE"), os.Getenv("OPENNEKO_VERSION")),
	} {
		if !opts.quiet {
			fmt.Fprintf(os.Stderr, "Pre-pulling sandbox image %s ...\n", img)
		}
		if err := sup.EnsureImage(ctx, img, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "warning: image pre-pull failed for %s (%v); it will pull on first use\n", img, err)
		}
	}

	// Stage 3: bring up the rest.
	upArgs := []string{"up"}
	if opts.detach {
		upArgs = append(upArgs, "-d")
	}
	upArgs = append(upArgs, pullFlag...)
	upArgs = append(upArgs, quietPull...)
	code, err := sup.Run(ctx, project, files, upArgs, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if code != 0 {
		return WithExit(code, nil)
	}
	return nil
}

// Release binaries carry the manifest-list digest produced by the same
// release workflow. Keep the readable :release tag in the reference while
// making Compose resolve exactly that immutable manifest. Explicit operator
// overrides always win; source/dev binaries have no embedded digest.
func configurePinnedLibrarianImage(imageVersion string) error {
	if os.Getenv("OPENNEKO_LIBRARIAN_IMAGE") != "" {
		return nil
	}
	pinned := strings.TrimSpace(version.LibrarianImage)
	prefix := "ghcr.io/open-neko/neko-librarian:" + imageVersion + "@sha256:"
	if !strings.HasPrefix(pinned, prefix) || len(pinned) != len(prefix)+64 {
		return nil
	}
	for _, char := range pinned[len(prefix):] {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return nil
		}
	}
	return os.Setenv("OPENNEKO_LIBRARIAN_IMAGE", pinned)
}

const librarianCPULimitEnv = "OPENNEKO_LIBRARIAN_CPUS"

// configureLibrarianCPULimit keeps the extractor bounded without requiring
// more CPUs than Docker can allocate on the current host. Operators can still
// provide an explicit fractional or lower limit through the environment.
func configureLibrarianCPULimit(hostCPUs int) error {
	if strings.TrimSpace(os.Getenv(librarianCPULimitEnv)) != "" {
		return nil
	}
	if hostCPUs < 1 {
		hostCPUs = 1
	}
	if hostCPUs > 4 {
		hostCPUs = 4
	}
	return os.Setenv(librarianCPULimitEnv, strconv.Itoa(hostCPUs))
}

// agentImageRef resolves the agent sandbox image: an explicit override wins,
// else the default repo at the running version.
func agentImageRef(override, version string) string {
	if override != "" {
		return override
	}
	return "ghcr.io/open-neko/agent:" + version
}

// openShellStateDirOverride returns the OPENSHELL_STATE_DIR to set for goos, or
// "" to keep the compose default. The containerized gateway bind-mounts its PKI
// and the per-sandbox JWT from this dir into sandboxes; the in-VM docker daemon
// must resolve the SAME host path. macOS/OrbStack only maps paths under the
// user's home into its Linux VM — a /var/lib/... source comes back as an empty
// mount and the sandbox crash-loops on a missing JWT — so on macOS the state
// dir must live under $HOME. On Linux the compose default
// (/var/lib/openneko/openshell) is correct and docker creates it. An
// already-set value is always respected.
func openShellStateDirOverride(goos, home, existing string) string {
	if goos != "darwin" || existing != "" {
		return ""
	}
	return filepath.Join(home, ".openneko", "openshell")
}

func configureOpenShellStateDir() error {
	if stateDir, ok, err := instance.StateDir(); err != nil {
		return err
	} else if ok {
		// A named stack's PKI and gateway state must never inherit a global
		// OPENSHELL_STATE_DIR from another customer.
		dir := filepath.Join(stateDir, "openshell")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.Setenv("OPENSHELL_STATE_DIR", dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := openShellStateDirOverride(runtime.GOOS, home, os.Getenv("OPENSHELL_STATE_DIR"))
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.Setenv("OPENSHELL_STATE_DIR", dir)
}

const (
	openShellNetworkSubnetEnv  = "OPENNEKO_DOCKER_SUBNET"
	openShellNetworkGatewayEnv = "OPENNEKO_DOCKER_GATEWAY"
	openShellNetworkIPRangeEnv = "OPENNEKO_DOCKER_IP_RANGE"
	openShellGatewayIPEnv      = "OPENSHELL_GATEWAY_IP"
)

// configureOpenShellNetwork keeps each installation's private Compose network
// and gateway address stable. The host-networked Docker supervisor can route
// to the private service and broker IPs on this network.
//
// Each stack mode gets a distinct /24 so prod/dev/demo can coexist. Operators
// with an overlapping host route can override OPENNEKO_DOCKER_SUBNET; when the
// Docker bridge gateway or OpenShell gateway IP is omitted we derive the first
// and second usable addresses from that subnet, respectively.
func configureOpenShellNetwork(mode compose.Mode) error {
	subnet := os.Getenv(openShellNetworkSubnetEnv)
	if subnet == "" {
		subnet = defaultOpenShellSubnet(mode)
		if err := os.Setenv(openShellNetworkSubnetEnv, subnet); err != nil {
			return err
		}
	}

	prefix, err := netip.ParsePrefix(subnet)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 24 {
		return fmt.Errorf("%s must be an IPv4 /24 or larger subnet (got %q)", openShellNetworkSubnetEnv, subnet)
	}
	prefix = prefix.Masked()
	dynamicRange := os.Getenv(openShellNetworkIPRangeEnv)
	if dynamicRange == "" {
		dynamicRange = upperHalfPrefix(prefix).String()
		if err := os.Setenv(openShellNetworkIPRangeEnv, dynamicRange); err != nil {
			return err
		}
	}
	pool, err := netip.ParsePrefix(dynamicRange)
	if err != nil || !pool.Addr().Is4() || pool != pool.Masked() ||
		pool.Bits() < prefix.Bits() || !prefix.Contains(pool.Addr()) ||
		!prefix.Contains(lastIPv4Address(pool)) {
		return fmt.Errorf("%s must be contained by %s (got %q)", openShellNetworkIPRangeEnv, subnet, dynamicRange)
	}

	networkGateway := os.Getenv(openShellNetworkGatewayEnv)
	if networkGateway == "" {
		networkGateway = prefix.Addr().Next().String()
		if err := os.Setenv(openShellNetworkGatewayEnv, networkGateway); err != nil {
			return err
		}
	}
	bridgeAddress, err := netip.ParseAddr(networkGateway)
	if err != nil || !bridgeAddress.Is4() || !prefix.Contains(bridgeAddress) || pool.Contains(bridgeAddress) ||
		bridgeAddress == prefix.Addr() || bridgeAddress == lastIPv4Address(prefix) {
		return fmt.Errorf("%s must be a usable address in %s outside %s (got %q)", openShellNetworkGatewayEnv, subnet, dynamicRange, networkGateway)
	}

	gatewayIP := os.Getenv(openShellGatewayIPEnv)
	if gatewayIP == "" {
		// Docker reserves the first usable address for the network gateway.
		gatewayIP = prefix.Addr().Next().Next().String()
		if err := os.Setenv(openShellGatewayIPEnv, gatewayIP); err != nil {
			return err
		}
	}
	address, err := netip.ParseAddr(gatewayIP)
	if err != nil || !address.Is4() || !prefix.Contains(address) || pool.Contains(address) ||
		address == prefix.Addr() || address == bridgeAddress ||
		address == lastIPv4Address(prefix) {
		return fmt.Errorf("%s must be a usable address in %s outside %s (got %q)", openShellGatewayIPEnv, subnet, dynamicRange, gatewayIP)
	}
	return nil
}

func upperHalfPrefix(prefix netip.Prefix) netip.Prefix {
	raw := prefix.Masked().Addr().As4()
	value := binary.BigEndian.Uint32(raw[:])
	value += uint32(1) << uint(31-prefix.Bits())
	binary.BigEndian.PutUint32(raw[:], value)
	return netip.PrefixFrom(netip.AddrFrom4(raw), prefix.Bits()+1)
}

func lastIPv4Address(prefix netip.Prefix) netip.Addr {
	raw := prefix.Masked().Addr().As4()
	value := binary.BigEndian.Uint32(raw[:])
	value |= ^uint32(0) >> uint(prefix.Bits())
	binary.BigEndian.PutUint32(raw[:], value)
	return netip.AddrFrom4(raw)
}

func defaultOpenShellSubnet(mode compose.Mode) string {
	switch mode {
	case compose.ModeDev:
		return "172.29.1.0/24"
	case compose.ModeDemo:
		return "172.29.2.0/24"
	default:
		return "172.29.0.0/24"
	}
}

// configureOpenShellDBURL derives the gateway's database URL from the local
// config. The gateway keeps its state in neko-db, and the compose default URL
// carries the initial password — after the setup wizard rotates the neko
// role, a stale URL strands every sandbox create ("fetch settings failed:
// password authentication failed"), which kills all agent runs. The local
// config is the single source of the rotated password (ReadLocal decrypts
// it), so build the URL from it on every start. An operator-set
// OPENSHELL_DB_URL remains available for the legacy unnamed installation;
// named installations always derive their own internal database URL.
// openShellDBRole is the gateway's dedicated neko-db login role. It exists so
// the gateway's DB credential is independent of the `neko` admin password the
// operator rotates during setup — rotating that password never strands the
// gateway, so it needs no restart.
const openShellDBRole = "openshell"

const openShellDBPasswordEnv = "OPENNEKO_OPENSHELL_DB_PASSWORD"
const requireExplicitOpenShellDBPasswordEnv = "OPENNEKO_REQUIRE_EXPLICIT_OPENSHELL_DB_PASSWORD"

func configureOpenShellDBURL() {
	if instance.Current() != "" {
		// These values are derived from the selected instance's secret-key and
		// internal Compose database. Do not let a host-wide override point one
		// customer's gateway at another customer's credentials or database.
		_ = os.Unsetenv(openShellDBPasswordEnv)
		_ = os.Unsetenv("OPENSHELL_DB_URL")
	}
	if os.Getenv(openShellDBPasswordEnv) == "" {
		if pw, err := config.OpenShellDBPassword(""); err == nil {
			_ = os.Setenv(openShellDBPasswordEnv, pw)
		}
	}
	if os.Getenv("OPENSHELL_DB_URL") != "" {
		return
	}
	if u, ok := deriveOpenShellDBURL(); ok {
		_ = os.Setenv("OPENSHELL_DB_URL", u)
	}
}

// deriveOpenShellDBURL builds the gateway's neko-db URL for its dedicated role,
// with the stable per-install password derived from the host secret-key. ok is
// false only if that derivation fails (then the caller leaves the compose
// default in place).
func deriveOpenShellDBURL() (string, bool) {
	pw, err := openShellDBPassword()
	if err != nil {
		return "", false
	}
	database := "neko"
	if lc, _ := config.ReadLocal(""); lc.Pg != nil && lc.Pg.Database != "" {
		database = lc.Pg.Database
	}
	// Host/port are the compose network's, not the local config's: the
	// gateway dials neko-db inside the project network regardless of how
	// host-side tools reach the DB.
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(openShellDBRole, pw),
		Host:   "neko-db:5432",
		Path:   "/" + database,
	}
	return u.String(), true
}

// ensureOpenShellGatewayRole provisions the gateway's dedicated DB login role
// (idempotent — safe on every bring-up). The gateway shares the app's `public`
// schema, so the role is a member of the app role and inherits its privileges
// (it runs its own sqlx migrations and reads/writes without owning every
// table); its password is the stable per-install value from the host
// secret-key. Runs as the app superuser over the same connection migrations
// use — which is reachable by the time this is called (stage 1 waited on it).
func ensureOpenShellGatewayRole(ctx context.Context, password string) error {
	cc := defaultConn()
	conn, err := pgx.Connect(ctx, cc.DSN())
	if err != nil {
		return fmt.Errorf("connect to provision gateway DB role: %w", err)
	}
	defer conn.Close(ctx)

	appRole := cc.User
	if appRole == "" {
		appRole = "neko"
	}
	// pw is hex (no quotes/backslashes), so the literal is safe; the role names
	// are fixed/operator-owned identifiers, not request input.
	stmts := []string{
		fmt.Sprintf(`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s LOGIN; END IF; END $$`, openShellDBRole, openShellDBRole),
		fmt.Sprintf(`ALTER ROLE %s WITH LOGIN PASSWORD '%s'`, openShellDBRole, password),
		fmt.Sprintf(`GRANT %s TO %s`, appRole, openShellDBRole),
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("provision gateway DB role: %w", err)
		}
	}
	return nil
}

func openShellDBPassword() (string, error) {
	if pw := os.Getenv(openShellDBPasswordEnv); pw != "" {
		return pw, nil
	}
	// Managed migration containers must receive the exact password that the
	// host supervisor put in the gateway's OPENSHELL_DB_URL. Falling back to
	// the container volume's secret-key can derive a different password from
	// the host and silently ALTER ROLE underneath an already-running gateway.
	if envTruthy(requireExplicitOpenShellDBPasswordEnv) {
		return "", fmt.Errorf(
			"%s is required for managed migrations; refusing to derive a replacement from container-local config",
			openShellDBPasswordEnv,
		)
	}
	// Standalone host-side `openneko migrate` keeps its historical behavior:
	// derive from the host install's secret-key when no managed-container guard
	// is present.
	return config.OpenShellDBPassword("")
}

// defaultConn resolves the metadata-DB connection. Precedence: the local
// config.json (written by /setup after the operator rotates the bootstrap
// password) wins over env vars, which win over hardcoded defaults. This
// matches the TS reader in packages/db/src/local-config.ts so every consumer
// — Go migrate, web, worker, graphjin — converges on the rotated password.
func defaultConn() db.ConnConfig {
	conn := db.ConnConfig{
		Host:     envOr("NEKO_PG_HOST", "127.0.0.1"),
		Port:     envInt("NEKO_PG_PORT", envInt("OPENNEKO_DB_PORT", 5432)),
		User:     envOr("NEKO_PG_USER", "neko"),
		Password: envOr("NEKO_PG_PASSWORD", "secret"),
		Database: envOr("NEKO_PG_DATABASE", "neko"),
		SSLMode:  envOr("NEKO_PG_SSLMODE", "disable"),
	}
	local, _ := config.ReadLocal("")
	if local.Pg != nil {
		if local.Pg.Host != "" {
			conn.Host = local.Pg.Host
		}
		if local.Pg.Port != 0 {
			conn.Port = local.Pg.Port
		}
		if local.Pg.User != "" {
			conn.User = local.Pg.User
		}
		if local.Pg.Password != "" {
			conn.Password = local.Pg.Password
		}
		if local.Pg.Database != "" {
			conn.Database = local.Pg.Database
		}
		if local.Pg.SSLMode != "" {
			conn.SSLMode = local.Pg.SSLMode
		}
	}
	// OPENNEKO_PG_ENV_OVERRIDE=1 puts set env vars ahead of config.json, as
	// packages/db does for host processes in development.
	if os.Getenv("OPENNEKO_PG_ENV_OVERRIDE") == "1" {
		conn.Host = envOr("NEKO_PG_HOST", conn.Host)
		conn.Port = envInt("NEKO_PG_PORT", conn.Port)
		conn.User = envOr("NEKO_PG_USER", conn.User)
		conn.Password = envOr("NEKO_PG_PASSWORD", conn.Password)
		conn.Database = envOr("NEKO_PG_DATABASE", conn.Database)
		conn.SSLMode = envOr("NEKO_PG_SSLMODE", conn.SSLMode)
	}
	return conn
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		var x int
		_, err := fmt.Sscanf(v, "%d", &x)
		if err == nil {
			return x
		}
	}
	return fallback
}
