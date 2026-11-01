# Agent startup

The web and worker images carry the agent runner at `/opt/openneko-agent`: two
JavaScript bundles (`entry.js` and `mcp-bridge.js`) and the built-in skills. It
does not include the worker's Node dependency tree, tsx, database libraries or
embedding stack. The agent image runs `openneko serve`. See
[AGENT_RUNTIME.md](../../AGENT_RUNTIME.md) for how the two fit together.

Each run stages its workspace in a fresh folder under `OPENNEKO_AGENT_RUNS_DIR`
and starts `node /opt/openneko-agent/entry.js` as a child process in its own
process group. The entry validates its bridge and skills before running the
job; the worker image build runs the same check (`entry.js --preflight` and
an import of `mcp-bridge.js`) as a non-root user on both release
architectures. The runner sends each turn to `openneko serve`. When the run
ends, the launcher kills
the runner's process group, copies artifacts back and deletes the folder.

Measured image sizes, restart/readiness timings, single-agent and three-session
results, and lazy document-service memory are recorded in the
[12 September 2026 demo benchmark](benchmarks/2026-09-12-agent-startup.md).
That benchmark and the later latency, preload and warm-pool experiments in
[benchmarks](benchmarks/) measured earlier runtimes; the per-run sandbox
containers and warm pool they evaluated were removed.

## Resource limits

Runs share the openneko-serve container's resources; there are no per-run
CPU or memory limits. Size the host for the maximum number of concurrent
turns and their delegated children.

## Timings

Launcher logs contain JSON records with `type: "sandbox_phase"`, `runId`,
`phase`, and `durationMs`. Individual operations also include `ok`:

- `stage`: copy the selected workspace and skill overrides into the run folder.
- `exec`: run the runner and drain streamed event handlers. This includes
  runner startup, model time, tools, and the artifact scan.
- `download`: copy artifacts back when present or their state is unknown.
- `skill_capture`: save skill edits the agent made, or record why it skipped.
- `total`: the full launcher call, including staging and cleanup.

Do not interpret `exec` or total lifecycle duration as time to first answer.

## Artifact recovery

After the agent finishes, including an ordinary agent error, the entry reports
whether the artifact tree contains files or links. Only an explicit empty-tree
report skips copying back. Crashes, timeouts, or unreadable trees keep the
recovery attempt. Background jobs and aborted runs retain their existing
no-copy behavior.

## Verification

```sh
pnpm --filter @neko/worker exec vitest run test/agent-sandbox
pnpm --filter @neko/llm exec vitest run test/sandbox-launcher.test.ts test/workflow-api-contract.test.ts
docker build --target worker -t openneko-worker:startup .
```

The bundle test builds both executables into an isolated temporary directory,
rejects host DB/telemetry and embedding imports, and checks boot with
only the bundled assets and no inherited runtime overrides. Missing assets
must fail. Launcher tests cover cancellation, partial output, empty output,
and cleanup.

Embedding computation runs in the shared embedding service. It uses the
[lazy processing listener](../lazy-service/README.md), which releases model
memory after idle time. OpenNeko calls it only while semantic search is on.

Every run starts a fresh runner in a fresh folder; there is no warm pool and no
per-user reuse.

## Knowledge cache

JWT datasource knowledge is stored as one atomic snapshot in the shared agent
home. The worker checks GraphJin's active catalog revision every 60 seconds and
rebuilds only when it changes. Chat turns use the cached snapshot without a
GraphJin request; a missing snapshot is built once on demand. Revision metadata
is read as admin, while catalog content remains service-scoped. Source identity
and pack-format changes invalidate the snapshot. A recompile during fetching
discards the incomplete revision and retries; a refresh failure retains the last
complete snapshot for that same source. Run staging materializes only the
snapshot's six knowledge files, never cache metadata or temporary builds.

An isolated diagnostic against `neko-vm` on 12 September measured a 13,515 ms
cold build (41 HTTP calls), a 20 ms cached read (zero HTTP calls), and a 22 ms
unchanged-revision check (one HTTP call). This used the patched cache in a
temporary directory; it did not deploy the patch or change the live cache.

