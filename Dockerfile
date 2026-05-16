# mykb-curator production/runtime image.
#
# Multi-stage: a Go build of the curator binary, then a Node runtime
# that also ships `mmdc` (@mermaid-js/mermaid-cli) so the
# RenderDiagrams pass's MermaidRenderer has its subprocess available
# (DESIGN §16: "Curator container ships with mmdc"). This is the
# image the harness/experiment loop and eventual deployment use.

# ---- builder ----
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -o /out/mykb-curator ./cmd/mykb-curator

# ---- runtime ----
# node base gives npm for mmdc; mmdc itself drives a headless
# Chromium that mermaid-cli pulls in.
FROM node:20-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @mermaid-js/mermaid-cli@11.15.0 \
    && npx --yes puppeteer browsers install chrome-headless-shell || true

COPY --from=build /out/mykb-curator /usr/local/bin/mykb-curator

# Config + spec/kb mounts are supplied at run time; the curator is a
# CLI (cobra) so the image has no default long-running process.
ENTRYPOINT ["/usr/local/bin/mykb-curator"]
CMD ["--help"]
