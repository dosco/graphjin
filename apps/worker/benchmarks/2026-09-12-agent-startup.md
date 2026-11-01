# Agent startup and limited demo benchmark — 12 September 2026

Source: `186383e` on `fix/agent-cold-start`. All 12 project images were built
locally from this commit and tagged `ghcr.io/open-neko/<image>:branch-186383e`.
[Machine-readable measurements](2026-09-12-agent-startup.json) include exact image
IDs, bytes, run IDs, phase timings, model usage, caps, and memory samples.

## Test conditions

- MacBook Air M2 (`Mac14,2`), 8 GiB physical RAM; user reported about 4 GB available.
- Docker 29.4.0 on Linux ARM64, 8 visible CPUs, 5,229,015,040-byte VM limit (4.87 GiB).
- Packaged CLI demo mode, AdventureWorks data, containerized OpenShell 0.0.54.
- Gemini `gemini-3.5-flash-lite`; real provider calls, brokered GraphJin queries,
  fresh sandbox per session, local images already present. No image pulls in run timings.
- One initial session, followed by one batch of three overlapping Ask sessions.
  All four returned **31,465 sales orders**, checked independently with
  `SELECT count(*) FROM sales.salesorderheader`.
- Demo simulators and workflow cron triggers disabled. Background worker cap remained
  one; this does **not** serialize interactive Ask sessions. Three Ask sandboxes were
  observed running together. The test performed no external actions.
- Sandbox cap: **768 MiB / 2 CPUs**, a test override of the branch's **1 GiB / 2 CPU**
  default. Automatic memory-based cancellation was removed at the user's request;
  existing container caps remained. No session was cancelled.
- Small sample, warm host filesystem cache, shared laptop. No p95, cold-host install
  benchmark, matched `origin/main` baseline, or ten-active-session capacity claim.

## Startup and response time

| Measurement | Single session |
| --- | ---: |
| Stage workspace | 0.371 s |
| Sandbox create + upload | 1.369 s |
| Stage + create/upload | 1.740 s |
| Agent execution (includes model/tools) | 20.574 s |
| Sandbox deletion | 0.450 s |
| Launcher lifecycle total | 22.832 s |
| First output, harness telemetry | 22.312 s |
| Complete run, harness telemetry | 24.333 s |

Creation/upload means the OpenShell sandbox is available and files are staged;
it is **not** Hermes/model readiness. Execution includes Hermes startup, model
latency, tool calls, and output processing. Harness timing also includes work
outside the launcher. No separate Hermes-only initialization timing was captured
in these live runs. Artifact download was skipped for the empty artifact tree.

