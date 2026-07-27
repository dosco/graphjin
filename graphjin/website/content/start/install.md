---
title: "Install"
description: "Install the GraphJin CLI and scaffold a local project."
nav_group: "start"
doc_kind: "guide"
weight: 10
---

## Install the CLI

Use the install script when you want the native CLI binary:

```bash
curl -fsSL https://graphjin.com/install.sh | bash
graphjin version
```

The website serves the script at `/install.sh`; the build syncs it from the repository root before publishing.

Other common development paths:

```bash
graphjin serve --demo
brew install dosco/graphjin/graphjin
go install github.com/dosco/graphjin/cmd/graphjin@latest
```

`graphjin serve --demo` is the fastest smoke test - with no `--path` it extracts the built-in SaaS ops demo (SQLite, no Docker) to `./graphjin-demo` and boots it: schema, seeded data, saved queries, workflows, and (with a model key in `./.env`) the built-in agent. The other [demo verticals](/start/demos/) run from a repo clone via `--path examples/<name>`.

## Scaffold a project

```bash
graphjin serve new my-api
cd my-api
graphjin serve
```

The generated `dev.yml` and `agentic.yml` deliberately omit feature toggles:
their mode defaults provide managed artifacts, watches, the built-in agent,
stateful MCP HTTP, and the primitive MCP tools. `prod.yml` remains opt-in.
The scaffold also includes a database connection section, saved-query
directories, and source-mode templates.

{{< verified by="TestCmdNewWritesAgenticAndSourcesTemplates" file="cmd/cmd_new_test.go" line="14" >}}
{{< verified by="TestTemplatesDecodeAsConfig" file="cmd/cmd_new_test.go" line="83" >}}

The scaffold includes environment-specific config files. Use `dev.yml` for local work, `prod.yml` for locked-down deployments, and `agentic.yml` when MCP/catalog/security surfaces are the main interface.

The generated `dev.yml` sets `mcp.allow_config_updates: true`, so a scaffolded project is configurable from your AI IDE on day one — ask for a database connection or a new role and GraphJin writes it back to `dev.yml`. See [Configure GraphJin from your AI IDE](/agentic/mcp/#configure-graphjin-from-your-ai-ide).

## Connect an AI client

The shortest path needs nothing running — no server, no Docker, no config file, no model key:

```bash
claude mcp add graphjin -- graphjin mcp --demo
codex mcp add graphjin -- graphjin mcp --demo
```

`graphjin mcp --demo` extracts the built-in SaaS ops demo to `./graphjin-demo` and serves MCP over stdio. Your IDE's own model does the reasoning.

To connect to a GraphJin you are already running:

```bash
graphjin mcp add codex
graphjin mcp add claude
```

Start the server first — this path is HTTP-only and sends a real MCP `initialize` probe, so it stops if nothing answers. Defaults are client `codex`, server `http://localhost:8080`, and project scope.

For hosted GraphJin, point the MCP client at the HTTP endpoint:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
```

See [MCP](/agentic/mcp/) and [OAuth](/agentic/oauth/) for hosted identity flows.
