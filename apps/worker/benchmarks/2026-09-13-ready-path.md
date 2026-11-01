# Ready-to-execute improvements — 13 September 2026

Implemented in `codex/startup-ready-path`, in the dedicated
`openneko-startup-ready` worktree, based on origin/main
`67b4c6f5b9f3add6bd68813c64a4479347dec1b0` (v2.45.0).
The benchmark predates the rebase onto `0477d53` (origin/main, including PR #324).
No VM changes or deployment. The original eval checkout was preserved.
The timing artifacts retain the original measured source provenance.

## Changes

1. Immutable organization staging snapshots share prepared file manifests across
   turns. Atomic knowledge publication, skill/overlay edits, and team library
   changes invalidate the snapshot. Concurrent turns retain their own generation
   until they finish. Personal memory, uploads, credentials, and run inputs are
   excluded from the stable snapshot.
2. Warm checkout and workspace/config reconciliation use one sandbox exec with
   stdin input. Independent uploads and policy binding overlap; execution waits
   for all binding operations, including when one fails. Actual sandbox hashes
   still repair modified files and remove stale files.
3. Unused spares receive renewal every idle timeout / 3 (60 seconds by default).
   Checkout waits for an in-flight renewal. Failed spares are discarded and
   replenished; assigned-user scope checks and expiration remain unchanged.
4. Startup prepares an organization-scoped spare with stable knowledge, team
   library, and skill overrides already uploaded. Renewal refreshes changed
   generations. User configuration is bound only on assignment.

## Local comparison

Three fresh server processes with one prewarmed spare each; one first turn and
two immediate follow-ups in each process. Three additional processes explicitly
set pool size to zero for cold controls. Same image, database, seven-file input
fixture, model, and prompt before and after; 12 completed turns per version.
The baseline ran from a clean detached worktree at the commit above before the
final implementation. The image was not rebuilt or retagged:

`sha256:3e7b627a7e739e29bf38d29edde2e2141d61a632e9ff97293ef1c2f05f233fc9`

Real local PostgreSQL, GraphJin, embedding service, OpenShell, and Gemini
`gemini-3.5-flash-lite` were used. Next ran in development mode. Ready time is
server elapsed time at the end of `sandbox.warm_bind` or, for the cold control,
`sandbox.create_upload`; it excludes Next route compilation and model execution.
All prompts asked for 17 + 25, with no tools or data changes.

| Metric | Samples/version | Before median | After median | Observed reduction |
| --- | ---: | ---: | ---: | ---: |
| Warm first-turn ready | 3 | 4.539 s | 2.795 s | 38.4% |
| Hot-turn ready | 6 | 1.829 s | 1.564 s | 14.4% |
| Warm binding | 3 | 2.730 s | 1.105 s | 59.5% |
| Hot binding | 6 | 0.686 s | 0.311 s | 54.7% |
| Warm host staging | 3 | 263.5 ms | 65.6 ms | 75.1% |
| Hot host staging | 6 | 137.1 ms | 36.9 ms | 73.1% |
| Cold-control ready | 3 | 9.840 s | 2.456 s | Not attributable to this change |

Ready ranges: warm 3.910–5.276 s before / 1.774–5.795 s after;
hot 0.551–13.015 s before / 0.403–3.965 s after;
cold 2.934–11.086 s before / 2.262–4.620 s after.

Client total-time medians (including compilation and model response) were
23.452 → 25.600 s warm, 12.111 → 10.263 s hot, and 30.552 → 19.372 s cold.
This is not evidence that every turn or the model response will be faster.

The shared machine continued running other eval work. The large change in the
unchanged cold control demonstrates host/runtime variability; these small
sequential samples do not establish causal percentages or production p95.
The seven-file fixture also does not represent the 238-file VM workspace.
A controlled production-runtime comparison is needed before promising a VM SLA.

All nine pooled after turns hit the host staging cache. First turns used a
preloaded generic spare and uploaded one changed workspace file; follow-ups
reused assigned slots. Revision refresh, stale-file removal, and spare survival
were independently exercised with real OpenShell integration tests.

### Observer issue retained in the evidence

An initial after attempt was aborted by the benchmark after SSE returned 404
while the run was still running. Its cleanup interrupted execution after binding
had succeeded (run `7283b77b-53c7-486c-88e9-0178affcb1c0`). This interrupted attempt
is excluded from the timing aggregates, not counted as a completed turn.
The benchmark was corrected to await terminal status through the thread API
when the stream ends early. In the final batch the first process had three SSE
404s, but all three runs completed; the other nine streams succeeded. The cause
of those development-server stream 404s remains unconfirmed. Server timing logs
remain the source for ready/binding measurements. Client totals for those three
turns use polling and are not exactly equivalent to stream-observed totals.

## Validation

After rebasing onto `0477d53`, the full serial workspace suite passed again:
**2,555 passed, 65 optional skips**. Web and worker typechecks also passed.
The first post-rebase invocation rejected an inherited benchmark-only development
flag; removing that flag from the test environment resolved the setup error.

Original measured-revision validation:

- Full serial local workspace suite passed: **2,552 tests passed, 65 optional
  tests skipped**, against an isolated migrated test database.
- The initial concurrent workspace invocation hit web/worker 10-second timeouts
  while sharing that test database; the serial rerun passed without increasing
  timeouts. This was not hidden by excluding test files.
- Both opt-in real OpenShell tests passed: a 402-file manifest above the 32 KiB
  argument limit, and preloading/catalog refresh/stale-file deletion while the
  same spare survived beyond its configured idle expiry.
- Web and worker TypeScript checks passed. These also typecheck the imported
  LLM implementation. `git diff --check` passed.
- New regression checks cover immutable generation lifetime, org separation,
  exclusion of personal inputs, atomic publication, invalidation, renewal, and
  checkout waiting for renewal. Existing binding/scope/cleanup tests passed.

## Reproduction and artifacts

`2026-09-13-ready-path-before.json` and `2026-09-13-ready-path-after.json` contain
sanitized per-run timings and image/source provenance. `ready-path-benchmark.py`
retains the HTTP experiment with the terminal-status fallback.

Provision an isolated database and an isolated writable config/agent home,
apply migrations, and put the required environment in `STATE_DIR/web-env.json`
with mode 0600. Set the database URL, agent home, XDG config home, gateway,
agent image, reachable GraphJin/embedding endpoints, broker port 4298, and
`OPENNEKO_HOST_WEB_DEV=1`. Do not point the benchmark at a production database.
Reserve web port 3288. The script starts/stops only its own web process group;
callers own their fixture services. Use a fresh output label for each attempt.

```sh
python3 apps/worker/benchmarks/ready-path-benchmark.py CLEAN_BASELINE before STATE_DIR
python3 apps/worker/benchmarks/ready-path-benchmark.py CHANGED_WORKTREE after STATE_DIR
pnpm -r --workspace-concurrency=1 test
OPENNEKO_OPENSHELL_WARM_E2E=1 OPENNEKO_AGENT_IMAGE=openneko-agent:eval \
  pnpm --filter @neko/llm exec vitest run test/sandbox-warm-e2e.test.ts
```

Runtime fixture credentials and raw logs remain outside the repository under
`/tmp/openneko-startup-bench-state`; they are not included in the artifacts.
