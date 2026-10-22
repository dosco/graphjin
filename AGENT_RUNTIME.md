# Agent runtime

OpenNeko treats the agent loop as untrusted, prompt-injectable code. Three
pieces run it, and each holds as little as its job needs:

- the **runner**, a short-lived Node process web or the worker starts for one
  run;
- **`openneko serve`**, which runs each agent turn (the Ax harness, the model
  calls and the agent's file tools);
- **`openneko sandbox`**, which runs the agent's terminal commands.

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
            ▲                                          │ terminal commands
            │ MCP tools over the serve socket          ▼
            └──────── broker (loopback) ◀──       openneko sandbox
                                                  (agent-sandbox container,
                                                   per-run user, no secrets)
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
4. Serve sends terminal commands to `openneko sandbox` over
   `OPENNEKO_SANDBOX_SOCKET`. The sandbox runs each command in the run's
   workspace, as a user of the turn's own. When the turn ends, serve tells the
   sandbox, which kills everything that user still has running and removes
   its home.
5. When the run ends, the launcher kills the runner's process group, copies
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
| `openneko sandbox` (`agent-sandbox`) | root, to give each turn's commands their own user | **no secrets**; commands get only `PATH`, `HOME`, `TMPDIR`, `LANG` and `SKILL_DIR` | only the `agent-sandbox` network |

Model keys exist only in the runner and in serve, and in serve only for the
turn that uses them. Serve drops model-key variables from the environment it
passes on, so neither the agent's tools nor its terminal can read them.

## The sandbox

`openneko sandbox` gives each turn's terminal a user ID from
`OPENNEKO_SANDBOX_BASE_UID` (default 20000) upward, with a group of the same
number. Turn users are not in the shared group `OPENNEKO_SANDBOX_GID` (1001). It runs a command only when the workspace lies
under `OPENNEKO_SANDBOX_WORKSPACES`, gives the user a home under
`OPENNEKO_SANDBOX_HOMES`, and passes the command no other environment. Two
turns running at once never share a user, and ending a turn kills every
process its user started.

In Compose the sandbox container:

- sits **only** on the `agent-sandbox` network. Terminal commands can reach the
  internet, but not `neko-db`, GraphJin, the broker, or any other
  service of the stack;
- drops every capability except `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `KILL`,
  `SETGID` and `SETUID`, which it needs to create run homes, switch to run
  users and end their processes, and sets `no-new-privileges`;
- mounts only the sockets folder, the runs folder and the sandbox homes. No
  configuration, secret or Docker socket is mounted.

The agent image carries the document toolchain (Python, LibreOffice, Poppler,
qpdf, Tesseract, the Node document libraries) that bundled skills run from the
terminal.

## The shared runs folder and per-run groups

The runner and serve work on a run's folder through the shared group 1001;
the run's terminal user works on it through its own group:

- `openneko-agent-runs` → `/var/lib/openneko/agent-runs`, owned by
  `1001:1001`. The sandbox makes it crossable but not listable by others, so
  a turn user can reach the folder it is given but cannot see the others.
- On a turn's first command the sandbox sets POSIX ACLs on that run's
  folder: rwx for group 1001 and for the turn's own group, nothing for
  others, and the same as the default for files created later. A turn's
  terminal therefore reaches only its own run's folder, and what it writes
  stays readable by web and the worker, which copy artifacts back.
- When the turn ends the sandbox takes the turn's group off the folder
  again, so a later turn that is given the same ID cannot reach it.
- `openneko-run` → `/run/openneko`, `1001:1001`, mode `0770`, holds
  `serve.sock` and `sandbox.sock`. The sandbox hands its socket to group 1001
  (mode `0660`) so serve can connect.
- `openneko-sandbox-homes` → `/var/lib/openneko/sandbox-homes`, root, `0711`,
  mounted only in the sandbox; each turn's home belongs to its user.

The web, worker and agent images create user and group `neko` (1001) and
pre-create these folders, so fresh named volumes start with this ownership.
The runs folder's filesystem must support POSIX ACLs (ext4, xfs and Docker's
default volumes do); the sandbox refuses a turn rather than share a folder
it cannot restrict.

## Configuration

Compose sets all of these; they matter only for custom deployments.

| Variable | Where | Meaning |
|---|---|---|
| `OPENNEKO_SERVE_SOCKET` | web, worker | serve's socket (`/run/openneko/serve.sock`) |
| `OPENNEKO_AGENT_RUNS_DIR` | web, worker | where runs are staged |
| `OPENNEKO_AGENT_ENTRY` | web, worker | the runner (`/opt/openneko-agent/entry.js`; a `.ts` path runs with tsx) |
| `OPENNEKO_SERVE_GRAPHJIN` | web, worker | `1` when serve hosts the customer GraphJin, so runs ask its agent in-process |
| `OPENNEKO_SANDBOX_SOCKET` | serve | the sandbox's socket |
| `OPENNEKO_SANDBOX_WORKSPACES` | sandbox | the folder every run workspace must lie under |
| `OPENNEKO_SANDBOX_HOMES` | sandbox | per-run homes |
| `OPENNEKO_SANDBOX_GID` | sandbox | the shared group (1001) |
| `OPENNEKO_SANDBOX_BASE_UID` | sandbox | first run user ID (default 20000) |

Web and the worker check that serve answers on its socket before setup
completes and before onboarding work is queued; when it does not, they report
that the agent runtime (openneko serve) is unavailable.

## Development

`pnpm dev` runs `openneko serve` and `openneko sandbox` on the host next to
web and the worker (`scripts/dev-agent.sh`, built by `pnpm dev:up`). Not being
root, the development sandbox runs commands as you rather than as per-turn
users, and its commands share your network.
