# Agent startup

The agent image ships two JavaScript bundles and built-in skills. It does not
ship the worker's Node dependency tree, tsx, database libraries, or embedding
stack. Hermes and the document tools remain part of the agent image.

Each run creates a fresh OpenShell sandbox with its own provider and egress
policy. Creation runs `true`; execution starts `/app/entry.js` once. The entry
validates its bridge and skills before running the job. The same filesystem
checks run during the image build on both release architectures.

Measured image sizes, restart/readiness timings, single-agent and three-session
results, and lazy document-service memory are recorded in the
[12 September 2026 demo benchmark](benchmarks/2026-09-12-agent-startup.md).
The follow-up [latency and warmed-session profile](benchmarks/2026-09-12-latency-profile.md)
separates Hermes/ACP initialization, MCP setup, provider chunks, client delivery,
and the memory cost of an initialized idle sandbox.
The [request-independent preload experiment](benchmarks/2026-09-12-generic-prewarm.md)
measures common Hermes/tool code before any model configuration or MCP attachment.

## Resource limits

`OPENNEKO_AGENT_CPUS` defaults to `2`; `OPENNEKO_AGENT_MEMORY` defaults to `1Gi`.
The launcher passes these as OpenShell `--cpu` and `--memory` creation flags.
The limits cover the sandbox's process tree, including Hermes and tool children.
The Compose overlays forward both variables to web and worker. Restart those
hosts after changing them; existing sandboxes retain their creation limits.

Ten sandboxes can consume at most 10 GiB at this default, leaving roughly 6 GiB
on a 16 GiB host for the OS and services. This is a capacity budget, not proof
that those services fit in the remainder.

A local Linux ARM64 startup probe loaded the bundled entry, initialized a
Hermes ACP session, and connected the real MCP bridge under a 1 GiB cap. Its
cgroup peak was about 395 MiB (including charged file cache). It made no model
calls and did not exercise document tools or delegation. Ten concurrently
launched copies also initialized successfully, with per-container cgroup peaks
of 233–266 MiB. Shared file-cache charging affects that comparison; it is not
a benchmark of ten active model/document workloads.

These are configurable starting limits, not measured workload peak requirements.
Size them for the document/delegation workloads and maximum concurrent turns
on the host. A memory cap is not a reservation or an aggregate admission limit.

## Timings

Launcher logs contain JSON records with `type: "sandbox_phase"`, `runId`,
`phase`, and `durationMs`. Individual operations also include `ok`:

- `stage`: copy the selected workspace and skill overrides.
- `create_upload`: OpenShell creation, image availability, policy setup,
  upload, and the initial command. Each retry has its own record.
- `exec`: execute the agent and drain streamed event handlers. This includes
  Hermes initialization, model time, tools, and the artifact scan.
- `download`: recover artifacts when present or their state is unknown.
- `delete`: destroy the sandbox and its remaining process tree.
- `total`: the full launcher call, including staging, retries, and cleanup.

Creation and upload share one CLI call, so the host does not claim separate
gateway/provision/upload measurements. Compare these logs with
`scripts/bench-hermes-acp.mjs` when investigating Hermes initialization.
Do not interpret `exec` or total lifecycle duration as time to first answer.

## Artifact recovery

After the agent finishes, including an ordinary agent error, the entry reports
whether the artifact tree contains files or links. Only an explicit empty-tree
report skips downloading. Old images, crashes, timeouts, or unreadable trees
keep the existing recovery attempt. Background jobs and aborted runs retain
their existing no-download behavior.

## Verification

```sh
pnpm --filter @neko/worker exec vitest run test/agent-sandbox
pnpm --filter @neko/llm exec vitest run test/sandbox-launcher.test.ts test/hermes-install-contract.test.ts test/workflow-api-contract.test.ts
docker build --target agent -t openneko-agent:startup .
```

The bundle test builds both executables into an isolated temporary directory,
rejects host DB/records/telemetry and embedding imports, and checks boot with
only the bundled assets and no inherited runtime overrides. Missing assets
must fail. Launcher tests cover resource limits, cancellation, partial output,
empty output, and cleanup. Real gateway/model acceptance remains the opt-in
`packages/llm/test/sandbox-launcher-e2e.test.ts` suite.

