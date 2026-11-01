# Query-to-first-output profile and warm-sandbox experiment

Measured 12 September 2026, runtime commit `186383e`, on the same MacBook Air
M2 / Linux ARM64 Docker demo documented in the [baseline benchmark](2026-09-12-agent-startup.md).
Model: `gemini-3.5-flash-lite`. Sandbox limits remained 768 MiB and two CPUs.
[Raw traces and derived metrics](2026-09-12-latency-profile.json) contain no prompts,
answers, credentials, or tool results; prompt **byte counts** are recorded.

## Findings

- Fully initialized warm sandboxes deserve a prototype: measured Node + Hermes +
  session startup was **6.46–10.35 seconds** before the first prompt was sent.
  This is a substantially larger opportunity than container provisioning alone.
- A real initialized session held idle for 20 seconds used **375.3–376.6 MiB**
  of cgroup memory (median **376.3 MiB**, 27 samples). After release, first answer
  emission took **8.896 s** and delivery to the local SSE client took **8.974 s**.
- Blank OpenShell boxes cost only **10.6–11.0 MiB** of measured idle working set,
  but creation plus initial `true` cost only **0.688–1.256 s** in three samples.
  Blank pooling leaves the larger agent/session startup untouched.
- The normal four-request query spent **9–10.5 s** between prompt submission and
  answer emission. A different agent-chosen path used **11 provider requests**
  and took **35.79 s** for that segment. Keep that variability separate from boot.
- First-answer emission to the SSE client was **23–109 ms** across the five
  non-held runs. These short answers do not support treating web buffering as
  the dominant bottleneck. The debounce behavior remains a concern for longer,
  continuously streamed answers; it was not changed or benchmarked here.

No prompt reduction, eager tool discovery, pooling, or production streaming change
was implemented. Profiling used disposable derived images; the original agent tag
was restored afterward and all profiling/blank sandboxes were deleted.

## Timeline: ordinary fresh sessions

All durations below are seconds except the last column. Thread creation occurs
before the client query clock starts. Fresh images were already local; host caches
were warm. Each run used a newly created OpenShell sandbox.

