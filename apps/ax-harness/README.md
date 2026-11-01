# Ax Harness

Ax Harness is a Go agent harness built on the [Ax](https://github.com/ax-llm/ax)
framework. It is OpenNeko's agent backend.

Each harness run is one agent turn. It reads one run specification,
runs an Ax agent with tools, and writes events as JSON lines. `openneko serve`
runs many turns in one process, one turn per connection on a Unix socket.
The `ax-harness` binary, or the `openneko ax-harness` role, runs one turn per
process on stdin and stdout. The host owns history, approvals and scheduling.
The harness runs no commands.

## Layout

| Path | Contents |
| --- | --- |
| `internal/agent` | The agent loop, budgets, the budget summary, usage and cost, model routes and fallback, reasoning, the child agent and the skills catalog. |
| `internal/command` | Run input, model routes for any Ax provider, and the event stream. |
| `internal/mcp` | A stdio MCP client that exposes server tools as capabilities. |
| `internal/localtool` | File, upload and skill tools confined to one directory, and `web_fetch`. |
| `internal/catalog` | Model-call prices from the model catalog of the pinned Ax module. |
| `harness` | The OpenNeko entry: one turn from the run scope, the bridge tools and the model routes, and GraphJin's data agent when the organization opts in. |
| `serve` | The `openneko serve` socket server: agent turns, the host's model calls, and GraphJin config checks and applies. |
| `cmd/ax-harness` | The standalone binary. It calls `harness.Main`. |
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
