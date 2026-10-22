# OpenNeko

[![License](https://img.shields.io/badge/license-Apache_2.0-0B64A0)](LICENSE)
[![Release](https://img.shields.io/github/v/release/open-neko/openneko)](https://github.com/open-neko/openneko/releases/latest)
[![Self-hosted · Docker](https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white)](INSTALL.md)
[![openneko.app](https://img.shields.io/badge/openneko.app-website-111111)](https://openneko.app)
[![Stars](https://img.shields.io/github/stars/open-neko/openneko?style=social)](https://github.com/open-neko/openneko/stargazers)

**OpenNeko watches your business data, flags what's worth a look, and drafts the next action — for you to approve.** Self-hosted, on your infrastructure, with whichever LLM you prefer.

> The intelligence is rented; the findings, rules, and decisions are yours. Models keep changing — the memory of how your business actually runs (promises, exceptions, baselines, decisions) shouldn't live inside the vendor that rents you the model. OpenNeko keeps the agent **and** that memory on your infrastructure.

![OpenNeko on mobile — Briefing, Ask, Workflows](cfo-briefing.png)

## Quickstart

You'll need **Docker** and **one LLM provider API key**.

```bash
curl -fsSL https://openneko.app/install.sh | sh
mkdir -p ~/openneko && cd ~/openneko
openneko setup --mode demo
```

The installer detects your OS/arch (Homebrew on macOS, a checksum-verified release binary on Linux) and checks for Docker. `openneko setup` then runs preflight checks, brings up the stack, and walks you through configuration **right in the terminal** — admin password, data source, model provider + key. Prefer a browser? Choose **browser** at the first prompt (or pass `--skip-onboarding`) to finish the wizard at [http://localhost:3000](http://localhost:3000).

The demo seeds three watchers against sample data — kick off **Slow-Ship Operations** from `/workflows` and watch an *"orders stuck in pending > 5 days"* finding land on your Briefing.

Full propose-and-approve walkthrough, the live trial (order simulator + scenario injector), and connecting your own data → **[INSTALL.md](INSTALL.md)**.

Hosting several smaller customers on one VM? Named installations isolate each
customer's Compose project, databases, secrets, sandbox state, backups, ports,
and Docker network. See [Multiple customer instances](INSTALL.md#multiple-customer-instances-on-one-host).

## What you get

The full feature catalog, in plain language, lives in **[FEATURES.md](FEATURES.md)** — ask-anything answers, chat-first administration, watchers, channels (Slack / Telegram), personal-vs-team knowledge, and the verifiable security model. The highlights:

- **Operational findings on the Briefing.** A SKU below reorder, orders stuck in *pending* for days, payment retries piling up — checked on a schedule and posted as findings.
- **Watchers you describe in plain English.** *"Alert me when any SKU's on-hand stock drops below its reorder point."* OpenNeko schedules it, runs it, and writes up what it found.
- **Actions drafted, not auto-fired.** Slack alerts, Gmail follow-ups, Sheets updates, Shopify writes — proposed with the finding that triggered them, queued for your approval. Write a rule when you want a specific class of safe action to auto-fire.
- **Follow-up questions about your data, in chat.** *"Which territories drove this week's revenue drop?"* — drill in on a finding without leaving the app or writing SQL.
- **A complete record of what fired, when, and why.** Every proposal, decision, and execution stays against the finding that triggered it — auditable, searchable, yours.

## How your data gets in

OpenNeko reads through **[GraphJin](https://graphjin.com)**, a GraphQL gateway you point at data you already have:

- **Databases** — Postgres, MySQL, and [more](https://graphjin.com), with an auto-generated query surface.
- **Files, external APIs, custom code** — first-class GraphQL fields via GraphJin's script layer.

One consistent surface no matter where the data lives — you don't write per-connector plumbing.

## What stays yours

OpenNeko separates the *intelligence* (the model) from the *memory* (your business's context) on purpose. Swap the model whenever something better ships; the rest doesn't move.

The memory layer runs on your own Postgres, on your own infrastructure:

- **Findings & briefings** — every watcher run and result, kept against the watcher that produced it.
- **Pinned facts & learned rules** — what the agent figured out and you confirmed, kept separate from what the agent proposed and you haven't decided on yet.
- **Document library** — documents and spreadsheets you upload, private to you. The assistant searches them, reads them whole, and answers questions over spreadsheet data with SQL, citing the file and page or sheet it used.
- **Action policies** — your rules for what auto-fires, what queues, what's blocked.
- **Decision history** — every approval, rejection, and execution receipt.

Apache-2.0 core, self-hosted, single Postgres. Take a backup, take it with you — your memories, workflows and skills are plain-Markdown [Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) files any OKF-aware tool can read, and your library is the files you uploaded.

## Integrations, channels and sign-in

Integrations ship as **solution packs** — sources, API specs, actions, workflows and skills for an application, installed from **Admin → Settings → Solution packs** or `openneko pack install`. See **[PACKS.md](PACKS.md)**.

- **Channels are built in.** Slack, Telegram and the HUD come with the core; a pack turns a channel on by listing it in its manifest, and you add the credentials at **Admin → Settings → Channels**.
- **Sign-in is built in.** Enterprise SSO and directory sync come from Scalekit: authorize the workspace from **Admin → Settings → Single sign-on** (a guided checklist) and every Scalekit-backed IdP (Okta, Entra ID, Google Workspace…) lights up. Email-link sign-in is the lighter option for small teams.
- **Secrets never reach the model context.** Channel, sign-in and pack credentials live in the encrypted secrets store.

## Under the hood

- **Self-hosted via Docker.** One binary, one `start` command. Data lives on your Postgres.
- **Bring your own LLM.** The Ax agent runs against Anthropic, OpenAI, Google, Ollama, and other compatible providers. Change providers without changing the sandboxed agent runtime.
- **Agent kept away from your secrets.** Each run's agent loop runs in its own runner process; turns run in `openneko serve`, which holds the model key only for the turn, and the agent's terminal runs in a separate sandbox container as a user of its own, with no secrets and no route to the stack's databases or services. See [AGENT_RUNTIME.md](AGENT_RUNTIME.md).
- **Apache-2.0.** Read the source, self-host, fork, and build on it. OpenNeko trademarks are separately controlled — see [LICENSING.md](LICENSING.md) and [TRADEMARKS.md](TRADEMARKS.md).

## Evaluations

OpenNeko's data questions run on GraphJin's eval engine: 53 questions about a
frozen AdventureWorks snapshot, each graded against a GraphQL oracle. See
[evals/adventureworks/README.md](evals/adventureworks/README.md) to run it.

## Stack modes

`openneko start|setup|upgrade --mode prod|dev|demo` picks which Compose layers the
CLI deploys. Every mode shares the same core: app + records Postgres, migrations,
backup/restore, web, worker, three internal GraphJin instances (app
subscriptions, records, records-watch), plus the customer-facing GraphJin in
sources (agentic) mode, which `openneko serve` hosts together with the agent
runtime. The agent runtime (`openneko serve` and `agent-sandbox`)
is part of the core — it is the only way agents run, in every mode.

| Mode | Layers on top of core | What's different |
|------|----------------------|------------------|
| **prod** (default) | none | Core stack only. You connect your own data source; actions run for real. |
| **demo** | AdventureWorks sample DB, seed job, order simulator, scenario injector | Customer GraphJin points at the sample data. What `openneko setup --mode demo` gives you. |
| **dev** | none (reserved overlay) | For working on OpenNeko itself: Docker runs only the dependency services while web and worker hot-reload as host processes (`pnpm dev:setup && pnpm dev`, using the repo's root `compose.yml`) — see [CONTRIBUTING.md](CONTRIBUTING.md). |

**Solution packs** (e.g. `openneko pack install magento`) are not a mode: they
layer onto a running stack in any mode, adding sources, specs, and saved queries
to the customer GraphJin config at runtime.

## Docs

- [ROADMAP.md](ROADMAP.md) — implementation follow-ups, including RBAC and warm sandbox access revisions
- [PACKS.md](PACKS.md) — authoring solution packs, directory layout, connectors, validation, and contributions
- [CONTEXT_REPOSITORY.md](CONTEXT_REPOSITORY.md) — version history of skills and workflows, and an optional GitHub, GitLab or other git remote
- [INSTALL.md](INSTALL.md) — install, [upgrade](INSTALL.md#upgrade), requirements, troubleshooting, connecting your data, full demo trial
- [AGENT_RUNTIME.md](AGENT_RUNTIME.md) — how agent runs execute: the runner, `openneko serve` and the terminal sandbox, and what each holds
- [SECRETS.md](SECRETS.md) — secrets at rest and the optional Infisical vault (enterprise)
- [CONTRIBUTING.md](CONTRIBUTING.md) — dev setup, repo layout, pre-PR checks
- [evals/adventureworks/README.md](evals/adventureworks/README.md) — the AdventureWorks data eval on GraphJin's eval engine
- [CHANGELOG.md](CHANGELOG.md) — releases

## Issues

Please file bugs and feature requests at [github.com/open-neko/openneko/issues](https://github.com/open-neko/openneko/issues).

## Contributing

Pull requests are welcome — on your first PR a bot will ask you to sign a quick [Contributor License Agreement](CLA.md). See [CONTRIBUTING.md](CONTRIBUTING.md) for the developer setup, repository layout, and the checks to run before opening a PR.

## License

OpenNeko source code is licensed under the [Apache License 2.0](LICENSE). Third-party and modular components remain under their own licenses. The OpenNeko name and logo are governed by [TRADEMARKS.md](TRADEMARKS.md). See **[LICENSING.md](LICENSING.md)** for the full model.

## Author

Created by [Amit Deshmukh](https://openneko.app/#about).
