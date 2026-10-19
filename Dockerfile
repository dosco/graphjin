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

# Stateful Postgres data planes have an enforced storage ABI: PostgreSQL 16 on
# Debian Bookworm/glibc. Text indexes persist in named volumes, so switching
# their libc/collation provider (for example, to Alpine/musl) can make an
# existing btree silently disagree with equality lookups. Keep the base
# immutable. If its collation provider/version changes, bump the compiled
# StorageContractVersion so existing volumes are rebuilt exactly once. Records
# does not need pgvector, but sharing this exact base keeps the combined pull
# deduplicated.
FROM pgvector/pgvector:0.8.6-pg16-bookworm@sha256:ccc6e83d6e35e931dc7c5def2022729d5a6c370318d099181995567ff1fb4d6b AS postgres-runtime
LABEL org.openneko.storage-contract="1" \
      org.openneko.storage-owner="999:999"
USER root
# apt.postgresql.org keeps only current versions. The archive keeps the
# pinned pgBackRest build installable after newer releases replace it.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && echo "deb [ signed-by=/usr/local/share/keyrings/postgres.gpg.asc ] https://apt-archive.postgresql.org/pub/repos/apt bookworm-pgdg-archive main" \
      > /etc/apt/sources.list.d/pgdg-archive.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
      libssh2-1=1.10.0-3+b1 \
      pgbackrest=2.58.0-1.pgdg12+1 \
    && rm -rf /var/lib/apt/lists/* \
    && test "$(getconf GNU_LIBC_VERSION)" = "glibc 2.36" \
    && test "$(pg_config --version)" = "PostgreSQL 16.15 (Debian 16.15-1.pgdg12+2)" \
    && test "$(id -u postgres):$(id -g postgres)" = "999:999" \
    && pgbackrest version | grep -Fx 'pgBackRest 2.58.0'
COPY apps/worker/scripts/postgres-pgbackrest-entrypoint.sh /usr/local/bin/openneko-postgres-entrypoint
RUN chmod 0755 /usr/local/bin/openneko-postgres-entrypoint
ENTRYPOINT ["/usr/local/bin/openneko-postgres-entrypoint"]
CMD ["postgres"]

FROM postgres-runtime AS neko-db
FROM postgres-runtime AS records-db

# The backup coordinator mounts PostgreSQL's 0700 data directories and reads
# 0600 control files as the postgres OS user. Inheriting the exact database
# runtime makes that numeric identity, PostgreSQL toolchain, libc, and
# pgBackRest version one enforced storage ABI instead of duplicating them in a
# distribution whose postgres account has a different UID.
FROM postgres-runtime AS neko-backup
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      curl \
      python3 \
    && rm -rf /var/lib/apt/lists/* \
    && test "$(id -u postgres):$(id -g postgres)" = "999:999" \
    && test "$(python3 --version)" = "Python 3.11.2" \
    && pgbackrest version | grep -Fx 'pgBackRest 2.58.0'
COPY apps/worker/scripts/openneko-backup.py /usr/local/bin/openneko-backup.py
COPY apps/worker/scripts/openneko-backup-entrypoint.sh /usr/local/bin/openneko-backup-entrypoint
RUN chmod 0755 /usr/local/bin/openneko-backup.py /usr/local/bin/openneko-backup-entrypoint
EXPOSE 9470
ENTRYPOINT ["/usr/local/bin/openneko-backup-entrypoint"]
CMD ["serve"]

# Pinned static OpenShell client shared by control planes and the readiness
# one-shot without pulling a Node/worker filesystem into the latter.
FROM debian:bookworm-slim AS openshell-bin
ARG OPENSHELL_VERSION=0.1.2
ARG OPENSHELL_ASSET_AMD64=594438336
ARG OPENSHELL_ASSET_ARM64=594438335
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && OPENSHELL_ASSET_ID="$(case "${TARGETARCH}" in amd64) echo "${OPENSHELL_ASSET_AMD64}" ;; arm64) echo "${OPENSHELL_ASSET_ARM64}" ;; *) exit 1 ;; esac)" \
    && curl -fsSL --retry 10 --retry-delay 5 --retry-all-errors -o /tmp/openshell.tgz \
      -H 'Accept: application/octet-stream' \
      "https://api.github.com/repos/NVIDIA/OpenShell/releases/assets/${OPENSHELL_ASSET_ID}" \
    && tar -xzf /tmp/openshell.tgz -C /usr/local/bin openshell \
    && rm /tmp/openshell.tgz \
    && rm -rf /var/lib/apt/lists/* \
    && test "$(openshell --version)" = "openshell ${OPENSHELL_VERSION}"

FROM alpine:3.22 AS openshell-ready
RUN apk add --no-cache ca-certificates
COPY --from=openshell-bin /usr/local/bin/openshell /usr/local/bin/openshell
CMD ["openshell", "--version"]

# ─── 2. control-plane runtime: sandbox launcher only ───────────────────
# Web is a trusted control plane that launches OpenShell sandboxes but never
# contains an agent runtime, GraphJin CLI, or document toolchain. The worker
# uses the narrower shared GraphJin/npm runtime defined below.
FROM node-runtime AS runtime-base
# Git supports trusted config VCS and git-backed installs. openssh-client is
# required by `openshell sandbox exec`.
RUN apt-get update && apt-get install -y --no-install-recommends \
      git openssh-client \
    && rm -rf /var/lib/apt/lists/*
COPY --from=openshell-bin /usr/local/bin/openshell /usr/local/bin/openshell

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
# configuration. It is never part of the sandbox agent image.
FROM npm-runtime AS graphjin-node-runtime
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/graphjin && graphjin version

# ─── 2b. agent runtime: Hermes (sandbox only) ──────────────────────────
FROM npm-runtime AS agent-base
ARG HERMES_AGENT_REF=29112bef099274229cadff79cdff7bf7b99c4b77
RUN apt-get update && apt-get install -y --no-install-recommends \
      git patch python3 python3-pip \
    && rm -rf /var/lib/apt/lists/*
# Hermes uses Debian's system Python instead of downloading a second Python
# distribution. Hermes 0.21 intentionally blocks wheel builds, so install its
# exact source revision into an isolated editable environment (the supported
# upstream layout). Keep checkout, patching, dependency installation, and
# pruning in one layer: deleting upstream development assets in a later layer
# would leave their bytes in the compressed image. OpenNeko supplies
# HERMES_HOME/skills for every run, so the upstream development, desktop, web,
# evaluation, and optional-catalog trees are outside the curated ACP runtime.
COPY scripts/patches/hermes-acp-reasoning-config.patch /tmp/hermes-acp-reasoning-config.patch
COPY scripts/patches/hermes-acp-interim-messages.patch /tmp/hermes-acp-interim-messages.patch
COPY scripts/patches/hermes-acp-anthropic-reasoning.patch /tmp/hermes-acp-anthropic-reasoning.patch
COPY scripts/patches/hermes-acp-native-delegation-policy.patch /tmp/hermes-acp-native-delegation-policy.patch
COPY scripts/patches/hermes-acp-tool-usage.patch /tmp/hermes-acp-tool-usage.patch
COPY scripts/patches/hermes-acp-mcp-output.patch /tmp/hermes-acp-mcp-output.patch
COPY scripts/patches/hermes-acp-cost.patch /tmp/hermes-acp-cost.patch
COPY scripts/patches/hermes-acp-run-budget.patch /tmp/hermes-acp-run-budget.patch
COPY scripts/test-hermes-acp-tool-usage.py /tmp/test-hermes-acp-tool-usage.py
COPY scripts/test-hermes-acp-cost.py /tmp/test-hermes-acp-cost.py
RUN --mount=type=cache,id=hermes-uv,target=/tmp/uv-cache \
    curl -LsSf --retry 5 --retry-delay 5 --retry-all-errors https://astral.sh/uv/install.sh \
      | env UV_INSTALL_DIR=/usr/local/bin UV_NO_MODIFY_PATH=1 sh \
    && mkdir -p /usr/local/lib/hermes-agent \
    && git -C /usr/local/lib/hermes-agent init \
    && git -C /usr/local/lib/hermes-agent remote add origin https://github.com/NousResearch/hermes-agent.git \
    && git -C /usr/local/lib/hermes-agent fetch --depth 1 origin "$HERMES_AGENT_REF" \
    && git -C /usr/local/lib/hermes-agent checkout --detach FETCH_HEAD \
    && test "$(git -C /usr/local/lib/hermes-agent rev-parse HEAD)" = "$HERMES_AGENT_REF" \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-reasoning-config.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-interim-messages.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-anthropic-reasoning.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-native-delegation-policy.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-tool-usage.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-mcp-output.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-cost.patch \
    && patch --batch --forward --fuzz=0 -d /usr/local/lib/hermes-agent -p1 < /tmp/hermes-acp-run-budget.patch \
    && rm /tmp/hermes-acp-run-budget.patch /tmp/hermes-acp-cost.patch /tmp/hermes-acp-mcp-output.patch /tmp/hermes-acp-tool-usage.patch /tmp/hermes-acp-reasoning-config.patch /tmp/hermes-acp-interim-messages.patch /tmp/hermes-acp-anthropic-reasoning.patch /tmp/hermes-acp-native-delegation-policy.patch \
    && cd /usr/local/lib/hermes-agent \
    && UV_PROJECT_ENVIRONMENT=/usr/local/uv/tools/hermes-agent \
       UV_CACHE_DIR=/tmp/uv-cache \
       uv sync --locked --no-dev --extra acp --extra mcp --extra anthropic \
    && ln -sf /usr/local/uv/tools/hermes-agent/bin/hermes /usr/local/bin/hermes \
    && rm -rf /root/.cache/uv \
    && hermes --version \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "import hermes_cli; assert hermes_cli.__version__ == '0.21.0'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "import openai; assert openai.__version__, 'Hermes OpenAI provider SDK missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "import anthropic; assert anthropic.__version__, 'Hermes Anthropic provider SDK missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from mcp.types import CallToolResult; import websockets; result = CallToolResult(content=[]); assert hasattr(result, 'is_error')" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.session import SessionManager; import inspect; source = inspect.getsource(SessionManager._make_agent); assert 'reasoning_config' in source and 'resolve_reasoning_config' in source, 'Hermes ACP must pass configured reasoning into AIAgent'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.server import HermesACPAgent; import inspect; source = inspect.getsource(HermesACPAgent.prompt); assert 'usage=usage' in source, 'Hermes ACP prompt response must expose exact turn usage'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.events import make_interim_message_cb; from acp_adapter.server import HermesACPAgent; import inspect; source = inspect.getsource(HermesACPAgent.prompt); assert 'interim_assistant_callback' in source and 'pending_streamed_message.append(text)' in source and 'raw_interim_cb(text, already_streamed=False)' in source and 'not streamed_message' not in source; assert callable(make_interim_message_cb), 'Hermes ACP buffered interim callback missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from agent import chat_completion_helpers; from pathlib import Path; source = Path(chat_completion_helpers.__file__).read_text(); assert '_emit_unstreamed_anthropic_reasoning' in source and 'reasoning_was_streamed' in source, 'Hermes ACP Anthropic reasoning fallback missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.session import _openneko_disabled_toolsets; import os; os.environ['OPENNEKO_HERMES_NATIVE_DELEGATION']='disabled'; assert _openneko_disabled_toolsets() == ['delegation'], 'Hermes ACP native delegation policy missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.session import _openneko_run_budget; import os; os.environ['OPENNEKO_HERMES_REASONING_EFFORT']='medium'; os.environ['OPENNEKO_HERMES_MAX_ITERATIONS']='60'; b = _openneko_run_budget(); assert b['max_iterations'] == 60 and b['reasoning_config'], 'Hermes ACP run budget missing'" \
    && /usr/local/uv/tools/hermes-agent/bin/python /tmp/test-hermes-acp-tool-usage.py \
    && /usr/local/uv/tools/hermes-agent/bin/python /tmp/test-hermes-acp-cost.py \
    && rm /tmp/test-hermes-acp-tool-usage.py /tmp/test-hermes-acp-cost.py \
    && rm -rf \
      /usr/local/lib/hermes-agent/.git \
      /usr/local/lib/hermes-agent/.github \
      /usr/local/lib/hermes-agent/apps \
      /usr/local/lib/hermes-agent/contributors \
      /usr/local/lib/hermes-agent/docs \
      /usr/local/lib/hermes-agent/evals \
      /usr/local/lib/hermes-agent/native \
      /usr/local/lib/hermes-agent/nix \
      /usr/local/lib/hermes-agent/optional-skills \
      /usr/local/lib/hermes-agent/scripts \
      /usr/local/lib/hermes-agent/tests \
      /usr/local/lib/hermes-agent/ui-tui \
      /usr/local/lib/hermes-agent/web \
      /usr/local/lib/hermes-agent/website \
    && find /usr/local/lib/hermes-agent /usr/local/uv/tools/hermes-agent \
      -type d -name __pycache__ -prune -exec rm -rf '{}' + \
    && test ! -e /usr/local/lib/hermes-agent/tests \
    && test ! -e /usr/local/lib/hermes-agent/apps \
    && test ! -e /usr/local/lib/hermes-agent/website \
    && PYTHONDONTWRITEBYTECODE=1 hermes --version \
    && PYTHONDONTWRITEBYTECODE=1 /usr/local/uv/tools/hermes-agent/bin/python -c "from acp_adapter.server import HermesACPAgent; import toolsets; assert HermesACPAgent and toolsets" \
    && echo "hermes v0.21 ACP/MCP runtime present"

# ─── 2c. document toolchain (agent image only) ─────────────────────────
# Bundled skills (xlsx / pptx / docx / pdf / document-extraction) shell out to
# Python + LibreOffice + Poppler / qpdf / Tesseract via the agent's Bash inside
# the OpenShell sandbox — web never runs these, so the ~1GB toolchain stays out
# of its image (the worker gets a minimal subset below for the librarian).
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
COPY packages/evals/package.json packages/evals/package.json
COPY packages/interaction/package.json packages/interaction/package.json
COPY packages/llm/package.json packages/llm/package.json
COPY packages/packs/package.json packages/packs/package.json
COPY packages/plugin-install/package.json packages/plugin-install/package.json
COPY packages/plugin-types/package.json packages/plugin-types/package.json
COPY packages/records/package.json packages/records/package.json
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

# ─── 5a. web runtime ───────────────────────────────────────────────────
# Web remains a trusted OpenShell control plane; the agent runtime is not here.
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
RUN useradd --system --create-home --uid 1001 neko
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
# Blueprint JSON is read through the trusted records control plane at runtime;
# Next's file tracer cannot discover fs-relative assets automatically.
COPY --from=web-deploy --chown=neko:neko /app/packages/records/blueprints ./packages/records/blueprints
COPY --chown=neko:neko entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
# Vendor the openneko Go binary so the entrypoint can run `openneko migrate`
# at boot (replaces the legacy neko-db-init container). Same binary as the
# worker image and the host install.
COPY --from=go-build --chown=neko:neko /out/openneko /usr/local/bin/openneko
RUN chmod +x /usr/local/bin/openneko
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
# parent supplies it without inheriting Hermes or the document toolchain.
FROM graphjin-node-runtime AS worker
ARG OPENNEKO_VERSION=unknown
ARG OPENNEKO_COMMIT=unknown
ENV OPENNEKO_VERSION=$OPENNEKO_VERSION OPENNEKO_COMMIT=$OPENNEKO_COMMIT
LABEL org.opencontainers.image.revision=$OPENNEKO_COMMIT org.opencontainers.image.version=$OPENNEKO_VERSION
WORKDIR /app
# Minimal extraction toolchain for the library distiller ("the librarian"),
# which shells out to the bundled document-extraction script on this host:
# python3 covers docx/pptx/xlsx via stdlib zipfile fallbacks, pdftotext
# (poppler-utils) covers PDFs. Deliberately no pip deps and no tesseract —
# scanned-PDF OCR runs in the agent image; a worker-side extraction miss
# fails the document row with a clear reason and is retryable from /library.
# Hermes remains agent-only. The zero-payload interpreter marker lets the
# worker resolve the agent's exact /proc/<pid>/exe identity for OpenShell
# egress without reinstalling the Hermes tool or virtual environment here.
RUN apt-get update && apt-get install -y --no-install-recommends \
      git openssh-client python3 poppler-utils unzip postgresql-client \
    && rm -rf /var/lib/apt/lists/* \
    && install -d /usr/local/uv/tools/hermes-agent/bin \
    && ln -s /usr/bin/python3 /usr/local/uv/tools/hermes-agent/bin/python
COPY --from=openshell-bin /usr/local/bin/openshell /usr/local/bin/openshell
ENV NODE_ENV=production \
    PORT=4100 \
    HOSTNAME=0.0.0.0 \
    HOME=/tmp/openneko-home \
    XDG_CONFIG_HOME=/tmp/openneko-config
RUN useradd --system --create-home --uid 1001 neko
# /cache is mounted by the demo-mode adventureworks-init step; pre-creating
# it here lets the named volume initialize with neko ownership instead of root
# (Docker copies image-side ownership into a fresh named volume on first mount).
# /app must be writable by neko so `openneko install` (run in-container via
# docker exec) can write the plugin manifest during installs.
RUN mkdir -p /config/openneko /config/graphjin /tmp/openneko-home /tmp/openneko-tmp /cache /var/lib/openneko/plugins \
    && chown -R neko:neko /app /config /tmp/openneko-home /tmp/openneko-tmp /cache /var/lib/openneko
# Seed the plugin install dir with an empty package.json so `npm install`
# inside that dir has a workspace to operate on. Isolated from /app's
# node_modules (OPENNEKO_PLUGIN_INSTALL_DIR points here), so plugin packages
# land cleanly regardless of the worker's own (pruned) deps.
RUN printf '{\n  "name": "openneko-plugins",\n  "version": "0.0.0",\n  "private": true\n}\n' > /var/lib/openneko/plugins/package.json \
    && chown neko:neko /var/lib/openneko/plugins/package.json
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
# container can run `openneko install/secrets/marketplace …` without an
# extra install step. Same binary operators install on their host.
COPY --from=go-build --chown=neko:neko /out/openneko /usr/local/bin/openneko
RUN chmod +x /usr/local/bin/openneko
USER neko
EXPOSE 4100
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["node", "--import", "tsx/esm", "src/index.ts"]

# ─── 5d. agent sandbox runtime (OpenShell) ─────────────────────────────
# Compile the sandbox-only entry and multiplexed bridge. Ship their assets,
# not the worker's dependency closure (DB, ONNX, transformers, tsx).
FROM source AS agent-deploy
RUN cd apps/worker && pnpm exec esbuild \
      src/agent-sandbox/entry.ts src/agent-sandbox/mcp-bridge.ts \
      --bundle --platform=node --format=esm \
      --banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" \
      --outdir=/out/agent-app \
    && printf '{"type":"module"}\n' > /out/agent-app/package.json \
    && mkdir -p /out/agent-app/assets \
    && cp -R ../../packages/llm/assets/builtin-skills /out/agent-app/assets/

FROM cli AS agent
USER root
# Late-bound warm slots must observe policy/provider revisions promptly.
ENV OPENSHELL_POLICY_POLL_INTERVAL_SECS=1
# OpenNeko pre-installs the ACP/MCP feature set. Never let a sandbox spend its
# startup budget trying a lazy install through the restricted egress policy.
ENV HERMES_DISABLE_LAZY_INSTALLS=1
# Supervisor egress-netns tools + a non-root `sandbox` user (high UID, OpenShell
# convention). The openneko binary here carries GraphJin's code but no GraphJin
# config or credentials; the agent reaches GraphJin only through the
# authenticated host broker.
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 nftables jq ripgrep \
    && rm -rf /var/lib/apt/lists/*
# The Ax harness is the openneko binary's ax-harness role.
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/ax-harness
COPY --from=duckdb-bin /usr/local/bin/duckdb /usr/local/bin/duckdb
RUN groupadd -g 1000660000 sandbox \
    && useradd -u 1000660000 -g sandbox -d /sandbox -M sandbox \
    && install -d -o sandbox -g sandbox /sandbox
# Only the two bundles and built-in skills enter the agent filesystem.
COPY --from=agent-deploy --chown=1000660000:1000660000 /out/agent-app /app
COPY --chown=1000660000:1000660000 apps/worker/src/agent-sandbox/hermes-warm.py /app/hermes-warm.py
# OpenShell may execute as a remapped non-root UID. Build output can inherit
# restrictive source modes, so every runtime bundle and parent directory must
# be readable regardless of the sandbox's assigned UID.
RUN chmod -R a+rX /app
USER sandbox
# Fail the image build if the non-root sandbox cannot read and boot the
# bundled runtime without workspace node_modules.
RUN cd /app && node entry.js --preflight \
    && node --input-type=module -e "await import('./mcp-bridge.js')"
WORKDIR /sandbox
# Supervisor-replaced; launcher runs: cd /app && node /app/entry.js
CMD ["node", "--version"]

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

# ─── 6. neko-graphjin runtime ──────────────────────────────────────────
# OpenNeko's own GraphJin instance — exposes the metadata Postgres
# (workflow_definition, workflow_run, workflow_output, observation,
# subscription, action_*) so the worker can subscribe to output-match
# firings and dogfood query features. Distinct from the customer-data
# graphjin in compose.adventureworks.yml.
#
# The entrypoint re-templates db/graphjin/neko.yml from the openneko
# config.json on every start, so password rotation via /setup just
# requires `docker compose restart neko-graphjin`. The credential templater is
# a small static Go binary; Node is not present in either GraphJin image.
FROM alpine:3.22 AS graphjin-runtime
RUN apk add --no-cache ca-certificates curl tini
COPY --from=go-build /out/openneko /usr/local/bin/openneko
RUN ln -s openneko /usr/local/bin/graphjin && graphjin version

FROM graphjin-runtime AS neko-graphjin
COPY scripts/neko-graphjin-entrypoint.sh /usr/local/bin/neko-graphjin-entrypoint.sh
RUN chmod +x /usr/local/bin/neko-graphjin-entrypoint.sh
COPY db/graphjin/neko.yml /seed/neko.yml
EXPOSE 8089
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/neko-graphjin-entrypoint.sh"]
CMD ["serve", "--path", "/config"]

# ─── 7. records-graphjin runtime ───────────────────────────────────────
# The generated-app data plane has a separate process and a separate complete
# config writer. Reuse the pinned GraphJin binary layer, but never inherit the
# metadata GraphJin templating path. The entrypoint refuses to serve until the
# worker has projected an exhaustive live-catalog RBAC config.
FROM neko-graphjin AS records-graphjin
COPY scripts/records-graphjin-entrypoint.sh /usr/local/bin/records-graphjin-entrypoint.sh
RUN chmod +x /usr/local/bin/records-graphjin-entrypoint.sh
EXPOSE 8090
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/records-graphjin-entrypoint.sh"]
CMD ["serve", "--path", "/config"]
