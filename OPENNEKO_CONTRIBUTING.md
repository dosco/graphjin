# Contributing to OpenNeko

Thanks for your interest in contributing. Issues and pull requests are welcome.

## Reporting bugs and requesting features

File an issue at [github.com/open-neko/openneko/issues](https://github.com/open-neko/openneko/issues). Please include:

- What you were doing
- What you expected
- What actually happened
- OpenNeko version and how you installed it (Docker / dev source)

## Setting up your dev environment

Setup commands — Node deps, migrations, `pnpm dev`, `openneko serve` (the customer GraphJin and the subscriptions GraphJin), the AdventureWorks dev variant, and the external CLI installer (`./scripts/install-clis.sh`) — live in [INSTALL.md → Developer Setup](INSTALL.md#developer-setup). Update / reset / troubleshooting are in the same file.

## Repository layout

```text
apps/
  web/                 Next.js UI and API routes
  worker/              Background job runner, built-in channels and sign-in, agent runner
  openneko/            `openneko` CLI + stack supervisor (Go), distributed as a single binary
  ax-harness/          Agent harness (Go): the `serve` role that runs agent turns
  embedding/           EmbeddingGemma 2 embedding service (Python)
packages/
  db/                  Drizzle ORM client, schema, migrations, job queue
  llm/                 Providers, agents, classifier, GraphJin work support; Library
                       file storage (src/library), bundled skills (assets/)
  secrets/             Deployment secrets store, at-rest cipher, Infisical resolver (TS — consumed by worker)
  packs/               Solution pack manifest, bundle loader and validation
  channels/            Built-in Slack, Telegram and HUD channels
packs/                 First-party solution packs (Magento, Google Workspace, Slack, …)
core/, serv/, agent/,  GraphJin's Go modules at the repository root, built into the
cmd/, …                `openneko` binary. See CONTRIBUTING.md for GraphJin.
db/
  migrations/          Metadata database migrations (mirrored into apps/openneko/assets/migrations/ for embedding)
  graphjin/            Sample GraphJin config
  seeds/dev/           AdventureWorks seed assets
```

## Before opening a pull request

Run the relevant checks:

```bash
pnpm test
pnpm lint
pnpm build

# For changes that touch apps/openneko/** or db/migrations/**:
cd apps/openneko
go vet ./...
go test ./... -count=1
./scripts/sync-migrations.sh --check
# Integration tests (need docker on the host):
go test -tags=integration -count=1 -timeout 10m ./internal/db/...
```

Keep PRs focused — one change per PR makes review tractable. Include a short description of the user-visible behavior change, and link the issue it closes if there is one.

## Contributor License Agreement

Before your first pull request can be merged, you'll need to sign the OpenNeko [Contributor License Agreement](CLA.md). A bot prompts you automatically on your first PR — it's a single comment to sign.

The CLA lets you keep the copyright in your contribution while granting the project the rights it needs to license OpenNeko under the [Apache License 2.0](LICENSE) and relicense if ever necessary. See [LICENSING.md](LICENSING.md) for how OpenNeko is licensed.
