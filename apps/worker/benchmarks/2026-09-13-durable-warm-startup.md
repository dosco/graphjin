# Durable warm startup validation — 13 September 2026

Working tree: `codex/durable-warm-startup`, based on `c280803` (v2.44.3).
Not deployed to the demo VM. No production latency claim.

Web instrumentation and worker startup await generic capacity. Pool-enabled
Work admission shares pending preparation instead of launching a separate
cold fallback. Exhausted capacity waits; explicitly disabled pools retain the
existing disposable path. Startup preparation failure leaves setup/pages
available and schedules retries; Work still requires a ready slot.

Binding reconciles real file hashes against staged inputs, removes stale files
and symlinks, restores agent-modified inputs, and uploads changed files only.
New turn inputs and broker credentials remain fresh. Provider/policy scope
invalidation remains unchanged. Checkout, reconciliation, upload, and transfer
counts have separate telemetry. Hermes configuration is no longer uploaded
both inside the run workspace and into the warm home.

## Checks

- 56 focused pool, launcher, and filesystem tests passed.
- Full local workspace suite: 2,540 passed, 64 opt-in tests skipped, zero
  failures, against a disposable database with all 73 migrations applied.
  LLM: 947 passed; web: 476 passed; worker: 618 passed.
- Repository-wide TypeScript checks passed.
- Two web instrumentation tests passed (await readiness, gateway failure,
  and unconfigured/edge runtime behavior).
- Web and worker TypeScript checks and web instrumentation lint passed.
- Real OpenShell integration test passed with a disposable local sandbox;
  the sandbox was deleted afterward. No model calls or production mutations.

The real test used `ghcr.io/open-neko/agent:warm-uncommitted`, one CPU,
512 MiB, and two files totaling 65,541 bytes. These are single observations:

| Operation | Time | Uploaded |
| --- | ---: | ---: |
| Startup preparation to ready | 4,330 ms | — |
| Initial directory sync | 796 ms | 65,541 bytes |
| Unchanged directory sync | 58 ms | 0 bytes |
| Changed turn input only | 109 ms | 5 bytes |

This exercises preparation and real OpenShell reconciliation/upload semantics,
not a full model turn or a representative production workspace. Provider and
policy binding are outside these directory-sync timings.

Repeat against a configured local gateway with an available agent image:

```sh
OPENNEKO_OPENSHELL_WARM_E2E=1 \
OPENNEKO_AGENT_IMAGE=ghcr.io/open-neko/agent:warm-uncommitted \
pnpm --filter @neko/llm exec vitest run test/sandbox-warm-e2e.test.ts
```
