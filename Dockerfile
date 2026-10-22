# syntax=docker/dockerfile:1.7
#
# One Dockerfile for the OpenNeko runtime images, with shared build and runtime
# layers. Release targets are enumerated in .github/workflows/release-binaries.yml.
#
# Runtime config strategy: the app reads ~/.config/openneko/config.json
# (DB connection) and ~/.config/openneko/secret-key (at-rest encryption
# key) on every boot. To support read-only container filesystems (e.g.
# Cloud Run), HOME + XDG_CONFIG_HOME point at writable /tmp paths and
# entrypoint.sh materializes those files from env vars before exec'ing
# the app. See entrypoint.sh for the env var contract.

# ─── 1. base: node + system tooling ────────────────────────────────────
# The official Node image also carries npm, Corepack, Yarn, C/C++ headers, and
# documentation. Copy only the executable into the shared runtime lineage;
# worker and agent add a pruned npm payload below because they genuinely offer
# runtime package installation. Web and plugin sandboxes do not pay that tax.
FROM node:24-bookworm-slim AS node-distribution

FROM node-distribution AS npm-payload
RUN rm -rf /usr/local/lib/node_modules/npm/docs /usr/local/lib/node_modules/npm/man \
    && find /usr/local/lib/node_modules/npm -type f -name '*.map' -delete

