#!/usr/bin/env bash
# Run the agent-service test suite inside the reproducible pi image
# (host Node is < 22; SDK deps are baked in the image, never
# shell-installed — pi-sdk PATTERN §3 env rule). $0, offline: the
# faux provider + local HTTP stubs mean no network, no spend.
#
# Mounting at /pi/app lets Node resolve @earendil-works/* from the
# image's baked /pi/node_modules (walks up from /pi/app).
set -euo pipefail
cd "$(dirname "$0")"
exec docker run --rm \
  -v "$PWD:/pi/app:ro" \
  -e HOME=/home/node \
  pi-agent:0.74.1 \
  sh -c 'cd /pi/app && node --test'