Embedding computation now runs in the shared embedding service. Both it and
Docling use the [lazy processing listener](../lazy-service/README.md), releasing
model memory after idle time. Incremental upload caches remain out of scope:
fresh boxes have no upload baseline and unchanged built-in skills are already omitted.

The [user-scoped warm pool experiment](benchmarks/2026-09-12-user-warm-pool.md)
implements generic prewarming enabled by default (one slot), user reuse, and a
three-minute idle timeout. Set `OPENNEKO_AGENT_WARM_POOL_SIZE=0` to disable it.
The pool serves Hermes only. An organisation on the Ax backend starts each run in
a fresh sandbox (about 2.5 s with the current supervisor), because the warm slot's
idle timer ends long Ax runs after three minutes.

Solo admin runs reuse their assigned sandbox by default. Userless runs use the
recorded solo owner's persisted ID. Web requests automatically adopt or create
that account, with no setup redirect or required email. The three-minute idle timeout starts after completion; active turns do
not consume it. Expired generic spares replenish automatically while the host is
alive. Changed model/provider or access configuration still invalidates reuse.

## Knowledge cache

JWT datasource knowledge is stored as one atomic snapshot in the shared agent
home. The worker checks GraphJin's active catalog revision every 60 seconds and
rebuilds only when it changes. Chat turns use the cached snapshot without a
GraphJin request; a missing snapshot is built once on demand. Revision metadata
is read as admin, while catalog content remains service-scoped. Source identity
and pack-format changes invalidate the snapshot. A recompile during fetching
discards the incomplete revision and retries; a refresh failure retains the last
complete snapshot for that same source. Sandbox staging materializes only the
snapshot's six knowledge files, never cache metadata or temporary builds.

An isolated diagnostic against `neko-vm` on 12 September measured a 13,515 ms
cold build (41 HTTP calls), a 20 ms cached read (zero HTTP calls), and a 22 ms
unchanged-revision check (one HTTP call). This used the patched cache in a
temporary directory; it did not deploy the patch or change the live cache.

Multi-user RBAC work must complete [RBAC-1 in the roadmap](../../ROADMAP.md#rbac-1-connect-effective-access-revisions-to-warm-sandbox-reuse)
before enabling assigned reuse for actors with fine-grained grants. It documents
the opaque revision string, the separate SHA-256 fingerprint, invalidation rules
and acceptance checks.

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
login and the unrelated Records service were not exercised in this test stack.

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
| Shared turn | mark running, load thread, workspace, knowledge prefetch/read, identity and authorization revision, GraphJin config, compaction, memory, skills, persona, fallback plugin catalog |
| Knowledge | snapshot read/age, hit/miss/coalesced wait, revision checks, individual catalog requests, rebuild, atomic publish, refresh failure and stale availability; 60-second background refresh |
| Sandbox | acquire, staging, cold creation, configuration binding, provider/policy attachment, execution, download, deletion; assigned/generic hits, cold misses, model/auth/config changes, dead slots, concurrent users, idle expiry and spare replenishment |
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
not exposed by the ACP event contract. Tool/delegation spans and usage continue
through the existing agent-event observer. Prompts, SQL, catalog content, tokens
and raw configuration are excluded from startup metrics; revisions are hashed.

To compare turns, filter container JSON logs by `type == "startup_timing"`, then
by `runId` (or `requestId` for a rejected/pre-run request). Compare the same
version/commit and execution path, grouping sandbox outcomes and cache outcomes
before calculating median/p95. Use `elapsedMs` for milestone timing and
`durationMs` for individual phases. Join background eviction/replenishment logs
by pool/slot; their originating run ID does not mean they blocked that run.

### Workflows, metrics and workflow API calls

The same timing helper also covers:

- Manual workflow HTTP setup; scheduled, subscription, watcher and API workflow
  claims/preparation; backend/workspace/memory/knowledge setup; sandbox phases;
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
