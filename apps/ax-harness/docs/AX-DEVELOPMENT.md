# Ax development references

Ax publishes [Go agent skills](https://axllm.dev/go/skills/) next to the
generated `github.com/ax-llm/ax/packages/go` package. Use them when you change
the harness. They are separate from the task skills that a running agent reads.

Check each example against the pinned module source, its `API.md` and a no-key
fixture. The published site can describe a newer build than `go.mod`.
TypeScript examples do not prove Go behavior.

| Harness work | Ax Go guide | Check |
| --- | --- | --- |
| Provider routes, fallback and credentials | [`ax-go-ai`](https://axllm.dev/go/skills/ax-go-ai/) | Assert the endpoint, key and model of each stage. A fallback cannot change permissions or repeat a tool call. |
| Agent stages, child agents and tools | [`ax-go-agent`](https://axllm.dev/go/skills/ax-go-agent/), [`ax-go-agent-rlm`](https://axllm.dev/go/skills/ax-go-agent-rlm/) | Check tool result pairing, the shared child budget and cancellation. |
| Context and skill discovery | [`ax-go-agent-context`](https://axllm.dev/go/skills/ax-go-agent-context/), [`ax-go-agent-memory-skills`](https://axllm.dev/go/skills/ax-go-agent-memory-skills/) | Force compaction. User constraints and evidence references must stay in context. |
| Usage and diagnostics | [`ax-go-agent-observability`](https://axllm.dev/go/skills/ax-go-agent-observability/) | Every dispatched call has start and finish events with usage coverage. Telemetry stays content-free. |
| Typed tool input | [`ax-go-signature`](https://axllm.dev/go/skills/ax-go-signature/) | Validate schemas at admission. |

The harness rate limiter, not the Ax usage observer, decides call admission and
charges. A canceled stream can end without a usage report.
