# OpenNeko Features

OpenNeko is an AI teammate for your business data: ask questions in plain
language, get trustworthy answers, and let it watch things for you — safely,
with every action approved and audited. This page describes everything that
has shipped, in plain language, organized by what it does for you. Each
feature links the pull request(s) that shipped it; the [complete PR
index](#complete-pr-index) at the bottom covers every PR in one table.

---

## Your own bots

The web app opens on chat. Each person makes their own bots and talks to them
the way they would text a colleague. Admins reach the full console from the
profile menu.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Bots you make yourself | Pick a name, a coat and a pattern on the New bot screen. Each bot gets a neko avatar and can use everything you can access. | #449 |
| One running chat per bot | A bot keeps one conversation. Desktop shows your bots in a sidebar beside the open chat. Long chats open at the latest message and load older turns as you scroll up. | #449 |
| Instructions the bot keeps current | The bot writes its own instructions from what you tell it and tunes them as you talk. | #449 |
| Routines in the chat | Tell a bot when to act: on a schedule, on an event, or when a value changes. Results post to its chat. A watcher stays quiet while the value stays the same. | #449 |
| Skills found by meaning | The bot finds the right skill among thousands by what the skill does, and you can edit a skill from the chat. | #449 |
| Connectors | The profile menu lists the connectors you can use and the ones coming next. | #449 |
| Existing chats carry over | On upgrade, each earlier chat thread becomes a bot with a random neko, so your history stays in the list. | #449 |

## Ask anything, get real answers

Talk to your company data the way you'd talk to an analyst. The assistant
finds the right tables on its own, double-checks its work, and presents
results as readable cards and charts — not raw data dumps.

| Feature | What it does for you | Shipped in |
|---|---|---|
| The Ask workspace | A persistent chat surface with threads, run history, and shareable per-thread URLs — your conversations with your data are first-class records, not ephemeral sessions. | #2, #4 |
| Live streaming runs | Watch the assistant work in real time — messages and tool activity stream in as they happen, and cancel takes effect instantly. | #3, #5 |
| Self-correcting answers | When a query comes back wrong or empty, the assistant notices, fixes its approach, and retries — instead of handing you a broken result. | #96 |
| Smart discovery | The assistant learns the paths through your data that answer common questions for your role, so repeat questions get faster and more reliable. | #96 |
| Rich answer cards | Answers arrive as tables, charts, and summary cards designed for the screen you're on, not walls of text — with headings scaled for conversation, not billboards. | #4, #78, #129 |
| On-demand data knowledge | Instead of a stale schema snapshot, the assistant carries a compact live map of your data — hub tables, join paths, query patterns — and looks up details exactly when it needs them. Answers reflect your data as it is today, at the speed of the old pre-loaded packs. | #96, #129, #132 |
| Hours-saved tracking | Every answer and action estimates the specialist time it saved you; a "Time saved" tile rides each thread and a live dashboard sparkline adds it up — value in hours, not vibes. | #88, #94 |
| A memory that works by meaning | Teach the assistant a fact once and it recalls it by meaning, not keywords — memory is embedding-backed and saved/searched by the agent itself. | #17 |
| Briefing → deep dive | Any briefing card opens into a full Ask thread seeded with that card's context — from "what's this number?" to a real investigation in one click. | #8 |
| Editable business profile | The assistant's understanding of your company is right there to read — click into it and edit it like a document. | #10 |
| Two runtimes, any model provider | Admins pick Hermes or Ax as the sandboxed agent runtime for the organisation. Ax is the default when the primary model provider supports it. Both keep the full administrative toolset, work with Anthropic, Google, OpenAI-compatible and other providers, and price each run from the same model price list. | #3, #112, #436, #449 |
| Information-dense Compact mode | A density toggle switches the whole app between a comfortable reading layout and an information-dense operator view. | #88 |

## Connect your data, conversationally

Hooking up databases and shaping who can see what no longer requires editing
config files. An admin does it from chat; the assistant proposes, the admin
approves.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Chat-first data sources | Register, enable, disable, or switch your data connections by asking — the assistant proposes the change as a card, and an admin approves it. | #96 |
| Chat-first source configuration | Admins can add data sources, define roles, and set read/write access on the data engine itself from a conversation — no YAML, no redeploy. | #101 |
| Credentials stay out of chat | Passwords and keys are never typed into the conversation. They're entered once in a secure form, referred to by name afterwards, and stored sealed — the assistant never sees the value. | #101 |
| Personal access passes | Every request to your data carries a short-lived pass identifying who's asking and what they're allowed to see — access is per-person, not one shared key. | #96 |
| Multi-tenant isolation | In shared deployments, every query is automatically fenced to your organisation's rows — no way to read a neighbour's data. | #96 |

## It works while you're away

OpenNeko isn't just question-and-answer. It runs an operating loop —
observe, understand, decide, act — watching metrics, surfacing findings,
and proposing actions, with brakes so it never runs away from you.

| Feature | What it does for you | Shipped in |
|---|---|---|
| The operating loop | Workflows observe your data on schedules, surface findings as observations, and propose actions through an approval stack — the full loop from data to decision. | #11 |
| Dashboard | Important observations are elevated into dashboard cards — a short, prioritised read on what changed and what needs you. | #11, #96 |
| At-a-glance stats | A four-number strip (runs, findings, approvals waiting, budget used) tells you the state of your assistant in two seconds. | #96 |
| Build workflows by chatting | Create, update, and look up workflows and approval rules in the same chat where you ask questions — every artifact links back to the conversation that produced it. | #46 |
| Retire workflows by @mention | Delete a noisy or obsolete workflow mid-conversation by @mentioning it — composer autocomplete included. | #110 |
| Row-level data triggers | Workflows fire the moment a row changes — stock dips below its reorder point, an account flips to at-risk — not just on a schedule. Wired in plain language when you save the workflow. | #50, #52, #63 |
| Watchers | Set a condition in plain language — "tell me if daily orders drop below 500" — and the assistant monitors it and alerts you when it trips. | #96 |
| Outside-world triggers | External systems can ping OpenNeko to kick off a workflow — so it reacts to events, not just schedules. | #96 |
| Action receipts | When a rule auto-approves an action, the receipt doesn't vanish — every fired action has a deep-dive page showing what ran, why, and on whose authority. | #15 |
| Code actions | The assistant can file issues and draft code patches as proposals — a human always reviews and applies them. | #96 |
| Mute and pause | Tired of a noisy topic? Mute it, or pause the assistant for the day with one tap — it resumes on its own tomorrow. | #96 |
| Safety brakes | Daily run budgets and fan-out caps mean a misbehaving workflow runs out of rope quickly instead of flooding you. | #11 |
| "Seen 3× today" dedupe | Repeat findings collapse into one card with a counter, instead of stacking up as noise. | #96 |

## Meet your team where it works

OpenNeko lives in your chat tools, knows who's talking, and routes messages to
the right workspace.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Slack & Telegram | Talk to your assistant from the tools your team already uses — replies are formatted natively for each app. | #68, #96 |
| Channels are plugins | Each chat tool is a plugin behind one modality-free interface, so new channels arrive without core changes — and install through the same CLI as everything else. | #68, #70 |
| Zero-config inbound | Install a channel and it just starts listening — inbound polling auto-enables and the first contact auto-binds to your workspace. No env flags. | #72 |
| It knows who's asking | Every inbound message is tied to the actual sender, so permissions and personalisation follow the person, not the channel. | #96 |
| Workspace routing | Messages from a connected Slack workspace or phone number land in the right organisation automatically. | #96 |
| Identity linking | Chat accounts link to OpenNeko accounts — automatically by matching email, or with an admin's approval. Unlinked strangers get safe, read-only treatment. | #96 |
| Channel administration from chat | Admins can list workspaces, see who's linked, and approve or revoke links — all conversationally. | #96 |
| Per-channel rendering | Answers are composed for the surface they'll appear on — cards on the web, native formatting in chat apps — from one channel-neutral brain. | #88 |

## Built for teams

Every run knows who started it and what they're allowed to do. Admin work —
people, plugins, channels — happens in chat with explicit approvals.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Enterprise SSO as a plugin | Install an auth plugin (Scalekit today; the contract fits Okta/Auth0/Keycloak) and "Sign in with…" lights up — no core rebuild. | #22, #24 |
| Real roles | Admins, members, and automated services each have distinct permissions, enforced everywhere — not just labels. | #96 |
| Every action has an owner | Whether something was started by a person in chat, a schedule, or a channel message, the record shows who and in what capacity. | #96 |
| Manage people from chat | Invite teammates, change roles, deactivate accounts — proposed by the assistant, approved by an admin. | #96 |
| Manage plugins from chat | Browse, install, and remove integrations conversationally, with each plugin's permissions shown on the card before you approve. | #96 |
| Personas | Tell OpenNeko what you do in your own words — free text, not a dropdown — and it tailors its briefings, language, and priorities to your role, from onboarding onwards. | #96, #120 |

## Yours and your team's knowledge

The assistant's memory, workflows, and settings are versioned like a careful
editor's drafts — personal by default, shareable on purpose, and always
restorable.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Invisible versioning | Every change to skills, workflows, and memory is snapshotted automatically — nothing is ever lost to an edit. | #96 |
| Personal workflows | Your saved workflows are yours; the team's are the team's. Same name, no collisions. | #96 |
| Personal memory layer | Correct or hide a team memory just for yourself without changing it for anyone else — and pull in team updates when you want them. | #96 |
| Save, history, restore | Browse what changed and roll back to any earlier state of your assistant's configuration. | #96 |
| Skill edits that stick | When the assistant changes a skill during a conversation, the change is saved as a new version when the turn ends. It applies to everyone if an admin asked, or only to you otherwise. | #413 |
| Optional git repository | Connect the company's skills and workflows to GitHub, GitLab or any git host over HTTPS or SSH. Publish by push or pull request, and bring in skills and packs that changed there. See [CONTEXT_REPOSITORY.md](CONTEXT_REPOSITORY.md). | #413 |
| Promote and adopt | When a personal workflow or memory proves valuable, an admin can promote it to the whole team with full lineage of where it came from. | #96 |
| Document library | Files you attach in a conversation are added to your personal library automatically — a librarian distills them into concepts the assistant can search and cite, each traced back to the original document. | #220 |
| Share and approve knowledge | A distilled concept stays private to you until you share it; an admin approves it into the team library, stamping who vouched for it and when. | #220 |
| Open Knowledge Format | The team library — and your memories, workflows, and skills — are written to disk as plain Markdown in the vendor-neutral [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) v0.2 format: readable in any editor, versioned invisibly, and portable beyond OpenNeko. | #220 |
| Take it with you | Export the team library as an OKF bundle and import it into another install — approval stamps, staleness dates, and source citations travel with it, and search vectors are rebuilt on arrival. | #220 |
| Starter knowledge packs | Install a curated pack of metric definitions and playbooks on day one instead of teaching the assistant from scratch — every concept lands pre-approved by the installing admin and stays editable. | #220 |

## Security you can verify

The assistant is treated as untrusted by design: it runs in a locked-down
sandbox, never holds your keys, and leaves a tamper-evident trail you can
hand to an auditor.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Always sandboxed | The AI agent and every plugin run inside isolation boxes with networking denied by default — this is the only mode, not an option you might forget to turn on. | #82, #96, #98 |
| Your keys never enter the box | API keys are injected on the wire by a gateway outside the sandbox; the agent only ever holds a placeholder. Verifiably — you can grep the box and find nothing. | #82 |
| Secrets encrypted at rest | Tokens, passwords, and keys are encrypted on disk everywhere they're stored. | #96 |
| Bring your own vault | Plug in an external secrets manager (Infisical) instead of local files, with no change to how anything else works. | #96 |
| Approval gates | Anything that changes the world — sending, installing, configuring, inviting — becomes a proposal a human approves first, governed by per-organisation policies. | #11, #96 |
| Dual-identity audit | Every privileged call records both the human behind it and the agent acting for them — no anonymous actions. | #96 |
| Tamper-evident log | The action trail is hash-chained: any edit or deletion after the fact is mathematically detectable. Exportable for auditors and SIEM tools. | #96 |
| Memory integrity | Stored memories are sealed against tampering and expire on schedule — a poisoned or stale note can't quietly steer the assistant. | #96 |
| Behavioral alarms | Unusual volumes — too many actions per hour, too many memory writes — raise alerts automatically. | #96 |
| Security profiles | One dial — solo, team, org, or hardened — scales approval strictness and alarm sensitivity to your deployment. | #96 |
| Audit in plain language | Ask "what did the assistant do yesterday and who approved it?" and get a readable timeline. | #96 |

## Plug in anything

Integrations are sandboxed plugins: installed in one command, hot-reloaded
without restarts, and governed by the same approval policies as everything
else.

| Feature | What it does for you | Shipped in |
|---|---|---|
| Sandboxed plugin system | Every plugin runs in its own isolation box with only the network egress its manifest declares — a misbehaving integration can't reach anything you didn't grant. | #20 |
| Hot-reload registry | Install, remove, or rotate a plugin's secrets and the running system reconciles itself — no worker restart. | #20 |
| Plugin actions as agent tools | Installed plugins surface their actions directly to the assistant, complete with example payloads so even small models call them correctly. | #24, #66 |
| Per-person integrations | Each operator connects their own accounts (OAuth) on the /integrations page — tokens are stored per-person, never shared. | #42 |
| Install policy | Admins decide who may install what; the policy is enforced at the CLI, the worker, and the web UI. | #42, #44 |
| Community plugins | Install straight from a git URL — the marketplace is a convenience, not a gate. | #42 |
| Always-current integrations | Deploys upgrade installed plugins to the latest marketplace release automatically. | #65 |

## Install in one command, operate calmly

A single binary brings the stack up, keeps it honest, and tells you the
truth when something is wrong — in language an operator can act on.

| Feature | What it does for you | Shipped in |
|---|---|---|
| The `openneko` binary | One Go binary supervises everything: start/stop/status/logs, migrations, seeding, plugins, secrets, doctor. No Node toolchain required to operate. | #26 |
| One-command sandboxed install | `openneko start` brings up the full stack — agent sandbox gateway included — with migrations applied before anything that needs them. | #82, #49, #54 |
| A demo with living data | The trial workspace ships with simulated sales trickling in and scripted scenarios firing, so briefings move and workflows have something real to find. | #47, #118, #121 |
| Honest status | `openneko status` probes what's actually serving — failures read as what they are (transient, cosmetic, or real) instead of all looking fatal. | #86 |
| Releases that gate themselves | Every release is installed and smoke-tested after it builds; a broken one is automatically demoted so installers always get the last good version. | #86, #105, #106, #107 |
| Deploys follow smoked releases | Production updates only when a release has passed its smoke — never on raw merges. Disk is garbage-collected on the way. | #116, #124, #125, #127 |
| Zero-click release cycle | Merge to main and the pipeline does the rest: version cut, binaries, images, smoke, deploy. | #16, #45, #92, #93 |
| Setup wizard you can trust | Rotating the database password in the wizard propagates everywhere — seeds, gateway, agent runs all keep working. | #121, #125 |
| Survives flaky networks | Image builds retry through registry hiccups; channel polling backs off and dedupes instead of log-flooding. | #80, #84 |
| Diagnosable agent failures | When an agent run dies, the error names the real cause — exit signal, memory state, the agent's own last log lines — and turn timeouts say "timeout", not mystery. | #129, #132 |

---

## Complete PR index

Every merged pull request and the top thing it shipped. Release-cut PRs
(`chore(main): release X.Y.Z`) are version bumps produced by the pipeline
and are omitted — each one corresponds to a release of the features listed
here.

| PR | Date | Top feature shipped |
|---|---|---|
| #2 | 2026-05-10 | The /work chat surface: per-org isolated agent runtime with threads, runs, and a durable job lifecycle |
| #3 | 2026-05-10 | Hermes backend rewritten on ACP — live streamed message and tool events |
| #4 | 2026-05-10 | Former Claude backend parity (retired); run-activity rendering and per-thread URLs |
| #5 | 2026-05-12 | Chat runs in-process: instant cancel, working file downloads, Hermes card fixes |
| #6 | 2026-05-12 | Self-healing database pool after shutdown — no more dead-pool cascades |
| #7 | 2026-05-12 | Former Claude SDK PATH fix (backend retired) |
| #8 | 2026-05-12 | Editorial design refresh; briefing cards open a "Deep dive" Ask thread |
| #9 | 2026-05-12 | Unblocked deploys: dead branch tripping the production typecheck removed |
| #10 | 2026-05-13 | Business profile and industry insights are inline-editable documents |
| #11 | 2026-05-14 | The OUDA operating loop: workflows, observations, subscriptions, action approval stack, operator UX |
| #15 | 2026-05-15 | Action receipts — auto-fired actions get a visible, auditable deep-dive page |
| #16 | 2026-05-15 | Zero-click releases: deduped PR checks + auto-merged release PRs |
| #17 | 2026-05-16 | Embedding-backed memory the agent saves and searches by meaning (pgvector) |
| #20 | 2026-05-17 | Sandboxed plugin system: per-plugin isolation, declared egress, hot-reload registry |
| #22 | 2026-05-19 | Enterprise SSO as a thin contract over the plugin runtime (Scalekit-ready) |
| #24 | 2026-05-19 | SSO page gating end-to-end + plugin actions exposed as agent tools |
| #26 | 2026-05-20 | The `openneko` Go binary: stack supervisor and plugin manager in one |
| #41 | 2026-05-21 | Hermes agent updated to v0.14.0 |
| #42 | 2026-05-21 | Per-operator OAuth (/integrations), install-policy enforcement, git-URL plugin installs |
| #44 | 2026-05-21 | Removed a broken admin gate that locked operators out of install policy |
| #45 | 2026-05-21 | Supervisor smoke moved post-release — main stays green and honest |
| #46 | 2026-05-22 | Workflows and approval rules are created by chatting, with full provenance |
| #47 | 2026-05-22 | Packaged demo gains living data: sales simulator + scenario injector |
| #49 | 2026-05-22 | One migrator: the Go binary migrates everywhere (Node migrator retired) |
| #50 | 2026-05-23 | IFTTT-style triggers: workflows fire on data-source row changes |
| #52 | 2026-05-23 | GraphJin 3.18.25: column-reference comparisons for row triggers |
| #54 | 2026-05-23 | Dedicated one-shot migrate service breaks a compose dependency deadlock |
| #63 | 2026-05-24 | Data triggers fold into workflow-save — same plain-language path on both backends |
| #65 | 2026-05-24 | Deploys upgrade installed plugins to marketplace-latest |
| #66 | 2026-05-24 | Plugin action examples survive marketplace installs (small models stay accurate) |
| #68 | 2026-05-25 | Channels V2: modality-free interaction layer + live bidirectional Telegram |
| #70 | 2026-05-25 | Channel plugins install through the CLI capability path |
| #72 | 2026-05-25 | Channels auto-enable inbound and auto-bind on first contact |
| #74 | 2026-05-25 | README redesigned as a scannable landing page |
| #75 | 2026-05-26 | INSTALL distilled to a three-command happy path |
| #78 | 2026-06-02 | Confirmation cards, new-thread Ask, workflow management + settings polish |
| #80 | 2026-06-02 | Inbound channel polling backs off and dedupes under failure |
| #82 | 2026-06-04 | OpenShell sandboxed agent runtime — model keys never enter the box — one-command install |
| #84 | 2026-06-04 | Image builds tolerate registry flakiness (embedding prewarm retries) |
| #86 | 2026-06-04 | Failures made legible to non-technical operators; post-release smoke becomes a gate |
| #88 | 2026-06-04 | Compact information-dense UI, hours-saved tracking, per-channel rendering, Ask context rail |
| #90 | 2026-06-05 | Responsive/styling consistency sweep + workflow-run reliability |
| #92 | 2026-06-05 | Release auto-merge survives apostrophes in release notes |
| #93 | 2026-06-05 | All GitHub Actions on Node 24 majors ahead of the June 2026 deadline |
| #94 | 2026-06-10 | Hours saved surfaces on every Ask thread + live dashboard sparkline |
| #96 | 2026-06-11 | The enterprise wave (45 commits): identity & RBAC, per-person data passes, sources-mode GraphJin, context versioning, Slack & WhatsApp, security hardening (encryption, audit chain, behavioral alarms, profiles) |
| #98 | 2026-06-11 | OpenShell becomes the only agent runtime — microsandbox and in-process paths removed |
| #100 | 2026-06-11 | Lockfile cleanup completing the microsandbox removal |
| #101 | 2026-06-11 | Chat-first configuration of the customer data engine: sources, roles, access — credentials value-blind |
| #103 | 2026-06-11 | Runtime-flag residue cleanup + the first edition of this document |
| #105 | 2026-06-11 | Post-release smoke tests the release it just built (was frozen on a stale tag) |
| #106 | 2026-06-11 | Smoke pre-pulls the OpenShell gateway image |
| #107 | 2026-06-11 | Smoke pre-pull list derived from the compose files themselves — no more hardcodes |
| #108 | 2026-06-11 | Seven live-sweep fixes: plugin-base default, sandbox corpse cleanup, egress, provisioning, Hermes ACP |
| #110 | 2026-06-11 | Delete a workflow from chat by @mentioning it, with composer autocomplete |
| #112 | 2026-06-11 | Hermes gains the full neko MCP toolset via stdio bridges — chat-first admin on every backend |
| #114 | 2026-06-12 | Agent-runtime resilience batch: bridge proxy env, broker warm-up, doc links |
| #116 | 2026-06-12 | Production runs the released compose stack — source-built deploy path retired |
| #118 | 2026-06-12 | Fresh demo boots clean in sources mode (env conflict + keystore generation fixed) |
| #120 | 2026-06-12 | Free-text personas in onboarding (solo + multi-user) and a two-card briefing grid |
| #121 | 2026-06-12 | Demo seed survives the wizard's password rotation |
| #124 | 2026-06-12 | Deploys garbage-collect superseded images — no more disk-full failures |
| #125 | 2026-06-12 | Agent runs survive the setup wizard: the sandbox gateway tracks the rotated DB password |
| #127 | 2026-06-12 | Deploys follow smoked releases, not merges |
| #129 | 2026-06-12 | Thread-scale answer surfaces, sidebar timestamps, diagnosable agent exits |
| #132 | 2026-06-12 | Agentic answers reach hand-tuned speed (join-path hubs, query pattern cards, compact digests everywhere) and the "agent died mid-run" mystery is fixed — turn timeouts are generous, configurable, and say so |
| #220 | 2026-08-18 | Document library: uploads distilled into an OKF knowledge base the assistant searches and cites |
| #449 | 2026-10-12 | Chat-first web app: personal bots with neko avatars, routines in chat, skills found by meaning, priced Ax runs |
