# Install OpenNeko

Sections below **Command reference** are reference — not needed to get started.
What you're installing, feature by feature, in plain language: **[FEATURES.md](FEATURES.md)**.

## Requirements

- Docker Desktop on macOS, or Docker Engine + Docker Compose on Linux
- An API key for at least one supported model provider

## Install

```bash
curl -fsSL https://openneko.app/install.sh | sh
mkdir -p ~/openneko && cd ~/openneko
openneko setup --mode demo
```

The installer picks the right path for your platform — Homebrew on macOS, a checksum-verified release binary on Linux — and checks for Docker. `openneko setup` then runs preflight (Docker daemon up, host supported, ports free), brings up the stack, and configures it (see [Setup](#setup)).

`--mode demo` pulls pinned images and loads AdventureWorks sample data with three example watchers. For your own data, use `--mode prod` ([Use your own data](#use-your-own-data)).

### Manual install

Prefer to manage the binary yourself:

- **macOS (Homebrew):** `brew install open-neko/tap/openneko`
- **Linux:** download `openneko_<version>_linux_<arch>.tar.gz` from the [latest release](https://github.com/open-neko/openneko/releases/latest), verify it against `checksums.txt`, and put `openneko` on your `PATH`.

Then run `openneko setup` (guided), or `openneko start` to bring the stack up without the guided flow.

## Setup

`openneko setup` walks through the steps below **in the terminal**. Prefer a browser? Choose **browser** at the first prompt (or pass `--skip-onboarding`, or just run `openneko start`) and open [http://localhost:3000](http://localhost:3000) for the same wizard. Either way:

1. Choose an admin database password.
2. Confirm the pre-filled GraphJin URL (`--mode demo`) or enter your own (`--mode prod`).
3. Pick the model provider the agent should use (Anthropic / OpenAI / Google / Ollama / others). The Ax agent runs inside the OpenShell sandbox with the full tool set, including the chat-first management tools.
4. Add your provider API key.
5. Add an industry research provider, or skip.

Both paths drive the same web endpoints with the same validation (a live provider-key test, a data-source connectivity check), and each step persists independently — so you can start in the terminal and finish in the browser, or vice versa. For unattended installs (CI), pass `--admin-password`, `--provider`, `--provider-key`, etc. to run setup headless.

As a final optional step, `openneko setup` offers to install **first-party plugins** from the [official marketplace](https://open-neko.github.io/plugins/) — Slack, Shopify, Telegram, Google Workspace, web search, SSO. Pick from the list and each plugin prompts for its own configuration (API keys, tokens), saved into the sandbox. Skip it with `--skip-plugins`, or pre-select non-interactively with `--plugins @open-neko/plugin-slack,@open-neko/plugin-shopify`.

The AdventureWorks seed pre-fills business onboarding (`AdventureWorks Cycles`, fiscal year `July`, seats `CEO`/`CFO`/`COO`, priorities `Defend wholesale margins` / `Grow DTC in Europe`). Otherwise fill it in at `/onboarding`.

## Multiple customer instances on one host

Give every customer a stable lowercase instance name. A named installation gets
its own Compose project, databases and named volumes, encrypted config and
secrets, OpenShell PKI/state, backup namespace, runtime markers, and Docker
subnet.

```bash
# Bind customer apps to loopback when Caddy, nginx, or another TLS proxy is
# the public entry point. Ports and the subnet are persisted by setup.
openneko --instance acme setup \
  --mode prod \
  --port 3101 \
  --openshell-port 18101 \
  --bind-address 127.0.0.1

openneko --instance globex setup \
  --mode prod \
  --port 3102 \
  --openshell-port 18102 \
  --bind-address 127.0.0.1
```

Route a separate TLS hostname to each loopback web port. For example, with
Caddy:

```caddyfile
acme.example.com {
  reverse_proxy 127.0.0.1:3101
}

globex.example.com {
  reverse_proxy 127.0.0.1:3102
}
```

Only the reverse proxy needs public `80`/`443` firewall access. The customer web
ports above and each OpenShell gateway stay on host loopback; databases,
GraphJin, brokers, and backup services stay inside their private Docker
networks.

For named instances, omitting `--port`, `--openshell-port`, or
`--docker-subnet` makes setup choose an available value and persist it. Inspect
the resulting inventory with:

```bash
openneko instances
```

Target every later operation explicitly:

```bash
openneko --instance acme status
openneko --instance acme logs -f
openneko --instance acme upgrade
openneko --instance acme backup now
openneko --instance acme stop
```

Named settings live under
`~/.config/openneko/instances/<name>/installation.json`; durable runtime and
OpenShell state live under
`${XDG_STATE_HOME:-$HOME/.local/state}/openneko/instances/<name>/`. Do not reuse
an instance name for two customers on the same Docker daemon. Each customer is
still a complete OpenNeko stack, so size VM memory, disk, and backup retention
for the number of concurrent instances.

Named instances are an operational and data-placement boundary, not a
hypervisor security boundary. A host administrator or anyone with Docker daemon
access can reach every stack. Use a separate VM or equivalent tenant boundary
when a customer contract, threat model, or compliance regime requires host-level
isolation.

## Use your own data

```bash
mkdir -p ~/openneko && cd ~/openneko
openneko setup --mode prod
```

Enter your GraphJin base URL when setup asks for it (terminal or browser). If GraphJin runs on your host, use `http://host.docker.internal:8080` — OpenNeko appends the GraphQL and MCP paths automatically.

## Plugins

```bash
openneko install @open-neko/plugin-parallel-search
```

Browse the marketplace at [open-neko.github.io/plugins](https://open-neko.github.io/plugins/). `openneko doctor` checks your host can run sandboxed plugins. Full sandbox model in **[PLUGINS.md](PLUGINS.md)**.

---

*Reference below — not needed to get started.*

## Command reference

```bash
openneko [--instance name] setup [--mode prod|dev|demo] [--port N] \
  [--openshell-port N] [--bind-address IPv4] [--docker-subnet IPv4/24]
openneko instances                 # configured host installations
openneko start [--mode prod|dev|demo] [--detach]
openneko upgrade [--version vX.Y.Z] [--mode auto|prod|dev|demo]
openneko status                    # docker compose ps proxy
openneko logs [service…] [-f]
openneko stop [--volumes]          # --volumes wipes data
openneko migrate                   # apply pending migrations against running neko-db
openneko seed adventureworks       # one-shot demo data load (already done by --mode demo)
openneko [--instance name] graphjin import <customer-config.yml|->
openneko reset [--all]             # tear down + clear local config (--all also wipes secrets/marketplaces)
openneko doctor                    # host check, docker daemon, manifest state
openneko version
```

Modes:

- **`prod`** (default) — the production control, records, customer-data,
  backup/storage, and OpenShell services. Ten containers remain running after
  the four startup jobs complete.
- **`dev`** — source-checkout workflow: use `pnpm dev:setup` for Docker-only dependencies plus AdventureWorks demo data, then `pnpm dev` for hot-reloaded web/worker from source.
- **`demo`** — core + AdventureWorks: `adventureworks-db`, `adventureworks-init`, `neko-adventureworks-seed`. The full live-trial flow (continuous order trickle + scenario injector) lives in `compose.adventureworks.yml` and needs the [Build from source](#build-from-source-advanced) path.

`~/.config/openneko/compose.override.yml` is auto-applied if present (last `-f` to docker compose).

`graphjin import` accepts a YAML fragment with `sources`, `tables`, and/or
`relationships`. Use `-` to read it from standard input. It requires a running
production installation, validates the change through GraphJin, seals source
credentials, and preserves OpenNeko-managed authentication and keystore settings.

### Audit logging failure alerts

Every failed tamper-evident audit append emits a structured
`security.audit_logging_failure` event to stderr. The worker also exposes
process-local state at `GET /health/security`; monitor for
`status: "degraded"` or `auditLogging.healthy: false`.

For immediate delivery independent of PostgreSQL, configure an HTTPS webhook
on both processes that can write audit events:

```yaml
# ~/.config/openneko/compose.override.yml
services:
  web:
    environment:
      OPENNEKO_AUDIT_FAILURE_WEBHOOK_URL: https://alerts.example/audit-failure
      OPENNEKO_AUDIT_FAILURE_WEBHOOK_TOKEN: ${OPENNEKO_AUDIT_FAILURE_WEBHOOK_TOKEN}
  worker:
    environment:
      OPENNEKO_AUDIT_FAILURE_WEBHOOK_URL: https://alerts.example/audit-failure
      OPENNEKO_AUDIT_FAILURE_WEBHOOK_TOKEN: ${OPENNEKO_AUDIT_FAILURE_WEBHOOK_TOKEN}
```

The token is optional and is sent as a bearer token. Webhook delivery has a
two-second timeout, is fire-and-forget, and never blocks the governed operation.
Alert delivery failures are themselves emitted as structured critical stderr
events.

### Agent & plugin sandboxing

The agent loop **and** plugins always run inside OpenShell policy
sandboxes — default-deny egress, and the model API key never enters the
sandbox (the gateway's egress proxy injects it on the wire; the box only
ever holds an opaque placeholder). The containerized OpenShell gateway is
part of the stack and the agent image is pre-pulled at install (never on
the first chat). State dir is handled per-platform automatically (under
`$HOME` on macOS/OrbStack, `/var/lib/openneko/openshell` on Linux). Needs
Docker — already required to run OpenNeko.


## Upgrade

`openneko upgrade` is the normal stack upgrade path. It:

1. Pulls the latest OpenNeko service, agent, and plugin-base images.
2. Detects the mode recorded by your last `setup` or `start` (`prod`, `dev`, or `demo`) and recreates that stack detached.
3. Re-runs every idempotent initializer against the existing volumes, including runtime-artifact repair and pending migrations.
4. Waits for every declared service health check; a crash loop or unreachable data plane fails the upgrade instead of being reported as success.
5. Persists the new version and removes old OpenNeko image tags only after the stack is healthy (unless you pass `--no-prune`).

Run it from the same install directory you use for `openneko start`:

```bash
cd ~/openneko
openneko upgrade
```

Install a specific image version/tag:

```bash
openneko upgrade --version v1.18.0
# "1.18.0" is accepted too and is normalized to "v1.18.0".
```

Older installs without a saved mode marker are detected from their existing
Docker Compose project (`openneko`, `openneko-dev`, or `openneko-demo`). If more
than one OpenNeko stack exists on the machine, pass the intended mode explicitly:

```bash
openneko upgrade --mode demo
```

The command persists the selected image tag in `.openneko/runtime/.image-version`,
so later `openneko start` runs the same tag. Upgrading the host binary is still
recommended when a release includes CLI changes.

### macOS

```bash
brew update
brew upgrade openneko
cd ~/openneko
openneko upgrade
```

> **Installed before 1.17.2?** Those releases shipped a Homebrew *formula*; 1.17.2+ ships a *cask*, so `brew upgrade openneko` won't move you across. Switch once:
>
> ```bash
> brew uninstall --formula openneko
> brew install --cask open-neko/tap/openneko
> ```
>
> After that, `brew upgrade openneko` keeps the cask current as normal.

### Linux

```bash
TAG=$(curl -fsSL https://api.github.com/repos/open-neko/openneko/releases/latest | grep -oE '"tag_name": *"[^"]+"' | head -1 | cut -d'"' -f4)
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSL "https://github.com/open-neko/openneko/releases/download/$TAG/openneko_${TAG#v}_linux_$ARCH.tar.gz" | tar -xz openneko
sudo install -m 0755 openneko /usr/local/bin/ && rm -f openneko
cd ~/openneko
openneko upgrade
```

### What `openneko start` does

1. **Migrate.** Connects to `neko-db`, takes a Postgres advisory lock, applies pending embedded migrations, releases the lock. Idempotent.
2. **Compose up.** Writes embedded compose files to `.openneko/runtime/` and runs `docker compose up`. Docker pulls the pinned image tags.

`openneko stop` doesn't touch volumes. Use `--volumes` for a clean slate (wipes the metadata DB, demo state, agent workspaces).

## Reset

```bash
openneko stop --volumes   # wipes data; keeps secrets + marketplaces
openneko reset --all      # wipes everything including secrets and marketplaces
```

## Ports

The packaged stack publishes only two host ports:

- Web app: `3000` by default
- OpenShell gateway: loopback-only `18080` by default

Postgres, GraphJin, worker broker, backup API, and demo data services remain on
the private Docker network. Web is the only port intended to be reachable from
another machine; the OpenShell host mapping exists only for local sandbox
callbacks.

Configure and persist ports during guided installation:

```bash
openneko setup --mode prod --port 3001 --openshell-port 18081
```

For the backward-compatible unnamed installation, `OPENNEKO_PORT` and
`OPENSHELL_PORT` remain available as one-process environment overrides. Named
instances use their persisted assignments; re-run setup with explicit flags to
change them. `--bind-address 127.0.0.1` restricts the web listener to the host
for a reverse-proxy deployment.

## Build from source (advanced)

> Needed for developing OpenNeko itself or running the full live trial (continuous order trickle + scenario injector).

```bash
git clone https://github.com/open-neko/openneko.git
cd neko
export OPENSHELL_STATE_DIR="$PWD/.openneko/openshell"
docker compose -f compose.yml -f compose.openshell.yml -f compose.adventureworks.yml up -d --build
docker compose -f compose.yml -f compose.openshell.yml -f compose.adventureworks.yml run --rm neko-adventureworks-seed
```

### Live trial data

The source compose trickles fresh sales orders into the sample DB every 10 minutes. Briefing numbers drift, cron workflows fire, runs accumulate on `/runs`. External actions use their configured adapters and approval policies.

```bash
AW_SIM_INTERVAL_SEC=300 AW_SIM_ORDERS_MIN=1 AW_SIM_ORDERS_MAX=5 \
  docker compose -f compose.yml -f compose.openshell.yml -f compose.adventureworks.yml up -d
```

Disable the trickle: `AW_SIM_ENABLED=0`.

### Watch the loop fire end-to-end

The seed pre-loads three watchers:

- **Daily Revenue Health Check** (9am cron) — yesterday's revenue vs trailing 7-day average.
- **Revenue Drop Alert** (hourly) — per-territory current hour vs same-hour-of-week baseline over 4 weeks; proposes a Slack alert if any territory falls below 50%.
- **Slow-Ship Operations** (8:30am cron) — orders stuck in *pending* > 5 days.

To see the loop without waiting for an organic dip, fire the Germany scenario (stops new orders for territory 8 for three hours):

```bash
docker compose -f compose.yml -f compose.openshell.yml -f compose.adventureworks.yml \
  exec adventureworks-scenario-injector \
  /scripts/scenario-injector.sh fire germany-revenue-drop
```

Wait ~15 minutes, then click **+ Run now** on **Revenue Drop Alert** in `/workflows`. A finding lands on the Briefing; a proposed Slack alert queues for approval. Click approve; the receipt lands under **Fired on your behalf**.

Then write your own watcher from `/work`, and swap AdventureWorks for your data — see [Use your own data](#use-your-own-data).

## Developer setup

Stack pieces in Docker, app processes from source:

```bash
corepack enable
pnpm bootstrap
pnpm dev:setup
pnpm dev
```

Web and worker run on the host with hot reload. Everything else runs in Docker.

- `pnpm dev:up` starts the backing services with `compose.dev.yml`: `neko-db`, `records-db`, both GraphJin data planes, `neko-graphjin`, `embedding`, `librarian`, the OpenShell gateway with the agent image, and AdventureWorks with its simulator. It installs the pinned GraphJin and OpenShell CLIs into `.openneko/dev/bin`, rebuilds a GraphJin image that reports another version than the pin, runs migrations from source, writes the GraphJin JWT secret, and registers the OpenShell gateway for host processes.
- `pnpm dev:seed` seeds demo workflows. `pnpm dev:setup` runs both steps.
- `pnpm dev` runs `next dev` on port 3000 and `tsx watch` for the worker. Edits to `apps/web`, `apps/worker`, and workspace packages reload without an image build.
- `scripts/dev-env.sh` holds the host settings. Each service a host process calls has a loopback port; `pnpm dev:web` and `pnpm dev:worker` source the file.
- The customer GraphJin runs in sources mode from `db/graphjin/dev.sources.example.yml`. Its config lives in `.openneko/dev/graphjin`, and the worker manages it like the core stack.
- OpenNeko state uses your XDG config home. Set `OPENNEKO_DEV_CONFIG_HOME=$PWD/.openneko/dev/config` to keep a worktree's stack separate, and a distinct `COMPOSE_PROJECT_NAME` with its ports when two stacks run at once.

Without `neko-graphjin`, subscription-chained workflows stay silent (`subscription manager ready (0 active)` in worker logs).

Install host-only CLIs the worker shells out to (Docker images already include them):

```bash
./scripts/install-clis.sh
```

Installs the GraphJin CLI.

In the dev flow, use `http://localhost:8080` for customer-data GraphJin in the setup wizard.

### Working on the openneko binary

```bash
cd apps/openneko
go test ./... -count=1
go test -tags=integration -count=1 -timeout 10m ./internal/db/...   # pgvector via testcontainers
go build -o /tmp/openneko ./cmd/openneko
```

Sync embedded migrations after editing `db/migrations/`:

```bash
apps/openneko/scripts/sync-migrations.sh
```

CI runs this with `--check` and fails on drift.

## Troubleshooting

**Docker not running.** Start Docker Desktop or the daemon; re-run `openneko start`.

**Port in use.** Re-run setup with `--port` / `--openshell-port`, or use the
equivalent `OPENNEKO_PORT` / `OPENSHELL_PORT` environment overrides.

**Image pull `unauthorized`.** Confirm packages are public at https://github.com/orgs/open-neko/packages.

**Worker crashes on boot.** Check `openneko logs worker`. `ERR_MODULE_NOT_FOUND` for a workspace dep means an old binary — `brew upgrade openneko` (≥ 1.7.3).

**GraphJin connection fails.** `--mode demo` uses internal `http://graphjin:8080` (pre-filled). Dev mode (`pnpm dev`) on the host uses `http://localhost:8080`.

**Workflow subscriptions don't fire.** `neko-graphjin` must be healthy. The worker logs `subscription manager ready (N active)` on boot — if `N=0` despite subscriptions in the DB, GraphJin is unreachable or its password drifted. Rotate via `/setup`, then `openneko stop && openneko start`.

**Provider key fails.** Confirm it's active, has billing/quota, and matches the provider in `/settings`.

**Stale `pnpm dev` processes hammering port 5432.** `tsx watch` processes survive `openneko stop` and reconnect with a stale password (`password authentication failed for user "neko"` in logs):

```bash
pkill -f "tsx.*apps/worker/src/index"
pkill -f "tsx.*apps/web"
openneko stop --volumes && openneko start --mode demo --detach
```

**Clean slate.**

```bash
openneko reset --all
brew uninstall openneko && brew untap open-neko/tap   # if also reinstalling
pkill -f "tsx.*apps/(worker|web)"                     # if you ever ran pnpm dev
```