FROM debian:bookworm-slim AS node-runtime
COPY --from=node-distribution /usr/local/bin/node /usr/local/bin/node
COPY --from=node-distribution /usr/local/share/doc/node /usr/local/share/doc/node
RUN ln -s node /usr/local/bin/nodejs
# Retry transient apt mirror hiccups instead of hard-failing the image build.
RUN printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\n' > /etc/apt/apt.conf.d/80-retries
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl tini \
    && rm -rf /var/lib/apt/lists/*

# npm is part of the worker/agent feature contract, but its generated manuals,
# source maps, Corepack, Yarn, and development headers are not.
FROM node-runtime AS npm-runtime
COPY --from=npm-payload /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

# Build stages additionally need Corepack/pnpm. Keeping it in this build-only
# lineage prevents package-manager shims and downloads from shipping.
FROM npm-runtime AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
COPY --from=node-distribution /usr/local/lib/node_modules/corepack /usr/local/lib/node_modules/corepack
RUN ln -s ../lib/node_modules/corepack/dist/corepack.js /usr/local/bin/corepack \
    && corepack enable \
    && corepack prepare pnpm@9.14.1 --activate

# The metadata database has an enforced storage ABI: PostgreSQL 16 on Debian
# Bookworm/glibc. Text indexes persist in a named volume, so switching the
# libc/collation provider (for example, to Alpine/musl) can make an existing
# btree silently disagree with equality lookups. Keep the base immutable. If
# its collation provider/version changes, bump the compiled
# StorageContractVersion so existing volumes are rebuilt exactly once.
# neko-db is otherwise the stock pgvector image and entrypoint; backups are
# logical dumps taken by `openneko backup`.
FROM pgvector/pgvector:0.8.6-pg16-bookworm@sha256:ccc6e83d6e35e931dc7c5def2022729d5a6c370318d099181995567ff1fb4d6b AS neko-db
LABEL org.openneko.storage-contract="1" \
      org.openneko.storage-owner="999:999"
RUN test "$(getconf GNU_LIBC_VERSION)" = "glibc 2.36" \
    && test "$(pg_config --version)" = "PostgreSQL 16.15 (Debian 16.15-1.pgdg12+2)" \
    && test "$(id -u postgres):$(id -g postgres)" = "999:999"

# ─── 2. control-plane runtime ──────────────────────────────────────────
# Web is a trusted control plane. It runs each agent run's runner as a child
# process but carries no GraphJin CLI or document toolchain; terminal
# commands run in the agent image's sandbox. The worker uses the narrower
# shared GraphJin/npm runtime defined below.
FROM node-runtime AS runtime-base
# Git supports trusted config VCS and git-backed installs.
RUN apt-get update && apt-get install -y --no-install-recommends \
      git openssh-client \
    && rm -rf /var/lib/apt/lists/*

# ─── 2a. the openneko binary ───────────────────────────────────────────
# Every Go process in an install runs this one binary. It picks its role from
# the name it runs under, so each image links it as graphjin, ax-harness,
# lazy-service or graphjin-config. GraphJin builds from graphjin/ in this
# repository at the same commit; there is no release tarball and no pin.
FROM golang:1.25-bookworm AS go-build
WORKDIR /src
COPY graphjin graphjin
COPY apps/ax-harness apps/ax-harness
COPY apps/lazy-service apps/lazy-service
COPY apps/openneko apps/openneko
COPY db/migrations db/migrations
COPY scripts/build-openneko.sh scripts/build-openneko.sh
# Ensure the embedded migration copies match the canonical source. CI also
# runs this check separately; build-time guard prevents drift sneaking in
# via an image-only rebuild.
RUN cd apps/openneko && ./scripts/sync-migrations.sh --check
ARG OPENNEKO_VERSION=container
ARG OPENNEKO_COMMIT=container
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux OPENNEKO_VERSION="$OPENNEKO_VERSION" OPENNEKO_COMMIT="$OPENNEKO_COMMIT" \
    sh scripts/build-openneko.sh /out/openneko

# The worker requires npm plus the GraphJin CLI for trusted host-side Records
# configuration.
FROM npm-runtime AS graphjin-node-runtime
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/graphjin && graphjin version

# ─── 2b. agent OS base (serve and sandbox) ────────────────────────────
# The agent image runs `openneko serve` and `openneko sandbox` (the binary is
# copied in the `agent` stage). The document toolchain below uses Debian's
# system Python and pip.
FROM npm-runtime AS agent-base
RUN apt-get update && apt-get install -y --no-install-recommends \
      git python3 python3-pip \
    && rm -rf /var/lib/apt/lists/*

# ─── 2c. document toolchain (agent image only) ─────────────────────────
# Bundled skills (xlsx / pptx / docx / pdf / document-extraction) shell out to
# Python + LibreOffice + Poppler / qpdf / Tesseract via the agent's terminal,
# which `openneko sandbox` runs — web never runs these, so the ~1GB toolchain stays out
# of its image.
# Keep this list in sync with KNOWN_SKILL_DEPS (packages/llm/src/work/skill-deps.ts);
# `pnpm skills:check` prints the current aggregate.
FROM agent-base AS cli
ENV NODE_PATH=/usr/local/lib/node_modules
RUN npm install --global --omit=dev --no-audit --no-fund \
      docx@9.7.1 pptxgenjs@4.0.1 react@19.2.8 react-dom@19.2.8 \
      react-icons@5.7.0 sharp@0.35.3 \
    && npm cache clean --force
RUN apt-get update && apt-get install -y --no-install-recommends \
      libreoffice-core libreoffice-common libreoffice-writer libreoffice-calc \
      libreoffice-impress libreoffice-draw poppler-utils qpdf tesseract-ocr file \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir --break-system-packages \
       openpyxl python-pptx Pillow python-docx pypdf pdfplumber reportlab PyYAML \
       defusedxml lxml

# ─── 3. deps: workspace install (cached on lockfile) ───────────────────
FROM base AS deps
WORKDIR /app
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/worker/package.json apps/worker/package.json
COPY apps/embedding/package.json apps/embedding/package.json
COPY packages/avatars/package.json packages/avatars/package.json
COPY packages/channels/package.json packages/channels/package.json
COPY packages/db/package.json packages/db/package.json
COPY packages/interaction/package.json packages/interaction/package.json
COPY packages/llm/package.json packages/llm/package.json
COPY packages/packs/package.json packages/packs/package.json
COPY packages/secrets/package.json packages/secrets/package.json
COPY packages/secret-crypt/package.json packages/secret-crypt/package.json
COPY packages/telemetry/package.json packages/telemetry/package.json
# onnxruntime-node defaults Linux x64 installs to its 300+ MiB CUDA provider.
# Control-plane images use the bundled CPU runtime and ship no NVIDIA stack.
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    ONNXRUNTIME_NODE_INSTALL=skip \
      pnpm install --frozen-lockfile --side-effects-cache=false

# ─── 4. source + web build ─────────────────────────────────────────────
# Keep source assembly separate from the expensive Next.js build. Worker,
# agent, and one-shot targets only need the installed workspace plus sources;
# forcing them through `next build` added several minutes to every image build.
FROM deps AS source
WORKDIR /app
COPY . .

FROM source AS build
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter @neko/web build

# Next's standalone tree and its manually copied native dependency use the same
# server-only pruning contract as the worker deployment.
FROM build AS web-deploy
ARG TARGETARCH
RUN sh scripts/prune-node-runtime.sh /app/apps/web/.next/standalone "$TARGETARCH" \
    && sh scripts/prune-node-runtime.sh /app "$TARGETARCH"

# DuckDB CLI for data work in the agent terminal, pinned by checksum.
FROM debian:bookworm-slim AS duckdb-bin
ARG TARGETARCH
ARG DUCKDB_VERSION=v1.5.5
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && case "$TARGETARCH" in \
         amd64) sum=c61f21485e6e41d3a0c28ce9904ea18346309cf427b4cf9479bc3564348dc885 ;; \
         arm64) sum=50c719e603a4e599d435e5321542458edc1cfc5f7eed65979fe9bc5ae4e3ba23 ;; \
         *) echo "unsupported arch $TARGETARCH" && exit 1 ;; \
       esac \
    && curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -o /tmp/duckdb.gz \
         "https://github.com/duckdb/duckdb/releases/download/${DUCKDB_VERSION}/duckdb_cli-linux-${TARGETARCH}.gz" \
    && echo "$sum  /tmp/duckdb.gz" | sha256sum -c - \
    && gunzip -c /tmp/duckdb.gz > /usr/local/bin/duckdb \
    && chmod 0755 /usr/local/bin/duckdb \
    && /usr/local/bin/duckdb --version

# Small Node one-shots. Bundling them prevents demo/config initialization from
# reusing the full worker image and its production dependency closure.
FROM source AS init-tools-deploy
RUN mkdir -p /out/init-tools \
    && cd apps/worker \
    && pnpm exec esbuild ../../packages/llm/src/graphjin/init-secret.mjs \
      --bundle --minify --platform=node --format=esm \
      --banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" \
      --outfile=/out/init-tools/graphjin-init-secret.js \
    && pnpm exec esbuild ../../packages/db/src/seed-adventureworks.mjs \
      --bundle --minify --platform=node --format=esm \
      --banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" \
      --outfile=/out/init-tools/seed-adventureworks.js

# Alpine one-shots likewise need only the Node executable, not npm/Yarn.
FROM node:24-alpine AS node-alpine-distribution

FROM alpine:3.22 AS node-alpine-runtime
RUN apk add --no-cache libstdc++
COPY --from=node-alpine-distribution /usr/local/bin/node /usr/local/bin/node
COPY --from=node-alpine-distribution /usr/local/share/doc/node /usr/local/share/doc/node

FROM node-alpine-runtime AS init-tools
WORKDIR /app
COPY --from=init-tools-deploy /out/init-tools /app
CMD ["node", "--version"]

FROM source AS adventureworks-loader-deploy
RUN pnpm --dir apps/worker exec esbuild scripts/load-adventureworks.ts \
      --bundle --minify --platform=node --format=esm \
      --banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" \
      --outfile=/out/load-adventureworks.js

FROM node-alpine-runtime AS adventureworks-loader
RUN apk add --no-cache ca-certificates curl postgresql-client unzip
WORKDIR /app
COPY --from=adventureworks-loader-deploy /out/load-adventureworks.js /app/load-adventureworks.js
ENTRYPOINT ["node", "/app/load-adventureworks.js"]

# Stateless upstream used only by the backend eval to prove that GraphJin
# forwards an explicitly exposed API `call` mutation. It keeps no mutable
# state, so repeated/cancelled episodes are deterministic and require no reset.
FROM node-alpine-runtime AS eval-api-fixture
WORKDIR /app
COPY evals/environment/adventureworks/api/server.mjs /app/server.mjs
EXPOSE 8090
ENTRYPOINT ["node", "/app/server.mjs"]

FROM source AS embedding-deploy
ARG TARGETARCH
RUN pnpm --filter @neko/embedding deploy --prod /out/embedding \
    && sh scripts/prune-node-runtime.sh /out/embedding "$TARGETARCH"
# Bake and test the model. Offline readiness is a build requirement.
RUN cd /out/embedding && NEKO_TRANSFORMERS_CACHE=/out/embedding/models node model.mjs --prewarm

FROM node-runtime AS embedding
RUN useradd --system --create-home --uid 10001 embedding
COPY --from=embedding-deploy --chown=embedding:embedding /out/embedding /app
# The shared lazy listener is the openneko binary's lazy-service role.
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/lazy-service
ENV NEKO_TRANSFORMERS_CACHE=/app/models
WORKDIR /app
USER embedding
EXPOSE 5003
ENTRYPOINT ["lazy-service"]
CMD ["--listen", ":5003", "--upstream", "http://127.0.0.1:5004", "--", "node", "/app/server.mjs"]

# ─── 4z. agent runner bundle ───────────────────────────────────────────
# Compile the runner entry and multiplexed tool bridge. Ship their assets,
# not the worker's dependency closure (DB, ONNX, transformers, tsx). Web and
# worker copy the bundle to /opt/openneko-agent.
FROM source AS agent-deploy
RUN cd apps/worker && pnpm exec esbuild \
      src/agent-sandbox/entry.ts src/agent-sandbox/mcp-bridge.ts \
      --bundle --platform=node --format=esm \
      --banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" \
      --outdir=/out/agent-app \
    && printf '{"type":"module"}\n' > /out/agent-app/package.json \
    && mkdir -p /out/agent-app/assets \
    && cp -R ../../packages/llm/assets/builtin-skills /out/agent-app/assets/

# ─── 5a. web runtime ───────────────────────────────────────────────────
# Web is a trusted control plane. It runs agent runners (the bundle at
# /opt/openneko-agent); their turns run in `openneko serve`.
FROM runtime-base AS web
ARG OPENNEKO_VERSION=unknown
ARG OPENNEKO_COMMIT=unknown
ENV OPENNEKO_VERSION=$OPENNEKO_VERSION OPENNEKO_COMMIT=$OPENNEKO_COMMIT
LABEL org.opencontainers.image.revision=$OPENNEKO_COMMIT org.opencontainers.image.version=$OPENNEKO_VERSION
WORKDIR /app
# Writable HOME under /tmp so the entrypoint can materialize config on
# read-only container filesystems. PORT=8080 matches the common PaaS
# convention (Cloud Run, Heroku, Fly, Railway).
ENV NODE_ENV=production \
    PORT=8080 \
    HOSTNAME=0.0.0.0 \
    HOME=/tmp/openneko-home \
    XDG_CONFIG_HOME=/tmp/openneko-config \
    NEXT_TELEMETRY_DISABLED=1
RUN groupadd --system --gid 1001 neko \
    && useradd --system --create-home --uid 1001 --gid neko neko
# Shared agent-run folders. Fresh named volumes mounted here take this
# ownership: runs (setgid, group neko) are shared by the runner, serve and the
# sandbox's run users, who may pass through it (o+x) but not list it; /run/openneko holds serve's and the sandbox's sockets.
RUN install -d -o 1001 -g 1001 -m 2771 /var/lib/openneko/agent-runs \
    && install -d -o root -g root -m 0711 /var/lib/openneko/sandbox-homes \
    && install -d -o 1001 -g 1001 -m 0770 /run/openneko
RUN mkdir -p /config/openneko /config/graphjin /tmp/openneko-home /tmp/openneko-tmp \
    && chown -R neko:neko /config /tmp/openneko-home /tmp/openneko-tmp
# Standalone output is self-contained (server.js + traced node_modules).
# Static + public are served by server.js but not auto-copied — we copy
# them in alongside, matching the layout server.js expects.
COPY --from=web-deploy --chown=neko:neko /app/apps/web/.next/standalone ./
COPY --from=web-deploy --chown=neko:neko /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=web-deploy --chown=neko:neko /app/apps/web/public ./apps/web/public
# Next.js standalone tracing misses static asset dirs — copy explicitly.
COPY --from=web-deploy --chown=neko:neko /app/packages/llm/assets ./packages/llm/assets
COPY --chown=neko:neko entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
# Vendor the openneko Go binary so the entrypoint can run `openneko migrate`
# at boot (replaces the legacy neko-db-init container). Same binary as the
# worker image and the host install.
COPY --from=go-build --chown=neko:neko /out/openneko /usr/local/bin/openneko
RUN chmod +x /usr/local/bin/openneko
# The agent runner bundle; the launcher runs `node /opt/openneko-agent/entry.js`
# for each run.
COPY --from=agent-deploy /out/agent-app /opt/openneko-agent
RUN chmod -R a+rX /opt/openneko-agent
USER neko
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["node", "apps/web/server.js"]

# ─── 5b. worker runtime ────────────────────────────────────────────────
# Trimmed prod closure of @neko/worker: drops devDeps + other apps' sources +
# web/Next, keeps src + tsx + @neko/llm (with assets). Same
# mechanism as agent-deploy; rooted at /app, so the entry is /app/src/index.ts.
FROM source AS worker-deploy
ARG TARGETARCH
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    ONNXRUNTIME_NODE_INSTALL=skip \
      pnpm --filter @neko/worker deploy --prod /out/worker-app \
    && sh scripts/prune-node-runtime.sh /out/worker-app "$TARGETARCH"

# The worker runs from source via tsx (not a build step). It serves /health +
# admin endpoints on port 4100 for liveness probes. Records config validation
# and approved schema diff/sync require the shared pinned GraphJin CLI; this
# parent supplies it without inheriting the document toolchain.
FROM graphjin-node-runtime AS worker
ARG OPENNEKO_VERSION=unknown
ARG OPENNEKO_COMMIT=unknown
ENV OPENNEKO_VERSION=$OPENNEKO_VERSION OPENNEKO_COMMIT=$OPENNEKO_COMMIT
LABEL org.opencontainers.image.revision=$OPENNEKO_COMMIT org.opencontainers.image.version=$OPENNEKO_VERSION
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
      git openssh-client postgresql-client \
    && rm -rf /var/lib/apt/lists/*
ENV NODE_ENV=production \
    PORT=4100 \
    HOSTNAME=0.0.0.0 \
    HOME=/tmp/openneko-home \
    XDG_CONFIG_HOME=/tmp/openneko-config
RUN groupadd --system --gid 1001 neko \
    && useradd --system --create-home --uid 1001 --gid neko neko
# Shared agent-run folders. Fresh named volumes mounted here take this
# ownership: runs (setgid, group neko) are shared by the runner, serve and the
# sandbox's run users, who may pass through it (o+x) but not list it; /run/openneko holds serve's and the sandbox's sockets.
RUN install -d -o 1001 -g 1001 -m 2771 /var/lib/openneko/agent-runs \
    && install -d -o root -g root -m 0711 /var/lib/openneko/sandbox-homes \
    && install -d -o 1001 -g 1001 -m 0770 /run/openneko
# /cache is mounted by the demo-mode adventureworks-init step; pre-creating
# it here lets the named volume initialize with neko ownership instead of root
# (Docker copies image-side ownership into a fresh named volume on first mount).
RUN mkdir -p /config/openneko /config/graphjin /tmp/openneko-home /tmp/openneko-tmp /cache \
    && chown -R neko:neko /app /config /tmp/openneko-home /tmp/openneko-tmp /cache
# Prod closure (src + node_modules) rooted at /app, replacing the full pnpm
# workspace install + per-package source copies.
COPY --from=worker-deploy --chown=neko:neko /out/worker-app ./
# Whole db/ (not just migrations): seeds + load-adventureworks-baked.sh
# are needed for `openneko start --mode demo`'s adventureworks-init step.
COPY --chown=neko:neko db ./db
# Embedded first-party solution packs are loaded and validated by the worker.
COPY --chown=neko:neko packs ./packs
COPY --chown=neko:neko apps/worker/scripts/magento-v2-live-acceptance.ts ./scripts/magento-v2-live-acceptance.ts
COPY --chown=neko:neko entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
# Vendor the openneko Go binary so the agent's Bash tool inside the worker
# container can run `openneko install/secrets/pack …` without an
# extra install step. Same binary operators install on their host.
COPY --from=go-build --chown=neko:neko /out/openneko /usr/local/bin/openneko
RUN chmod +x /usr/local/bin/openneko
# The agent runner bundle; the launcher runs `node /opt/openneko-agent/entry.js`
# for each run.
COPY --from=agent-deploy /out/agent-app /opt/openneko-agent
RUN chmod -R a+rX /opt/openneko-agent
USER neko
# Fail the image build if a non-root user cannot read and boot the bundled
# runner without workspace node_modules.
RUN cd /opt/openneko-agent && node entry.js --preflight \
    && node --input-type=module -e "await import('./mcp-bridge.js')"
EXPOSE 4100
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["node", "--import", "tsx/esm", "src/index.ts"]

# ─── 5d. agent runtime: openneko serve and openneko sandbox ───────────
# One image for both services. `openneko serve` runs each agent turn and holds
# model keys only for that turn; `openneko sandbox` runs the agent's terminal
# commands as a per-turn user and holds no secrets. The document toolchain from
# `cli` is here because terminal commands run in this image. The openneko
# binary carries GraphJin's code but no GraphJin config or credentials.
FROM cli AS agent
USER root
RUN apt-get update && apt-get install -y --no-install-recommends jq ripgrep \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/ax-harness
COPY --from=duckdb-bin /usr/local/bin/duckdb /usr/local/bin/duckdb
RUN groupadd --system --gid 1001 neko \
    && useradd --system --create-home --uid 1001 --gid neko neko
# Shared agent-run folders. Fresh named volumes mounted here take this
# ownership: runs (setgid, group neko) are shared by the runner, serve and the
# sandbox's run users, who may pass through it (o+x) but not list it; /run/openneko holds serve's and the sandbox's sockets.
RUN install -d -o 1001 -g 1001 -m 2771 /var/lib/openneko/agent-runs \
    && install -d -o root -g root -m 0711 /var/lib/openneko/sandbox-homes \
    && install -d -o 1001 -g 1001 -m 0770 /run/openneko
# serve runs as neko by default. Compose runs the sandbox as root (user 0:0)
# with most capabilities dropped, so it can give each turn's commands their
# own user.
USER neko
CMD ["openneko", "serve"]

# ─── 5c. neko-cli runtime ──────────────────────────────────────────────
# Minimal image containing just the openneko Go binary. Used as the
# `neko-migrate` one-shot container in compose: starts, runs
# `openneko storage reconcile` and `openneko migrate`, then exits. Every
# database consumer is gated on those one-shots, so persisted indexes and the
# schema are both valid before application startup.
#
# The binary is static (CGO_ENABLED=0), so the distroless static image is the
# complete runtime. Its Debian CA bundle keeps managed-Postgres TLS working.
FROM gcr.io/distroless/static-debian12:latest AS neko-cli
COPY --from=go-build /out/openneko /openneko
ENTRYPOINT ["/openneko"]
CMD ["--help"]

# ─── 6. graphjin runtime ───────────────────────────────────────────────
# The openneko binary linked as graphjin, for CI config validation and the
# AdventureWorks eval stack.
FROM alpine:3.22 AS graphjin-runtime
RUN apk add --no-cache ca-certificates curl tini
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/graphjin && graphjin version
