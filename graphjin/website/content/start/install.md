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

## Connect an AI client

```bash
graphjin mcp add codex
graphjin mcp add claude
```

For hosted GraphJin, point the MCP client at the HTTP endpoint:

```bash
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
```

See [MCP](/agentic/mcp/) and [OAuth](/agentic/oauth/) for hosted identity flows.
