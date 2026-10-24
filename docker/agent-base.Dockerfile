# OpenShell sandbox OS base for the Hermes agent runtime. Agent turns reach the
# trusted control plane only through the broker.
#
# This file is the OS base layer (the OpenShell-required shape: glibc +
# iproute2/nftables + a `sandbox` user @ /sandbox, same as plugin-base). The
# runnable agent image in the main Dockerfile adds Hermes, GraphJin, document
# tooling, two standalone JavaScript bundles, and built-in skill assets. It does
# not inherit the worker source or production dependency closure.
# See docs/OPENSHELL_MIGRATION_PLAN.md (Phase 2c) for the full image + launcher.
#   docker build -f docker/agent-base.Dockerfile -t ghcr.io/open-neko/agent-base:node24 .
# Node 24 is retained for the standalone agent/MCP bundles and proxy-aware fetch.
FROM node:24.22.1-bookworm-slim AS node-distribution

FROM node-distribution AS npm-payload
RUN rm -rf /usr/local/lib/node_modules/npm/docs /usr/local/lib/node_modules/npm/man \
    && find /usr/local/lib/node_modules/npm -type f -name '*.map' -delete

FROM debian:bookworm-slim
COPY --from=node-distribution /usr/local/bin/node /usr/local/bin/node
COPY --from=node-distribution /usr/local/share/doc/node /usr/local/share/doc/node
COPY --from=npm-payload /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s node /usr/local/bin/nodejs \
    && ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates iproute2 nftables \
    && rm -rf /var/lib/apt/lists/* \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' ca-certificates)" ge 20250419~deb12u2

RUN groupadd -g 1000660000 sandbox \
    && useradd -u 1000660000 -g sandbox -d /sandbox -M sandbox \
    && install -d -o sandbox -g sandbox /sandbox

WORKDIR /sandbox
USER sandbox
CMD ["node", "--version"]
