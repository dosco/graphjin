# OpenNeko Plugins

Sandboxed plugins add new action kinds — Slack, Gmail, web search, Shopify, Sheets, and more. Browse the marketplace at **[open-neko.github.io/plugins](https://open-neko.github.io/plugins/)**.

Every plugin runs inside an OpenShell sandbox with outbound network limited to its manifest's allowlist. Secrets (bot tokens, API keys, OAuth tokens) live in `~/.config/openneko/secrets.json` (0600 perms) and never touch `openneko.plugins.json` (tracked) or `action_request.payload` (logged).

## Capabilities

Any combination of:

- **`action`** — typed handlers the agent invokes (e.g. Slack's `send_slack_message`).
- **`auth`** — singleton SSO provider for the deployment (e.g. Scalekit, magic link). Lights up "Sign in with X" on `/signin`. The login gate engages only once the plugin's sign-in credentials are configured — until then the deployment stays in single-operator mode. Two declaration knobs shape the flow: `provisioning: "manual"` marks the identity self-asserted (magic link proves mailbox possession only), so the core never auto-creates users — sign-in works solely for users an admin pre-provisioned, unknown emails are dropped silently at begin-auth, and the gate additionally refuses to engage until at least one active admin user exists (no lockout window). `loginHintRequired: true` makes the sign-in page require the email field before starting the flow. Env vars declared with `autogenerate: true` (e.g. an HMAC signing key) are minted and stored by the host at install/load — the operator is never prompted.
- **`connect`** — OAuth for account-level integrations. Two credential scopes: `operator` (e.g. Google Workspace — each operator authorises their own account at `/integrations`) and `deployment` (e.g. Scalekit workspace — one admin consents once and the org-wide token backs every call, stored under a reserved secrets slot). The `mcp-oauth` flow drives MCP-OAuth-2.1 endpoints (resource/AS discovery + dynamic client registration + PKCE) with the in-flight state bridged through an `oauthState` echo.

## Installing

```bash
openneko install @open-neko/plugin-parallel-search
```

The host CLI proxies into the running worker container via `docker exec`, so installs land inside the sandbox boundary. Plugins with required env values prompt at install time (hidden input) and write to the secrets file. Pass `--local` to bypass the proxy (for source-build dev flows where the worker runs via `pnpm dev`).

From a repo checkout:

```bash
pnpm openneko init
pnpm openneko install @open-neko/plugin-parallel-search
```

The registry watches `openneko.plugins.json` and the secrets file — no worker restart needed. Sandboxes start lazily on first use. `openneko doctor` checks your host can run the OpenShell runtime.

## Federated marketplaces

The official marketplace only ships first-party `@open-neko/*` plugins. Anyone can publish their own `marketplace.json`; operators add it explicitly:

```bash
openneko marketplace add https://example.com/marketplace.json
openneko install @example/openneko-plugin-foo
```

Non-official marketplaces are operator-trusted, not OpenNeko-vouched. The sandbox enforces capability declarations regardless of source. See [open-neko/plugins/CONTRIBUTING.md](https://github.com/open-neko/plugins/blob/main/CONTRIBUTING.md) to publish.

## Bypass marketplaces (`--unverified`)

Install directly from npm:

```bash
openneko install <npm-package-name> --unverified
```

The CLI prints a loud warning. Sandboxing and manifest enforcement still apply; only the marketplace integrity check is skipped.

## Install a community skill from a git URL

For pure-procedural-knowledge skills (no network, no secrets) shipped as agentskills.io-spec `SKILL.md` bundles:

```bash
# Whole-repo skill:
openneko install https://github.com/owner/repo

# Skill inside a monorepo:
openneko install https://github.com/owner/repo#skills/finance/dcf-model
```

The CLI shallow-clones, validates `SKILL.md`, and drops the folder under `~/.openneko/skills/<skill-name>/`. The worker picks it up on the next agent turn. URLs must be HTTPS against github / gitlab / codeberg.

## Install policy

`/settings/security` gates every install path. Defaults are secure: `--unverified` and git-URL installs are off; any signed-in operator opts in. (No admin/member separation today — that's a future change.)

| Switch | Controls |
|---|---|
| `allowUnverified` | `openneko install <pkg> --unverified` |
| `allowGitUrlInstalls` | `openneko install <git-url>` |
| `allowedMarketplaces` | Which `marketplace.json` URLs the install path trusts (official is always on) |

Flipping a switch off flags pre-existing installs — not yanked — so the registry surfaces them for manual `openneko remove`. Every install records `installSource` + `installedAt` + `policySnapshot`.

## Per-operator integrations (`/integrations`)

Plugins with `connect` authorise OAuth. Operator-scoped connectors (Google Workspace today): each operator visits `/integrations`, clicks **Connect**, runs OAuth in their browser, and their tokens land in their secrets slot. Deployment-scoped connectors (Scalekit workspace): an admin authorises once at `/integrations` or from the SSO setup page; the credential lands in the reserved deployment slot and is injected on every tool call, pre-refreshed near expiry. Disconnect wipes the credential. Refresh-token rotation happens inside the plugin sandbox and gets persisted by the worker.

## Host support

| Host | Plugin system |
|---|---|
| macOS arm64 (Apple Silicon) | ✓ supported |
| macOS x86_64 (Intel) | ✓ supported |
| Linux x86_64 | ✓ supported |
| Linux arm64 | ✓ supported |
| Windows | ✗ WSL2 viability under evaluation |

The OpenShell gateway and every sandbox run as containers, so the only host
requirement is `docker` on PATH — no KVM or hardware-virtualisation
acceleration. On macOS the gateway's state dir must live under `$HOME`
(see [OPENSHELL.md](OPENSHELL.md) → Deployment).

On unsupported hosts the plugin subsystem is disabled with a clear log line; OpenNeko itself still runs. The built-in `send_webhook` adapter (no sandbox) remains as an escape hatch.
