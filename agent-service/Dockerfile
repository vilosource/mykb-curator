# Production image seed for the spec-chat agent-service.
#
# FROM the proven pi env image (pi-sdk pattern/scaffold/Dockerfile =
# spikes/07-stable-env): Node 22, @earendil-works/*@0.74.1 baked at
# build, public registry — NEVER shell-`npm install` the SDK
# (pi-sdk PATTERN §3 env rule). This service adds no npm deps of its
# own (pure stdlib + the baked SDK), so there is nothing to install:
# we only copy app code on top of the baked module tree.
FROM pi-agent:0.74.1

# App lives under /pi so Node resolves @earendil-works/* from the
# image's baked /pi/node_modules (resolution walks up from /pi/app).
WORKDIR /pi/app
COPY src ./src
COPY package.json ./

USER node
ENV HOME=/home/node
# Auth (~/.pi/agent Copilot OAuth) is bind-mounted at run, server-side
# only (PATTERN §2). Bind localhost; the browser reaches this via the
# slice-4 pi-web-ui, never with credentials.
EXPOSE 4774
CMD ["node", "src/server.mjs"]
