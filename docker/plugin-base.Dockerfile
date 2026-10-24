# Shared OpenShell sandbox base for all @open-neko plugins. Each plugin's
# self-contained dist/run.js is uploaded to /sandbox/run.js at start, not baked.
#   docker build -f docker/plugin-base.Dockerfile -t ghcr.io/open-neko/plugin-base:node24 .
# Node 24: OpenShell injects NODE_USE_ENV_PROXY into every sandbox, but node only
# honors it (routing fetch through the egress proxy) on 24+. On older node the
# plugin's fetch dials direct, and DNS fails (proxy-only egress) → EAI_AGAIN.
FROM node:24.22.1-bookworm-slim AS node-distribution

FROM debian:bookworm-slim
COPY --from=node-distribution /usr/local/bin/node /usr/local/bin/node
COPY --from=node-distribution /usr/local/share/doc/node /usr/local/share/doc/node
RUN ln -s node /usr/local/bin/nodejs

# iproute2 + nftables: required by the supervisor's egress setup (without them
# the sandbox hangs at "waiting for supervisor relay").
RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 nftables \
    && rm -rf /var/lib/apt/lists/*

# Home MUST be /sandbox so `sandbox upload` (writes to ~) succeeds.
RUN groupadd -g 1000660000 sandbox \
    && useradd -u 1000660000 -g sandbox -d /sandbox -M sandbox \
    && install -d -o sandbox -g sandbox /sandbox

WORKDIR /sandbox
USER sandbox
CMD ["node", "--version"]
