# OpenShell sandbox supervisor that checks for policy changes every second.
# The upstream default of 10 s made each warm sandbox wait up to 10 s for its
# run policy.
ARG OPENSHELL_VERSION=0.1.2
FROM ghcr.io/nvidia/openshell/supervisor:${OPENSHELL_VERSION}
ENV OPENSHELL_POLICY_POLL_INTERVAL_SECS=1
