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
# node base gives npm for mmdc; mmdc drives a headless Chrome that
# mermaid-cli (puppeteer) pulls in. Chrome needs a set of system
# libraries the slim image lacks, and inside a container it must run
# with --no-sandbox — supplied via a puppeteer config file referenced
# by MMDC_PUPPETEER_CONFIG (honoured by the Go MermaidRenderer).
FROM node:20-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    fonts-liberation \
    libasound2 libatk-bridge2.0-0 libatk1.0-0 libcairo2 libcups2 \
    libdbus-1-3 libdrm2 libgbm1 libglib2.0-0 libnspr4 libnss3 \
    libpango-1.0-0 libx11-6 libxcb1 libxcomposite1 libxdamage1 \
    libxext6 libxfixes3 libxkbcommon0 libxrandr2 \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @mermaid-js/mermaid-cli@11.15.0 \
    && npx --yes puppeteer browsers install chrome-headless-shell

# Puppeteer cannot use its sandbox inside an unprivileged container;
# mermaid-cli reads these args via -p (the renderer passes it when
# MMDC_PUPPETEER_CONFIG is set).
RUN printf '{"args":["--no-sandbox","--disable-setuid-sandbox","--disable-dev-shm-usage"]}\n' \
      > /etc/mmdc-puppeteer.json
ENV MMDC_PUPPETEER_CONFIG=/etc/mmdc-puppeteer.json

COPY --from=build /out/mykb-curator /usr/local/bin/mykb-curator

# Config + spec/kb mounts are supplied at run time; the curator is a
# CLI (cobra) so the image has no default long-running process.
ENTRYPOINT ["/usr/local/bin/mykb-curator"]
CMD ["--help"]