| Three concurrent sessions | Stage (s) | Create/upload (s) | Launcher total (s) | First output (s) | Run total (s) | Observed cgroup peak (MiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.202 | 2.586 | 28.901 | 28.220 | 30.491 | 427.2 |
| 2 | 0.194 | 2.542 | 28.204 | 27.762 | 30.001 | 433.1 |
| 3 | 0.199 | 2.573 | 29.465 | 28.668 | 31.232 | 417.9 |

Client-observed completion including the event stream: **29.962–31.328 s**.
All three completed successfully, with no harness retries. Aggregate throughput
for this one batch was about **5.7 completed sessions/minute** (3 / 31.328 s),
not a sustained-throughput result. The model-reported input totals were
106,293–107,563 tokens per concurrent session, including reported cache reads;
output totals were 223–252 tokens. These are multi-step run totals, not a claim
about one prompt's size. Billing/cost was not measured.

| Service measurement | Time | Endpoint / boundary |
| --- | ---: | --- |
| Worker restart to health | 8.320 s | `/health`, port 4100 |
| Web restart to setup API | 2.602 s | `/api/admin/change-password` |
| Embedding first request from sleep | 1.215 s | `/v1/embeddings`, 384 dimensions |
| Embedding warm request | 11.545 ms | Same text immediately afterward |
| Docling request accepted from sleep | 1.631 s | HTTP 202, asynchronous conversion |
| Docling extracted result from sleep | 36.603 s | One-page digital PDF, OCR disabled; 1-second polling |

Service restart timings include `docker restart` shutdown/start and readiness
polling, with dependencies already running. They do not measure a fresh stack
installation or full-stack boot. Lazy request timings include child startup and
request work; embedding input was `OpenNeko digital librarian smoke test`.
The PDF came from `scripts/smoke-librarian.mjs` and yielded 40 Markdown characters.
Model assets were already on disk; "from sleep" means the model process was absent,
not cold disk cache. Health checks alone never start the model child.

## Memory

**Three-session peak sampled aggregate: 2,194.8 MiB (2.14 GiB).**
Fourteen Docker samples covered the batch; all three sandboxes overlapped.
**Final idle stack: 869.9 MiB (0.85 GiB).**

| Service | Final idle Docker working set (MiB) | Test memory cap (MiB) |
| --- | ---: | ---: |
| web | 78.2 | 512 |
| worker | 159.8 | 640 |
| graphjin | 77.0 | 128 |
| openshell-gateway | 97.1 | 192 |
| neko-graphjin | 96.8 | 128 |
| records-watch-graphjin | 21.0 | 128 |
| records-graphjin | 16.6 | 128 |
| neko-backup | 13.0 | 128 |
| embedding | 8.6 | 512 |
| adventureworks-db | 126.7 | 256 |
| librarian | 7.5 | 2048 |
| records-db | 49.4 | 192 |
| neko-db | 118.0 | 256 |

The disabled simulator containers add less than 1 MiB combined and have no explicit
memory cap. Zero CPU quota in the evidence means no explicit service CPU limit.
Embedding and Docling each used one CPU; other shared services had no CPU quota.

The initial PDF check reached a **1,770.8 MiB Docling cgroup high-water mark**;
embedding reached **200.7 MiB**. Initial worker and web cgroup peaks were
**394.6 MiB** and **162.7 MiB**, respectively. These are container-lifetime peaks
collected before the three-session test and the explicit service restarts. A coarse
Docker sample during the first extraction showed approximately **2.5 GiB** for the
whole stack. It is not an exact extraction aggregate peak.

Docker CLI memory usage subtracts inactive file cache; cgroup `memory.peak` includes
charged cache and kernel memory. They are different metrics. Agent peaks above are
the largest cgroup high-water marks read while the ephemeral containers still
existed, so a final spike before deletion could be missed. Sampling and `docker exec`
add measurement overhead. Do not add independent service peaks as simultaneous use.
Container totals exclude the macOS processes, Docker VM/daemon overhead, and unrelated
host caches; this is not a measurement of total laptop RAM consumption.

No observed OOM kills or automatic service restarts. The explicit worker/web
restarts above were deliberate measurements. All inspected memory reservations
were **zero**: these memory caps do not preallocate their full amounts.

## Image sizes

Docker `image inspect .Size`: local uncompressed logical image bytes, including
layers. All images are ARM64. These are not registry transfer sizes or incremental
disk costs; layers are shared, and the two database tags resolve to the same image.

| Image | Bytes | MiB |
| --- | ---: | ---: |
| `neko-web` | 565,187,805 | 539.0 |
| `neko-worker` | 695,422,328 | 663.2 |
| `agent` | 1,352,359,404 | 1,289.7 |
| `plugin-base` | 236,619,021 | 225.7 |
| `neko-embedding` | 321,427,021 | 306.5 |
| `neko-librarian` | 1,438,985,655 | 1,372.3 |
| `neko-db` | 461,954,414 | 440.6 |
| `records-db` | 461,954,414 | 440.6 |
| `neko-graphjin` | 166,737,613 | 159.0 |
| `records-graphjin` | 166,742,135 | 159.0 |
| `neko-cli` | 16,401,010 | 15.6 |
| `neko-backup` | 500,272,341 | 477.1 |

The agent image is still **1.26 GiB** because it retains Hermes and document tools.
This test does not establish a size reduction versus main. Upstream dependencies
also used `postgres:16-alpine`, `busybox:latest`, and the OpenShell gateway image;
they are not branch-built artifacts.

## Processing, persistence, and limits

| Parameter | Branch default | This test |
| --- | --- | --- |
| Agent sandbox memory / CPUs | 1 GiB / 2 | 768 MiB / 2 |
| Worker background agent concurrency | 3 | 1; three independent Ask sessions |
| Embedding memory / CPUs / PIDs | 1 GiB / 1 / 64 | 512 MiB / 1 / 64 |
| Docling memory / PIDs | 4 GiB / 256 | 2 GiB / 256 |
| CLI Docling CPUs | min(host CPUs, 4) | 1 |
| Model idle timeout | 5 minutes | 20 seconds |
| Embedding inference | One at a time, 16 outstanding requests | Same |
| Durable dispatch | Every minute, up to 32 jobs, 128 outstanding | Same |
| Background embedding admission | One via PostgreSQL advisory lock | Same |
| Interactive / background embedding deadline | 15 s / 150 s | Same |
| Extraction admission | One converting + one waiting; excess HTTP 429 | Same |
| Result cache | 900 s TTL; 512 MiB; 256 results; 8 MiB/result | Same |

One test concept was inserted with a null vector. The real periodic dispatcher
created a durable `embedding_index` pg-boss job, which completed and stored a
384-dimensional vector. This was one-job correctness coverage, not queue-load or
crash-recovery testing. Keyword search requires no model; semantic search can wake
embedding. Both model services returned to `sleeping` after the idle interval.
Fetching the saved PDF result while Docling slept returned the expected content
without increasing its child-start counter. Cache resides on `librarian-results`;
this live check did not replace the container to retest volume persistence.

Embedding and extraction ports remained internal; their containers ran read-only,
with all capabilities dropped and `no-new-privileges`. The lazy listener has no
Docker socket. Internal authentication was not added. Security policy is inherited
from the branch; this small load test is not a security audit.

## Reproduction and interpretation

1. Build the main Dockerfile targets `web`, `worker`, `agent`, `embedding`,
   `neko-cli`, `neko-db`, `records-db`, `neko-backup`, `neko-graphjin`, and
   `records-graphjin`. Build librarian with `-f apps/librarian/Dockerfile --target
   librarian`; build plugin base with `-f docker/plugin-base.Dockerfile`.
2. Start a separate packaged demo instance with these local tags. Apply the test
   limits above, disable demo simulators and workflow cron triggers, and configure
   the model through provider settings. Keep credentials out of benchmark artifacts.
3. Create three threads through `POST /api/work/threads`, then issue their run
   requests concurrently to `POST /api/work/threads/<id>/runs`. Ask each for the
   total AdventureWorks sales-order count via a read-only query. Read each run's
   event stream to completion and verify the count against PostgreSQL.
4. Sample `docker stats --no-stream --format '{{json .}}'` throughout; read
   `/sys/fs/cgroup/memory.peak` inside live sandboxes before automatic deletion.
   Collect `sandbox_phase` logs and persisted `work_run_event` telemetry. Compare
   configured and observed model identity, completion state, answers, and retries.
5. Run `scripts/smoke-librarian.mjs` through the worker's Node process on stdin.
   Time submission and result separately. Exercise `/v1/embeddings` from sleep,
   then warm; wait for idle and verify cached extraction retrieval without restart.
6. Inspect image sizes, runtime limits, OOM counters, and final idle memory.

Three lightweight read-only sessions fit this measured setup. That does not prove
10 sessions plus document processing fit a 16 GiB server. Concurrent model/tool
work, large PDFs, OCR, background queues, Linux versus macOS, and cold host caches
need separate sizing. Cap totals can exceed RAM; idle eviction is not global
memory admission. Run a representative sustained workload on the target server
before treating the default 10 × 1 GiB sandbox budget as validated capacity.
