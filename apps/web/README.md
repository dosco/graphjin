# Web

The OpenNeko web app (`@neko/web`) is a Next.js app. It serves chat, the admin
console, and the API routes. It is also a trusted control plane: it starts the
agent runner for each chat turn. See [AGENT_RUNTIME.md](../../AGENT_RUNTIME.md).

## Development

Run the web app with the rest of the stack from the repository root:

```bash
pnpm dev:setup   # once: dependency services and demo data
pnpm dev         # openneko serve, web and the worker, with hot reload
```

Open [http://localhost:3000](http://localhost:3000). The app opens on `/chat`.
[INSTALL.md → Developer Setup](../../INSTALL.md#developer-setup) has the full
setup.

## Checks

```bash
pnpm --filter @neko/web lint
pnpm --filter @neko/web typecheck
pnpm --filter @neko/web test
pnpm ui:check    # from the repository root
```

UI changes follow [AGENTS.md](AGENTS.md), [DESIGN.md](DESIGN.md) and
[src/components/ui/README.md](src/components/ui/README.md).
