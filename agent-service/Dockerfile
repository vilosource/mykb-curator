# Production image for the spec-chat agent-service + its browser SPA.
#
# Stage 1 builds the pi-web-ui SPA (vite7 needs Node >= 20.19; the
# host is 20.18 — never build on host). Deps installed HERE at build
# from the public registry (pi-sdk PATTERN §3 env rule: bake, never
# shell-install at runtime).
FROM node:22-bookworm-slim AS web
WORKDIR /web
RUN npm config set registry https://registry.npmjs.org
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build   # -> /web/dist (vite build; the spike-04 "build-verified" bar)

# Stage 2: the service, FROM the proven pi env image (Node 22 +
# @earendil-works/*@0.74.1 baked). App under /pi so Node resolves the
# baked /pi/node_modules.
FROM pi-agent:0.74.1
WORKDIR /pi/app
COPY src ./src
COPY package.json ./
COPY --from=web /web/dist ./web/dist
ENV WEB_ROOT=/pi/app/web/dist
USER node
ENV HOME=/home/node
# Auth (~/.pi/agent Copilot OAuth) bind-mounted at run, server-side
# only (PATTERN §2). SPA + API same-origin; no creds client-side.
EXPOSE 4774
CMD ["node", "src/server.mjs"]
