# Request-independent Hermes prewarming

Measured 12 September 2026 on the same ARM64 MacBook Air/Docker setup as the
[baseline](2026-09-12-agent-startup.md). Original agent image and Hermes dependencies
from `186383e`; two-CPU and 768 MiB caps. [Raw measurements](2026-09-12-generic-prewarm.json).

## Result

**We can preload common Hermes code without knowing the request.** In the real
OpenShell gateway probe, common-code prewarming took **1.76–1.83 s**, then waited at
**117.7–117.9 MiB working set** (**118.2–118.4 MiB cgroup current**). After the request
fixture was supplied, session/new took **1.20 s without MCP** or **1.62 s with a
small MCP fixture**.

That validates a narrower boundary than the [earlier warm-session experiment](2026-09-12-latency-profile.md).
The earlier 6.5–10.3 s covered a real, already-configured run. It is **not a proven
saving for a generic slot**. These new tests establish about **1.8 s of measured
common startup** that can happen before assignment; production end-to-end savings
remain unmeasured. The workloads, available configuration, caches, and inference
activity differ, so do not subtract the offline timings from the earlier live run.

## What was allowed before checkout

Each sandbox/process started with a new empty home and a restricted environment
containing paths and runtime flags only. No actual API key, org identity, broker
token, user filesystem, memory, private pack, or configured MCP server was supplied.
`config.yaml` was absent before launch and remained absent throughout the five-second
idle interval. The future workspace was not created until after that interval.

Two variants:

- **stock:** start `hermes --yolo acp`, send ACP `initialize`, then wait. This is
  protocol readiness, not an initialized agent session.
- **preload:** first import `run_agent.AIAgent`, `model_tools`, and `toolsets`, then
  invoke the same Hermes CLI and ACP initialization. No AIAgent object or session
  is created before checkout. This loads code, not request permissions or content.

The preload registered **83 unscoped built-in tool entries**, including terminal.
The common installation also registered 12 entries from its bundled A2A/Spotify
plugin modules in an internal scope. They had no account credentials or MCP
attachment. Importing bundled tool/plugin code is not granting access to an account,
and registry presence does not prove every optional tool is available or usable.
No non-bundled plugin was installed during these tests.

Direct Docker probes used `--network none`, a read-only image, an isolated tmpfs,
dropped capabilities, and no-new-privileges. Gateway probes used an empty
`network_policies` map (deny-all egress), fixed sandbox UID, and the launcher's usual
filesystem shape with read-only `/bench` and `/sys` added for the benchmark and
memory counters. No network policy was widened during a probe.

## What arrived at checkout

After idle sampling, the harness created a fresh workspace and wrote a synthetic
model/provider configuration. The model name differed by sample. The provider was
an offline loopback fixture with a deliberately invalid, non-secret key. The
returned session state was checked for that newly supplied model name.

Odd samples offered one stdio MCP fixture in `session/new`; even samples offered
none. No MCP attachment was supplied at process start. Successful `session/new`
proves the late configuration was accepted; the benchmark did not invoke the
fixture tool through a model or exercise a real OpenNeko broker/private pack.
No model inference or real external API call was performed. Metadata probes may
be attempted by Hermes after checkout; the network restrictions remain in force.

## Measurements

Each idle value is the median of five one-second samples. `Initialize` includes
process launch, optional imports, CLI startup, and the ACP initialization response.
It excludes container creation. `Late session` starts immediately before the
`session/new` call; the small config-file write and directory creation precede it.

### Real OpenShell gateway

| Variant | Sample | Late MCP | Initialize (s) | Idle working set (MiB) | Idle cgroup current (MiB) | Late session (s) |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| stock | 1 | 1 fixture | 1.484 | 192.3 | 214.3 | 2.768 |
| preload | 1 | 1 fixture | 1.764 | 117.7 | 118.2 | 1.623 |
| stock | 2 | None | 1.035 | 105.9 | 106.4 | 1.691 |
| preload | 2 | None | 1.826 | 117.9 | 118.4 | 1.195 |

