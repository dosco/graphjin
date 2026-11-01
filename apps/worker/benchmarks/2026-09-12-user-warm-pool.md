# User-scoped warm sandbox experiment

Measured on 12 September 2026 on the same ARM64 MacBook Air and Docker setup as
[the baseline](2026-09-12-agent-startup.md). Measurements were taken from the working tree based on `21f0ac5`.
Generic prewarming is **on by default with one slot**. The normal demo web process and its agent tag
were not changed. An isolated harness in the existing demo worker exercised the
real launcher, `runChatTurn`, broker, Hermes and Gemini 3.5 Flash Lite.

## Behavior

- Keep a bounded generic pool (one slot in these tests). Generic slots have no
  provider attached, no egress, and no user configuration or workspace.
- Preload common Hermes/tool modules into a clean Python parent. Fork a fresh
  child for each turn. The parent never reads the turn's environment or prompt.
- At assignment, attach the provider, wait for the new policy, replace the staged
  workspace/configuration, and issue a fresh per-run broker token. Real provider
  keys remain in the OpenShell proxy.
- Retain one healthy assigned sandbox per **organisation and user**, across chat
  threads, for **180 seconds of idle time**. Assigned sandboxes never re-enter the
  generic queue. Concurrent requests for the same user get separate disposable
  sandboxes instead of sharing one running process.
- A missing authorization revision disables assigned reuse. A changed revision
  or incompatible model/tool/access configuration destroys the prior slot.
  Failed or cancelled runs are destroyed. Broker tokens are released per run.
- Each turn still uses `session/new`; OpenNeko already supplies conversation
  history in its prompt. This does not retain an ACP conversation between chats.

The generic pool now replenishes automatically after idle expiry while the host is alive.
It is local to each host process. Web and worker processes therefore have separate
pools, and generic misses can still incur preparation work. Assigned idle slots
are not evicted early to make room: memory planning must include the number of
users active in the preceding three minutes. This is not a server-wide admission
queue or a ten-user capacity certification.

## RBAC boundary

`RunChatTurnDeps.sandboxAuthorizationRevision` is a trusted server-side hook. The
principal comes from the stored run actor, never the browser or agent. The future
authorization service must return a revision covering every effective source,
pack/plugin, memory and library grant. Returning null disables assigned reuse.
No production caller supplies this hook yet. Solo deployments now enable reuse
for their recorded solo owner without it, using the persisted app-user ID. Web
requests automatically adopt or create this owner during upgrade. Multi-user deployments still require the hook.

The live demo has an admin actor with no user ID. The benchmark supplies explicit
synthetic cache principals and revisions to test reuse/invalidation while running
real queries. These are **not proof of future granular RBAC enforcement**. That
work must still filter staged files and context, authorize tool execution and
retrieval server-side, and invalidate cached access when grants change. Existing
org-wide knowledge/library staging is not a substitute for those future filters.

## Findings during implementation

The first live warm attempts failed because provider attachment was followed by
an immediate exec before OpenShell had refreshed the placeholder environment.
Attaching the provider **before** the policy-readiness wait fixed this; no key
copying or invented placeholder was needed.