| Run | Query → Node preload | Node setup → Hermes spawn | Hermes initialize | Session/new including MCP | Query → first client answer | Answer emit → client (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| no_tools | 3.875 | 0.645 | 3.934 | 5.751 | 17.795 | 60 |
| query_1 | 2.299 | 0.739 | 3.969 | 4.922 | 21.192 | 51 |
| query_2 | 2.242 | 0.409 | 3.664 | 4.624 | 20.125 | 109 |
| query_provider | 2.937 | 0.491 | 3.697 | 3.267 | 21.003 | 66 |
| query_variable_path_no_hold | 2.653 | 0.585 | 2.781 | 3.086 | 44.926 | 23 |

`no_tools` requested exactly OK without tools. The other runs requested the
AdventureWorks sales-order count with a read-only query; all answered **31,465**,
matching the independently checked database count. The unchanged OpenNeko prompt
supplied to ACP was 50,944 bytes for the control and 51,382 bytes for the data query.
These counts are not model input tokens and exclude Hermes' own added context.

The last ordinary run was initially intended as a held session, but the sandbox
exec environment did not pass the profiling flag. Its trace contains no hold and
is included as an additional ordinary run, not discarded as a slow outlier.

`Query → Node` includes web preparation, sandbox creation/upload, exec dispatch,
and initial Node interpreter work. It cannot all be removed by a pool. `Hermes
initialize` runs from the ACP initialize write to response and includes Python
startup/imports. `Session/new` includes Hermes session setup, MCP child setup, tool
listing, and other framework work; it is not synonymous with MCP handshake time.

The wrappers intentionally hold the process open **five seconds after result
emission** to retrieve the trace before sandbox deletion. Raw client completion
and launcher total times therefore include artificial delay; use first-output
boundaries here, not those totals, for production comparisons.

## Provider and tool path

In the representative `query_provider` run, the four SDK request-to-first-chunk
measurements were:

| Provider request | First chunk (s) | First answer text (s; 0 means none observed) |
| --- | ---: | ---: |
| 1 | 2.077 | 0.000 |
| 2 | 1.165 | 0.000 |
| 3 | 2.164 | 0.000 |
| 4 | 2.174 | 2.628 |

The early chunks were reasoning-summary or tool-call output, not answer text.
The final request's first answer text arrived at **2.628 s** after that request.
From that text chunk to the first ACP answer chunk took about **447 ms**, then
about **2 ms** to the agent event and **66 ms** to the SSE client. Thus provider
output and user-visible answer TTFT are distinct metrics.

The two initial discovery tool calls completed in approximately **156 ms** and
**39 ms** in ACP, respectively. The query tool's start to the next provider request
was about **295 ms**; that interval includes result handling and is not a separately
instrumented database execution time. Much of the elapsed time between tool steps
was waiting on the next model request, rather than running the tool itself.

We retained the extra 11-request run. Its first-chunk waits were **1.13–2.89 s**
per request. The same user question need not choose the same tool path, even with
an unchanged prompt/model; warming will not eliminate this source of variance.

## Warmed idle experiment

For one fresh sandbox, the wrapper allowed real Hermes initialize and session/new,
including the authenticated MCP bridge, to complete. It then withheld session/prompt
for **20 seconds**, sampled memory, and delivered the unchanged request. This tests
an initialized, already-configured sandbox; it is **not** a generic pool checkout
or cross-user reuse test. Run credentials/policy were established before the hold.

| Measurement | Result |
| --- | ---: |
| Node preload → initialized session/prompt-ready | 6.724 s |
| Deliberate hold | 20.005 s |
| Warm idle cgroup memory min / median / max | 375.3 / 376.3 / 376.6 MiB |
| Stable-window samples (exclude first 2 s and last 1 s) | 27 |
| Release → first agent answer event | 8.896 s |
| Release → first client answer | 8.974 s |
| Provider requests after release | 4 |
| Answer | 31,465 sales orders |

Cgroup memory includes charged file cache/kernel memory. An observed Docker working
set during the experiment was approximately 325 MiB, but it was not sampled over the
same stable interval, so use the cgroup figures for the documented idle footprint.
Two slots would arithmetically be about **753 MiB** at the measured cgroup footprint;
that is an estimate, not a measured two-slot pool or guaranteed scaling result.

The bridge itself exposed 60 tools through one multiplexed process:

| Session/new subphase | Time |
| --- | ---: |
| Session/new sent → bridge Node wrapper starts | 2.695 s |
| Bridge wrapper starts → MCP initialize response | 0.138 s |
| MCP initialize response → tool list response | 0.150 s |
| Tool list response → session/new response | 0.445 s |
| Total session/new | 3.428 s |

Most of this sample's session setup precedes the bridge's Node startup. Changing
bridge tool loading alone would not recover the full session/new time.

## Blank container comparison

Created through the same real OpenShell gateway with the original agent image,
768 MiB/two-CPU caps, default policy, no provider attachment, no workspace upload,
and initial command `true`. Three independent boxes were sampled three times each
then deleted. Create/initial-command times were **1.256, 0.688, 0.783 s**. Working set
(`memory.current - inactive_file`) was **10.6–11.0 MiB**; raw cgroup current was
**11.0–13.3 MiB**. These omit per-run policy/provider/upload overhead and are not a
like-for-like replacement for the production create/upload phase.

## Measurement method and limits

- A temporary Node wrapper around the unchanged bundled entry intercepted only
  Hermes spawn, ACP request/response metadata, and output event types. It did not
  log request bodies. Timestamps used monotonic milliseconds and epoch milliseconds.
- A temporary copy of installed Hermes `agent/chat_completion_helpers.py` recorded
  a marker immediately before `request_client.chat.completions.create`, and first
  accepted SDK chunk/text/reasoning/tool markers in `_accept_stream_chunk`.
  These include network/SDK latency, not just provider inference. They are not
  wire-level network traces or a measurement of hidden reasoning content.
- The warm image also wrapped the existing MCP bundle to timestamp process start,
  initialize reply, and tool-list reply. The corrected hold was baked into the
  wrapper because an image ENV flag did not reach the sandbox exec process.
- A Python host client recorded request acknowledgement and SSE frame arrival.
  It sampled `/sys/fs/cgroup/memory.current` and `memory.peak` via Docker exec
  roughly every 0.4 s plus command overhead. Warm stable-window sampling excluded
  the first two and last one seconds of the hold. No memory cancellation was used.
- Browser JavaScript rendering/paint was **not** measured. Client timestamps are
  local HTTP/SSE receipt, not visual paint. Cross-process epoch comparisons assume
  the host/VM clocks remain aligned; small delivery figures are approximate.
- Temporary wrappers, synchronous metadata writes, Docker sampling, cache warmth,
  background laptop activity, and model variability affect these numbers. The
  data cannot establish p95, sustained throughput, or a before/after pool gain.
- Original runtime image restored to
  `sha256:617c3cb3b2b20aff56bbce691003086585d21aebbe2434242dfcb2e03345dfb9`.
  No remaining test sandboxes; checked web/worker/GraphJin/gateway had zero automatic
  restarts and no OOM-killed state. The demo stack remains available.

## Decision supported by the measurements

A **fully initialized** single-use warm slot offers a measured startup segment of
roughly **6.5–10.3 s** to move ahead of the request, in addition to some provisioning
work. A blank pool targets only the smaller container phase. Neither eliminates
provider/tool-loop latency, and the released warm example still needed ~9 s to
answer. This is an opportunity estimate, not a promise that a generic pool can
safely preload every step.

Next experiment: one initialized slot for a compatible policy/provider/org setup,
consumed once and replenished asynchronously, with a memory budget and idle expiry.
Verify what state can safely be prepared before the run is known. Never reassign
an already-used session across users or silently carry stale credentials/egress
policy. Optimize Hermes startup/import/session work if generic prewarming cannot
preserve that boundary. Keep the prompt and tool-discovery strategy unchanged.
