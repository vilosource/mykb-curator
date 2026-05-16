# Pi harness — runs `pi` inside a container behind the pi-wrapper
# HTTP shim, exposing :8080 for the curator's PiClient.
#
# pi is distributed as the npm package @mariozechner/pi-coding-agent
# (binary: `pi`). Pinned to 0.66.1 — the version of the operator's
# kb-pi flow at the time the PiClient landed; bump deliberately and
# re-record the contract fixture (docs/pi-harness-contract.md) on
# upgrade, since the JSONL event schema is a contract.

# ---- builder ----
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -o /out/pi-wrapper ./cmd/pi-wrapper

# ---- runtime ----
# node:20 base gives us npm for the pi install. curl is for the
# healthcheck.
FROM node:20-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @mariozechner/pi-coding-agent@0.66.1

# Build-time verify the pi install (vafi images/pi lesson: fail the
# build early, never ship an image whose pi is broken).
RUN pi --version

COPY --from=build /out/pi-wrapper /usr/local/bin/pi-wrapper

EXPOSE 8080
ENV PI_BINARY=pi

# This image is deliberately credential-free. Auth + provider come
# from a bind-mounted ~/.pi/agent (settings.json selects the
# provider/model — kb-pi uses github-copilot / claude-opus-4-7 with
# a Copilot OAuth token in auth.json that pi auto-refreshes; the
# mount must be read-write so the rotated token persists). See
# deployments/personal-wiki/docker-compose.yml (personal-pi). An
# env API key (-e ANTHROPIC_API_KEY=…) still works for the
# disposable harness but is NOT the kb-pi-parity path.
#
# The wrapper invokes pi --no-tools --no-session --no-extensions
# --no-skills (see docs/pi-harness-contract.md): no kb extension or
# skills load — a plain LLM completion, not the kb-pi agent, but
# pointed at the same provider + credentials.

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/pi-wrapper"]
CMD ["--addr=:8080"]
