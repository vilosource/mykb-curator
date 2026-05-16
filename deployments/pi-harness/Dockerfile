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

COPY --from=build /out/pi-wrapper /usr/local/bin/pi-wrapper

EXPOSE 8080
ENV PI_BINARY=pi

# Pi auth (provider key) is supplied at run time via env, e.g.
#   -e ANTHROPIC_API_KEY=...  or  -e GEMINI_API_KEY=...
# The wrapper invokes pi --no-tools --no-session --no-extensions
# --no-skills (see docs/pi-harness-contract.md), so no kb extension
# or skills are loaded — this is a plain LLM, not the kb-pi agent.

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/pi-wrapper"]
CMD ["--addr=:8080"]