OpenShell 0.0.54 polls policy/provider state every ten seconds by default. This
added 8.5–9.3 seconds to warm assignment, making generic pooling counterproductive.
The agent image now sets `OPENSHELL_POLICY_POLL_INTERVAL_SECS=1`, an existing
[supervisor setting](https://github.com/NVIDIA/OpenShell/blob/v0.0.54/crates/openshell-sandbox/src/lib.rs#L987).
The final policy waits were about 1.0–1.2 seconds. This increases gateway polling
traffic; the tradeoff needs checking at larger server scale.

A checkout handshake renews the clean parent's idle lease before policy sync and
upload. The parent exits when idle, and the attached `--no-keep` creation command
plus host timer delete the sandbox. The live test checks normal idle cleanup;
full host/gateway crash recovery was not tested. A crashed control-plane container
may still require reconciliation of orphaned gateway sandboxes.

## Final live timings

Every query requested a real read-only AdventureWorks sales-order count and
returned the correct **31,465**. First output below means the first visible agent
output event delivered to the host callback, **not the provider's first token or
browser SSE delivery**. The timing starts at `runChatTurn`, after thread/run
creation and user-message persistence.

| Scenario | First visible output | Completed |
| --- | ---: | ---: |
| Cold, new sandbox | 21.066 s | 21.701 s |
| Generic warm slot | 17.982 s | 18.123 s |
| Same user, different chat | 14.598 s | 14.721 s |
| Changed access revision, new slot | 16.954 s | 17.095 s |
| Concurrent user 1, generic hit | 34.339 s | 35.376 s |
| Concurrent user 2, pool miss | 24.141 s | 24.661 s |
| Concurrent user 3, pool miss | 23.981 s | 24.655 s |

The sequential sample saved about 3.1 seconds to visible output for a generic hit
and 6.5 seconds for user reuse. These are small-sample observations, not latency
percentiles or guaranteed savings. An earlier one-second-poll comparison measured
17.0 / 14.5 / 15.0 seconds for cold / generic / assigned output, demonstrating
provider and workload variation. Three-way concurrency was not uniformly faster
than the earlier 30–31 second baseline. Two concurrent requests missed the
one-slot generic pool, and model/tool work dominated the slowest request.

## Resource and validation results

Final image size: **1,352,364,257 bytes**, compared with **1,352,359,404 bytes** for
the previous agent: about **5 KiB** extra uncompressed image data. No dependency
was added. Runtime limits stayed **768 MiB and two CPUs per sandbox**.

The accompanying raw report contains Docker working-set samples (not process RSS,
cgroup current, host RAM, or configured limits). Peak combined usage across demo
services and benchmark sandboxes was **2,607.4 MiB / 2.55 GiB**. Warm sandboxes alone
peaked at **1,429.7 MiB** combined. After the live turns, five idle warm sandboxes
used approximately **77–97 MiB each** in one sample. This is a clean fork parent,
so it is smaller than the earlier fully initialized ACP waiting process.

Validation includes 80 focused launcher/backend/pool tests, worker TypeScript
checking, the full Docker agent build/preflight, an offline real-Hermes two-child
configuration/idle-exit smoke test, and seven successful real-model queries.
The launcher test rejects nonzero policy commands even if they printed stdout,
and tests prove different chats reuse the same user scope while changed revisions
do not. The offline smoke uses a shorter timeout solely to exercise expiry quickly.
The final live harness waited the full three minutes. A warm sandbox was still
observed 179.5 seconds after the last query;
all were gone by 182.7 seconds, before explicit harness shutdown.
The final combined sample after expiry was 1023.2 MiB.
Shutdown initially reported an already-deleted generic slot as an error. The final
code makes that deletion idempotent; a regression test and separate real-gateway
expiry check validate the fix. The final image also replaces a polling child-wait
loop with blocking waitpid; its offline real-Hermes smoke was repeated.

[Raw timings and memory samples](2026-09-12-user-warm-pool.json).

## Reproduction

Build the image from this working tree:

```sh
docker build --target agent -t ghcr.io/open-neko/agent:warm-uncommitted .
docker run --rm --network none --memory 768m --cpus 2 \
  --user 1000660000:1000660000 \
  -v "$PWD/scripts/bench/generic-warm:/bench:ro" \
  ghcr.io/open-neko/agent:warm-uncommitted node /bench/fork-smoke.mjs
```

`live.mts` and `collect.py` beside the smoke test record the live scenario and
memory samples. The harness runs in an existing demo worker, uses its encrypted
provider configuration, and needs the current launcher bundled as
`/app/warm-test/sandbox-launcher.mjs` with its package dependencies available.
It writes demo threads/runs and executes read-only data queries. It does not enable
the feature in the normal web process or supply a production authorization hook.

Runtime defaults: `OPENNEKO_AGENT_WARM_POOL_SIZE=1` and
`OPENNEKO_AGENT_WARM_IDLE_MS=180000`. Set the pool size to zero to disable generic
prewarming. Solo admin reuse is enabled by default; other actors require the
trusted authorization-revision hook described above. The benchmark itself left
the running demo web process unchanged.
