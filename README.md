# DeepORG — One AI Agent for Your Whole Organization

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/tags)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)

> **DeepORG was formerly GraphJin.** The CLI (`graphjin`), the Go module, the npm package and the Docker images keep the `graphjin` name for now.

DeepORG is an AI agent that answers questions and makes changes across the
systems your company already runs: databases, warehouses, APIs, files and
source code. Ask it in plain words; it finds the right tables, has the database
do the work, and answers with the data behind it. It can only see and change
what your access rules allow, and by default a change waits for your approval.

## Why it's different

**No hand-built tools.** Most company agents are wired up one tool at a time:
a "get customer" tool here, a "list invoices" tool there, each written and
maintained by hand. They break when a schema changes, and every new question
or new system means more tools. DeepORG gives the agent a searchable map of
everything you connect, and the agent writes its own queries against it. The
engine checks each query against your access rules and runs it as one efficient
database query. Connecting a new database or API is a few lines of config, not
a new set of tools.

**Proven on a public benchmark.** [DeepORG, the Organizational Agent
Benchmark](https://graphjin.com/benchmarks/deeporg/) scores AI models on real
organizational work against a live system: answers, changes, alerts, follow-ups
and questions that span several sources. The best published run scores 97 of
100 with zero unsafe actions, and every score links to the exact test and build
that produced it.

**Turn your organization into a training environment.** The same machinery
generates checked tasks from your own systems and serves them as graded
episodes, so you can measure an agent, or train one with reinforcement
learning, on your organization instead of a toy.
[Agent environment](https://graphjin.com/environment/)

Underneath is GraphJin, the open-source engine that compiles each query into a
single database query. It is also a GraphQL API server and a Go library in its
own right.

## Install

```bash
curl -fsSL https://graphjin.com/install.sh | bash   # macOS and Linux
npm install -g graphjin                              # any platform
brew install dosco/graphjin/graphjin                 # macOS
```

Windows: `scoop bucket add graphjin https://github.com/dosco/graphjin-scoop`
then `scoop install graphjin`. Linux packages are on the
[releases page](https://github.com/dosco/graphjin/releases), and Docker is
`docker pull dosco/graphjin`.

## Try it

```bash
graphjin serve --demo
```

This starts a demo SaaS company (accounts, subscriptions, invoices, support
tickets) with seeded data. Put a model key in `./.env` (`OPENAI_API_KEY`,
`ANTHROPIC_API_KEY` or `GOOGLE_API_KEY`) and ask it something:

```bash
curl -sS localhost:8083/api/v1/agent \
  -H 'content-type: application/json' \
  -d '{"instruction": "Which account is most at risk of churning?"}'
```

Or chat with it at <http://localhost:8083/agent>. More demos:
[Demos](https://graphjin.com/start/demos/).

## Use it from Claude, Codex or any MCP client

```bash
graphjin mcp add claude http://localhost:8083
```

Other clients: [MCP](https://graphjin.com/agentic/mcp/). Hosted servers with sign-in:
[MCP OAuth](https://graphjin.com/agentic/oauth/).

## Learn more

| | |
|---|---|
| Get started | [Quick start](https://graphjin.com/start/quick-start/) · [Your first query](https://graphjin.com/start/first-query/) |
| The agent | [How it works](https://graphjin.com/agentic/server-agent/) · [Conversations](https://graphjin.com/agentic/lead-agent/) · [Watches](https://graphjin.com/agentic/watches/) · [Workflows](https://graphjin.com/agentic/workflows/) |
| Connect your systems | [Databases](https://graphjin.com/reference/database-support/) · [APIs](https://graphjin.com/integrations/openapi/) · [Files](https://graphjin.com/integrations/filesystem-uploads/) · [Source code](https://graphjin.com/integrations/codesql/) |
| Run it in production | [Production](https://graphjin.com/configure/environment-production/) · [Access rules](https://graphjin.com/configure/auth-rbac/) · [Config reference](CONFIG.md) |
| GraphQL and Go | [Query language](https://graphjin.com/core/) · [Go examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples) |
| Measure agents | [Benchmark](https://graphjin.com/benchmarks/deeporg/) · [Agent environment](https://graphjin.com/environment/) |

Works with PostgreSQL, MySQL, SQLite, MongoDB, Oracle, SQL Server, Snowflake,
Redshift, BigQuery, Cassandra, S3, GCS and local files.

## Community

[Discord](https://discord.gg/6pSWCTZ) · [Twitter @dosco](https://twitter.com/dosco)

## License

[Apache 2.0](https://opensource.org/licenses/Apache-2.0)
