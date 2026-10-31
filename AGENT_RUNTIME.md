# Agent runtime

OpenNeko treats the agent loop as untrusted, prompt-injectable code. Two
pieces run it, and each holds as little as its job needs:

- the **runner**, a short-lived Node process web or the worker starts for one
  run;
- **`openneko serve`**, which runs each agent turn (the Ax harness, the model
  calls and the agent's file tools).

The agent runs no code of its own. It has no terminal, and OpenNeko runs no
script from a skill or a workflow. Its tools are its file tools, the
OpenNeko tools the broker serves, GraphJin and, when a workflow allows hosts,
`web_fetch`.

Web and the worker stay the only trusted control plane. The database,
secrets, policy and approvals live there, and the agent reaches them only
through the per-run broker.

## How a run flows

```
 user ─▶ web / worker  (control plane: DB, secrets, policy, broker)
            │ stage the run's workspace in the shared runs folder
            │ start the runner: node /opt/openneko-agent/entry.js
            ▼
        runner  ── one turn per connection ──▶  openneko serve
        (child process,                          (openneko-serve container; it also
         broker token, model key                  hosts the customer GraphJin,
         for this run)                            model key only for the turn)
            ▲                                          │
            │ MCP tools over the serve socket          │
            └──────── broker (loopback) ◀──────────────┘
```

1. Web (chat) or the worker (channels, workflows, jobs) stages the run's
   workspace, skills and job file in a fresh folder under
   `OPENNEKO_AGENT_RUNS_DIR` (`/var/lib/openneko/agent-runs` in the images).
2. It starts the runner, `node $OPENNEKO_AGENT_ENTRY` (default
   `/opt/openneko-agent/entry.js`), as a child process in its own process
   group. The runner receives the broker URL and a token bound to
   `{run, org, thread}`, the serve socket, the proxy settings and the org's
   model key, which is loaded fresh for each run.
3. The runner sends each turn to `openneko serve` over the Unix socket
   `OPENNEKO_SERVE_SOCKET`. The turn carries the model key; serve uses it for
   that turn's model calls and keeps it nowhere else. The agent's MCP tools
   (records, GraphJin, memory, actions) travel back over the same socket to
   the runner, which calls the broker on loopback.
   By default the turn gets its data by asking GraphJin's agent, in serve's
   own process: the host mints the run's GraphJin JWT into the job, and
   GraphJin verifies it exactly as its `/api/v1/agent` route would. Its steps
   show as status lines, the thread's earlier data questions go along as
   history, and its tokens count against the turn's budget. An admin can
   switch the organization to GraphJin's direct tools instead (Admin →
   Settings → Agent), for an external GraphJin whose agent isn't set up;
   the agent then writes its own queries.
4. When the run ends, the launcher kills the runner's process group, copies
   artifacts and skill edits back, and deletes the run folder. Folders a
   restart left behind are swept at the next start.

The host's own model calls, the ones that are not agent turns (summaries,
classification, transcript compaction, research, the provider test), also go
through serve when `OPENNEKO_SERVE_SOCKET` is set: `buildLlm` gives Ax a fetch
that sends each request to serve as a `fetch` frame, and serve makes the call
and streams the response back (`apps/ax-harness/serve/gateway.go`). Serve is
then the one process that reaches model providers. Without the socket, as in
local development, those calls go out directly.

`runChatTurn` keeps its split: the **prologue** (build the prompt) and
**epilogue** (parse action/workflow fences, persist, scrub) run in the trusted
host; only the agent loop runs in the runner.

## What each piece holds

| Piece | Runs as | Holds | Network |
|---|---|---|---|
| Runner | web/worker user (`neko`, 1001) | the run's broker token and the org's model key, for this run only | web/worker's |
| `openneko serve --path /config` (`openneko-serve`) | `1001:999`, plus group 1001 | a turn's model key, only while the turn runs; GraphJin's own config and data source credentials, as the GraphJin service always held; the password of `neko_subscriptions`, a role that may only read `workflow_output`, for the subscriptions GraphJin | the stack's default network, for model APIs and data sources |

Model keys exist only in the runner and in serve, and in serve only for the
turn that uses them. Serve drops model-key variables from the environment it
passes on, so the agent's tools cannot read them.

## The shared runs folder

The runner and serve work on a run's folder through the shared group 1001:

- `openneko-agent-runs` → `/var/lib/openneko/agent-runs`, owned by
  `1001:1001`, mode `2770`. Web, the worker and serve reach it; nobody else
  does.
- `openneko-run` → `/run/openneko`, `1001:1001`, mode `0770`, holds
  `serve.sock`.

The web, worker and agent images create user and group `neko` (1001) and
pre-create these folders, so fresh named volumes start with this ownership.

## Configuration

Compose sets all of these; they matter only for custom deployments.

| Variable | Where | Meaning |
|---|---|---|
| `OPENNEKO_SERVE_SOCKET` | web, worker | serve's socket (`/run/openneko/serve.sock`) |
| `OPENNEKO_AGENT_RUNS_DIR` | web, worker | where runs are staged |
| `OPENNEKO_AGENT_ENTRY` | web, worker | the runner (`/opt/openneko-agent/entry.js`; a `.ts` path runs with tsx) |
| `OPENNEKO_SERVE_GRAPHJIN` | web, worker | `1` when serve hosts the customer GraphJin, so runs ask its agent in-process |

Web and the worker check that serve answers on its socket before setup
completes and before onboarding work is queued; when it does not, they report
that the agent runtime (openneko serve) is unavailable.

## Development

`pnpm dev` runs `openneko serve` on the host next to web and the worker
(`scripts/dev-agent.sh`, built by `pnpm dev:up`).
