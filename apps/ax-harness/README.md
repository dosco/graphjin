# Ax Harness

Ax Harness is a Go agent harness built on the [Ax](https://github.com/ax-llm/ax)
framework. It is OpenNeko's agent backend.

The harness runs one agent turn per process. It reads one run specification
on stdin, runs an Ax agent with tools, and writes events as JSON lines on
stdout. The host owns history, approvals, the sandbox and scheduling.

## Layout

| Path | Contents |
| --- | --- |
| `internal/agent` | The agent loop, budgets, the budget summary, usage and cost, model routes and fallback, reasoning, the child agent and the skills catalog. |
| `internal/command` | Run input, model routes for any Ax provider, and the event stream. |
| `internal/mcp` | A stdio MCP client that exposes server tools as capabilities. |
| `internal/localtool` | File, upload and skill tools confined to one directory, and the `terminal` tool. |
| `cmd/ax-harness` | The OpenNeko entry point. It is the only OpenNeko-aware code. |
| `compat` | Checks of the pinned Ax behavior that the harness uses. |

The protocol is in [docs/RUN-PROTOCOL.md](docs/RUN-PROTOCOL.md). Notes on Ax
development are in [docs/AX-DEVELOPMENT.md](docs/AX-DEVELOPMENT.md).

## Checks

```sh
go vet ./...
go test -race -count=1 -timeout 10m ./...
```

`internal/mcp` has opt-in tests against the real OpenNeko bridge. Install the
`apps/worker` dependencies, then set `OPENNEKO_BRIDGE_TEST=1`.