For the matched gateway cases, common imports reduced the work left in session/new
by **1.15 s with MCP** and **0.50 s without MCP**, versus a stock initialized ACP
process. Preload initialization itself took longer than stock; that work would
happen during pool replenishment, not disappear. Two generic preloaded slots would
arithmetically occupy about **236 MiB** at this measured footprint, excluding extra
pool/controller memory; a two-slot pool was not built or measured.

### Direct Docker, network disabled

| Variant | Sample | Late MCP | Initialize (s) | Idle working set (MiB) | Idle cgroup current (MiB) | Late session (s) |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| stock | 1 | 1 fixture | 1.569 | 161.0 | 180.7 | 2.386 |
| preload | 1 | 1 fixture | 1.910 | 103.1 | 103.1 | 1.510 |
| stock | 2 | None | 1.137 | 90.3 | 90.3 | 1.559 |
| preload | 2 | None | 1.827 | 102.8 | 102.8 | 1.002 |
| stock | 3 | 1 fixture | 1.346 | 91.0 | 108.1 | 2.209 |
| preload | 3 | 1 fixture | 1.886 | 103.0 | 103.0 | 1.416 |
| stock | 4 | None | 1.090 | 91.2 | 91.2 | 1.420 |
| preload | 4 | None | 1.891 | 102.9 | 102.9 | 0.841 |

The first stock sample charged substantially more memory than later samples.
Retain it rather than reporting only the favorable warmed-cache numbers. Docker
working set is `memory.current - inactive_file`; cgroup current includes charged
file cache. Neither is process RSS or total host memory. Gateway figures also
include sandbox supervision and the benchmark's Node process. The first exploratory
probe failed a harness assertion that mistook an internal bundled-plugin registry
scope for an MCP attachment; the corrected checks produced the samples above.

## What this means for a pool

A generic slot can contain the Python/Hermes process, common imported modules, and
bundled tool code. It need not contain the user's memory, packs, selected MCP tools,
provider key, or prompt. A slot would be consumed once; the newly assigned run would
create its own session and receive only its authorized inputs.

Remaining implementation work is significant and is not covered by this timing test:

- Attach the real provider configuration, workspace, broker identity, pack/memory
  permissions, and selected MCP servers after assignment.
- Preserve a fixed compatible filesystem/process sandbox policy. OpenShell exposes
  live policy update commands, but safe late egress/provider binding was not tested
  here. A slot with incompatible creation-time policy must not be reused.
- Keep empty slot-local paths stable across preload/assignment, or audit imported
  modules that cache paths/configuration. Our test kept one empty home path and
  filled it later; it did not switch an existing process between org homes.
- Recheck optional-tool availability after assignment. Import caches/registries
  must not freeze credentials or availability decisions made before checkout.
- Measure actual first-token latency through the production Node launcher and
  broker, plus replenishment behavior and memory under concurrency.

There is still **no production pool**. No prompt or production runtime was changed,
and the live demo's normal agent image tag was left unchanged. All test containers
were disposable; no credentials or user content were passed into them.

## Reproduce the component benchmark

The probe itself contains assertions for late model selection, absence of config
before checkout, and common built-in tool registration. From the repository root:

```sh
docker build -f scripts/bench/generic-warm/Dockerfile \
  --build-arg BASE_IMAGE=ghcr.io/open-neko/agent:branch-186383e \
  -t openneko-generic-warm:bench .
for sample in 1 2 3 4; do
  for variant in stock preload; do
    docker run --rm --network none --memory 768m --cpus 2 \
      --cap-drop ALL --security-opt no-new-privileges --read-only \
      --tmpfs /tmp:rw,nosuid,nodev,size=128m \
      openneko-generic-warm:bench "$variant" "$sample"
  done
done
```

For gateway parity, create a disposable OpenShell sandbox from this probe image
with the same resource caps and the deny-all policy described above. Run
`node /bench/measure.mjs <variant> <sample>` as the initial command, use `--no-keep`,
and verify deletion afterward. These are startup/session microbenchmarks, not
live Gemini response benchmarks or a security audit of a future pool.