Multi-user RBAC work is tracked as [RBAC-1 in the roadmap](../../ROADMAP.md#rbac-1-enforce-effective-access-on-every-agent-request).

## Solo admin identity

Migration 0073 adds `organization.solo_admin_user_id`. On the first solo request,
the existing unambiguous active local admin is adopted; otherwise a local admin
row is created automatically under the same per-org lock used by SSO sign-ins.
Its internal `.invalid` address is not a mailbox and is hidden from the UI.
The saved owner ID remains stable when more users are added. No setup redirect,
email entry, or manual database update is required to keep using the installation.
Explicitly disabling the recorded owner is still honored.

The Users form can save a real email on that same owner row, preserving work and
personal connection ownership. An auth provider remains pending while this local
owner has no real email, preventing activation from locking out the admin. Once
saved, the existing case-insensitive SSO email match attaches the IdP subject to
the same ID. Already-active SSO installations do not bootstrap a solo identity.
During first ownership assignment, existing null-owned web Ask threads are moved
to the owner atomically so history stays visible. Channel/workflow threads and
threads with an existing owner are excluded; historical run/audit actors remain
unchanged.

Validation covers concurrent upgrades, adoption, additional users, disabled
owners, email completion without changing ID, and SSO readiness. The UI reuses
Users' Field/Input/NativeSelect/Button and existing navigation. Solo mode exposes
its identity separately from sign-in state, so it does not show a sign-out action.

Upgrade UI checked at 1280px and 390px: optional email form, persisted account
visible before email entry, 44px phone targets, visible focus, no page overflow,
and no solo sign-out action. Saving email kept the same user ID and one user row.
SSO readiness and identity linking are covered by server tests; full external IdP
login was not exercised in this test stack.

### Startup timing coverage

Web Ask executes in the web process. Channel jobs execute in the worker queue;
only the latter report `queue.wait` from the enqueue timestamp (including elapsed retry time on retried jobs). Old queued jobs
without that timestamp have unavailable queue timing, not a fabricated zero.

Both paths write JSON `startup_timing` records with request/run/thread IDs,
version, commit, process ID, timestamp, monotonic elapsed time and phase duration.
`run.link` connects HTTP phases before a run exists to the eventual run ID.
Release web/worker images embed the checked-out commit and release version;
unversioned local images honestly report `unknown`.

| Area | Captured phases / decisions |
| --- | --- |
| HTTP | submit, organization, identity, authorization, backend/provisioning, run creation, user-message persistence, acknowledgement ready |
| Worker | queue wait, run lookup, provisioning, pack actions, broker readiness |
| Shared turn | mark running, load thread, workspace, knowledge prefetch/read, identity, GraphJin config, compaction, memory, skills, persona, action catalogs |
| Knowledge | snapshot read/age, hit/miss/coalesced wait, revision checks, individual catalog requests, rebuild, atomic publish, refresh failure and stale availability; 60-second background refresh |
| Agent run | staging, runner execution, artifact copy-back, skill capture |
| Streaming | SSE subscription, LISTEN setup, initial DB read, hello, first event and first assistant output enqueued |
| Browser | submit through acknowledgement (including new-thread/upload preparation), stream open, first event, first assistant output and subsequent paint opportunity; terminal run without output |

Timed phases pair start/end observations on exceptions too. With a run observer,
they feed the existing OTel exporter and persisted summary `phases` (bounded at
128 entries). Decision events and browser reports are structured logs. OTel
export still requires the existing collector configuration; logs do not.

Browser durations use only the browser's monotonic clock and are explicitly
`client_reported`. The reporting endpoint authorizes the thread/run and permits
only fixed numeric fields. A double animation frame is a **paint opportunity**,
not proof of visible pixels; hidden tabs can defer it. Resumed runs do not invent
a submission timestamp. Browser metrics are best effort if the tab closes.
SSE timings with nonzero `afterId` describe replay/reconnection, not original
startup. Do not add nested or parallel phase durations together.

The existing outer model observation measures the agent loop; its first-output
marker is explicitly tagged as agent output, **not provider TTFT**. Run-level
first output includes the HTTP/worker prelude when a startup trace is present. Provider-internal scheduling/token timings are
not exposed by the agent event contract. Tool/delegation spans and usage continue
through the existing agent-event observer. Prompts, SQL, catalog content, tokens
and raw configuration are excluded from startup metrics; revisions are hashed.

To compare turns, filter container JSON logs by `type == "startup_timing"`, then
by `runId` (or `requestId` for a rejected/pre-run request). Compare the same
version/commit and execution path, grouping runner outcomes and cache outcomes
before calculating median/p95. Use `elapsedMs` for milestone timing and
`durationMs` for individual phases.

### Workflows, metrics and workflow API calls

The same timing helper also covers:

- Manual workflow HTTP setup; scheduled, subscription, watcher and API workflow
  claims/preparation; backend/workspace/memory/knowledge setup; runner phases;
  compiled batch execution and result persistence. Preflight spans attach to
  the workflow root, not a fabricated chat run.
- Metric job loading, DB reads, provisioning, knowledge and isolated workspace
  setup, agent execution or deterministic saved-query HTTP execution, progress
  writes and snapshot/status persistence.
- Public workflow API throttling/body parsing, credential verification, batch
  input staging, the admission transaction (including idempotency/quota checks),
  replay outcome, admission-to-dispatch wait, enqueue, worker execution, status
  polling, artifact authorization/setup and file-stream completion/cancellation.
  Stream completion means bytes handed off by the server, not client receipt.
- Other jobs using the shared processing-job handler get a dispatch envelope and
  running/succeeded/failed persistence phases. Their internal steps retain their
  existing instrumentation; this does not claim every internal operation is timed.

Workflow API HTTP request IDs link to canonical `workflowRunId`; worker records
link it to the underlying `runId` and thread. Metric records carry `jobId`.
Queue stamps are added at enqueue for work, workflow and metric executions.
API workflow queue time includes admission/dispatch delay; scheduled executions
use enqueue time. Retries include elapsed time since enqueue. Old payloads with
no timestamp have unavailable queue timing. Phase logs include setup before an
observer exists and cleanup after a persisted run summary; use dispatch/refresh
logs for the complete handler envelope, not the sum of nested spans. Workflow
credentials, idempotency keys, request input, query text and result data are not
included in the timing records.
