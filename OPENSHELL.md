# OpenShell sandboxed-agent runtime

OpenNeko can run the **agent loop itself** — not just plugins — inside an
[NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) policy sandbox, with the
worker/web process as the only trusted control plane. This page covers the
threat model, the architecture, how to turn it on, and what's verified.

> **Status:** OpenShell is the only runtime — agents and plugins always run in
> sandboxes (SEC11 made it the default, SEC9 removed the in-process and
> microsandbox paths). Both gateway shapes are verified end-to-end: the
> **host-process gateway** via the web UI on macOS, and the
> **containerised** gateway on Linux and macOS/OrbStack — see
> [Deployment](#deployment).

## Why

The least-trusted component historically ran with the most access: the
autonomous, prompt-injectable agent loop ran **in-process** on the worker/web
host, while only plugins were isolated. OpenShell flips that — the agent is
treated as untrusted code and runs in a default-deny sandbox:

- **Egress is default-deny**, allowed per `(host, binary)` — the agent can only
  reach the model endpoint (and whatever a plugin's manifest declares).
- **The model API key never enters the sandbox.** The box holds only an opaque
  `openshell:resolve:env:…` placeholder; the gateway's TLS-terminating egress
  proxy substitutes the real key on the wire. Verified: a grep of the sandbox
  env, filesystem, and config for the real key returns zero hits.
- **The control plane stays outside.** Secrets, the database, policy/approval,
  and channels live in the worker/web process; the agent reaches them only
  through a narrow, audited boundary.

## Architecture

```
 user ─▶ web/worker (CONTROL PLANE: DB, secrets, policy)
            │  runChatTurn: prologue (build prompt) ─┐
            │                                         ▼
            │                              ┌───────────────────────────┐
            │   launch + stream  ─────────▶│  OpenShell sandbox         │
            │   (events over stdout/SSE)   │   worker workspace + tsx    │
            │                              │   → Ax Harness (MCP)        │
            │                              └────────────┬──────────────┘
            │                                  egress (default-deny)
            │                                           ▼
            │                              ┌───────────────────────────┐
            │                              │ gateway egress proxy:      │
            │                              │  injects real key on wire  │──▶ model API
            │  ◀──────────────────────────┤  (placeholder never leaves) │
            ▼  epilogue (parse fences, persist)
```

`runChatTurn` splits into **prologue** (host: build the prompt from the DB),
**runCore** (the agent loop, in a sandbox — tests inject an in-process core), and **epilogue**
(host: parse action/workflow fences, persist, scrub). Only `runCore` moves into
the box; the trusted halves stay on the host. Both the worker (channel messages)
and the web chat route launch the sandbox through the same shared launcher
(`@neko/llm/work` `makeSandboxRunCore`).

**Runtime.** The image uses the v2.28 architecture: a production `pnpm deploy`
closure of the worker workspace, with the sandbox entrypoint executed from
source through its deployed `tsx` dependency. The Ax agent streams output and uses
MCP tools through the host-side broker. Action/workflow fences are parsed
host-side, and model credentials are injected by the gateway rather than copied
into the sandbox. GraphJin follows the same contract: work and workflow reads
use the native `neko_graphjin` MCP server with an actor-bound broker token;
background agent jobs use an explicit service identity. The GraphJin binary,
source URL, and credentials are absent from the agent image, so there is no
shell or raw-HTTP fallback when the broker contract is unavailable—the run
fails closed instead.

## Configuration (developer / advanced)

Prerequisites: an OpenShell gateway reachable by the worker/web, the agent image
(`openneko/agent`), and the `openshell` CLI on PATH.

Set these on the **worker** (channel runs) and/or the **web** process
(interactive chat) — both honour the same flags:

| Env var | Meaning |
|---|---|
| `OPENNEKO_AGENT_IMAGE` | The agent image (Dockerfile `agent` stage) |
| `OPENNEKO_AGENT_MODEL_PROVIDER` | Gateway-side provider name (auto-synced; see below) |
| `OPENNEKO_AGENT_MODEL_HOST` | Comma-separated egress hosts (e.g. `generativelanguage.googleapis.com`) |
| `OPENNEKO_AGENT_MODEL_KEY_ENV` | The env var the backend reads (e.g. `GEMINI_API_KEY`) |
| `OPENSHELL_GATEWAY` / `OPENSHELL_GATEWAY_ENDPOINT` | Gateway selection (mTLS name, or endpoint) |
| `OPENNEKO_METRIC_REFRESH_CONCURRENCY` | Metric refreshes that run at once, each in its own sandbox (worker only; default 2, never above the agent concurrency cap) |

**Provider auto-sync.** On startup the worker turns your configured model key
(`/settings`) into a gateway-side OpenShell provider automatically — you do not
run `openshell provider create` by hand. The proxy injects that credential on
egress; the box only ever sees the placeholder.

**Connecting binary.** Egress is matched on the executable reported by
`/proc/<pid>/exe`. OpenNeko owns this as part of its vendored agent-image
contract and does not accept an installation override: model egress binds to
the Ax Harness binary, `/usr/local/bin/ax-harness`.

## Plugins

Plugins run in OpenShell sandboxes too, behind the same `PluginRuntime`
interface — see [PLUGINS.md](PLUGINS.md).

## One-command install

```sh
openneko start
```

overlays the containerised gateway (`compose.openshell.yml`) onto the stack and
runs the agent + plugins in sandboxes. On macOS the CLI points the gateway's
state dir under `$HOME` automatically (see Deployment); on Linux it uses
`/var/lib/openneko/openshell`. Egress, the gateway-side provider, and the
connecting binary all self-derive from your `/settings` model config on first
boot — the env table below is only for manual / advanced setups.

A full source build uses the checked-in overlay and an absolute state path so
the host Docker daemon and gateway container resolve the same files:

```sh
export OPENSHELL_STATE_DIR="$PWD/.openneko/openshell"
docker compose -f compose.yml -f compose.openshell.yml up -d --build
```

The overlay builds the matching agent image and does not start web or worker
until an authenticated gateway readiness call succeeds.

## Deployment

- **Host-process gateway** (brew on macOS / binary + systemd on Linux) — the
  original verified path; the first agent-in-sandbox web-UI e2e ran on this.
- **Containerised gateway** (`compose.openshell.yml`, what `openneko start`
  uses) — validated end-to-end on **both Linux** (neko-vm) **and macOS/OrbStack**:
  gateway boots with mTLS, the Docker driver creates a sandbox, the per-sandbox
  JWT delivers, `exec` runs, and the launcher drives a real agent turn with the
  key isolated. Requires the Docker socket, mTLS PKI, and **matched
  host:container bind paths** for the state dir. The one macOS subtlety: OrbStack
  only maps paths under `$HOME` into its Linux VM, so `OPENSHELL_STATE_DIR` must
  live under `$HOME` (a `/var/lib/...` source yields an empty JWT bind-mount →
  crash-loop). `openneko start` sets this for you; if you run
  compose directly on macOS, export `OPENSHELL_STATE_DIR=$HOME/.openneko/openshell`.

## Verifying key isolation

```sh
# Inside a running agent sandbox — the real key must be ABSENT:
env | grep -c "<your-key-prefix>"          # → 0
grep -rl "<your-key-prefix>" /sandbox /tmp # → (nothing)
echo "$api_key"                            # → openshell:resolve:env:v…_api_key
```

A 200/real response from the model with the box holding only the placeholder
confirms the proxy injected the key on the wire — it never entered the sandbox.
