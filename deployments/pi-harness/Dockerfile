# Pi harness — runs `pi` inside a container with the pi-wrapper HTTP
# shim in front of it, exposing :8080 for the curator's PiClient to
# talk to.
#
# v0.0 sketch: real pi installation steps are intentionally TBD —
# pinned to the version of pi used by the user's kb-pi flow. The
# follow-up PR that wires the PiClient also finalises this Dockerfile.

# ---- builder ----
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -o /out/pi-wrapper ./cmd/pi-wrapper

# ---- runtime ----
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# TODO(v0.5): install pi here. The exact install steps depend on how
# pi is distributed (npm package? release binary? git clone + build?).
# For now, the container starts the wrapper which reports unhealthy
# until pi is reachable.

COPY --from=build /out/pi-wrapper /usr/local/bin/pi-wrapper

EXPOSE 8080
ENV PI_BINARY=pi

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/pi-wrapper"]
CMD ["--addr=:8080"]
