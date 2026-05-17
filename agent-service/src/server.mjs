// The agent-service HTTP boundary. The browser (slice-4 pi-web-ui)
// talks here; this owns the pi Agent + OAuth + gate; NO credentials
// client-side (pi-sdk PATTERN §2). Endpoints:
//
//   POST /chat    {prompt}            -> runTurn result (incl.
//                                        pendingApprovals for the UI)
//   POST /approve  {name, args}       -> records an out-of-band human
//                                        ACK so the gate lets that
//                                        exact mutation through next
//                                        call (design D2/D6 HITL)
//   GET  /healthz
//
// The approvals store is the confirm channel the gate consumes: the
// model cannot write to it — only an explicit human /approve can.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { join, resolve, relative, isAbsolute, extname } from "node:path";
import { createAgent } from "./agent.mjs";

const MIME = {
  ".html": "text/html", ".js": "text/javascript", ".mjs": "text/javascript",
  ".css": "text/css", ".svg": "image/svg+xml", ".json": "application/json",
  ".woff2": "font/woff2", ".png": "image/png", ".ico": "image/x-icon",
};

// serveStatic serves the built SPA same-origin with the API so the
// browser holds no cross-origin creds (D1/D7). Path-traversal-guarded;
// SPA fallback to index.html.
async function serveStatic(webRoot, urlPath, res) {
  let decoded;
  try {
    decoded = decodeURIComponent(urlPath.split("?")[0]);
  } catch {
    res.writeHead(400).end("bad path");
    return;
  }
  const rel = decoded === "/" ? "/index.html" : decoded;
  const rootAbs = resolve(webRoot);
  const target = resolve(rootAbs, "." + rel);
  // Reject anything resolving outside webRoot (real containment, not
  // a substring heuristic — normalize() collapses leading "..").
  const r = relative(rootAbs, target);
  if (r.startsWith("..") || isAbsolute(r)) {
    res.writeHead(400).end("bad path");
    return;
  }
  for (const candidate of [target, join(rootAbs, "index.html")]) {
    try {
      const buf = await readFile(candidate);
      res.writeHead(200, { "content-type": MIME[extname(candidate)] || "application/octet-stream" });
      res.end(buf);
      return;
    } catch {
      /* try fallback */
    }
  }
  res.writeHead(404).end("not found");
}

export function makeApprovals() {
  const set = new Set();
  const key = (name, args) => name + "::" + JSON.stringify(args ?? {});
  return {
    approve: (name, args) => set.add(key(name, args)),
    isApproved: (name, args) => set.has(key(name, args)),
    _size: () => set.size,
  };
}

export function createApp(deps = {}) {
  const approvals = deps.approvals || makeApprovals();
  const webRoot = deps.webRoot || process.env.WEB_ROOT || null;

  // The Agent needs server-side OAuth (~/.pi/agent). Build it LAZILY
  // on first /chat so the SPA + /healthz serve credential-free; an
  // unauth deployment then fails only /chat, cleanly (502), instead
  // of crashing the whole service at startup.
  let agent = deps.agent || null;
  const getAgent = () => {
    if (!agent) agent = createAgent({ approvals, ...deps.agentDeps });
    return agent;
  };

  async function handle(req, res) {
    const send = (code, obj) => {
      res.writeHead(code, { "content-type": "application/json" });
      res.end(JSON.stringify(obj));
    };
    if (req.method === "GET" && req.url === "/healthz") return send(200, { ok: true });

    // Non-API GETs => the SPA (same-origin with /chat).
    if (req.method === "GET" && webRoot && !req.url.startsWith("/chat") && !req.url.startsWith("/approve")) {
      return serveStatic(webRoot, req.url, res);
    }

    if (req.method !== "POST") return send(405, { error: "POST required" });
    let body = "";
    req.on("data", (c) => (body += c));
    await new Promise((r) => req.on("end", r));
    let payload;
    try {
      payload = body ? JSON.parse(body) : {};
    } catch {
      return send(400, { error: "bad json" });
    }

    if (req.url === "/approve") {
      if (!payload.name) return send(400, { error: "name required" });
      approvals.approve(payload.name, payload.args);
      return send(200, { approved: payload.name });
    }
    if (req.url === "/chat") {
      if (!payload.prompt) return send(400, { error: "prompt required" });
      try {
        const result = await getAgent().runTurn(payload.prompt);
        return send(200, result);
      } catch (e) {
        // Includes the unauth case (no ~/.pi/agent OAuth) — clean
        // 502, the SPA stays up.
        return send(502, { error: String(e?.message || e) });
      }
    }
    return send(404, { error: "no route" });
  }

  const server = createServer((req, res) => {
    handle(req, res).catch((e) => {
      if (!res.headersSent) res.writeHead(500, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: String(e?.message || e) }));
    });
  });
  return { server, approvals, getAgent };
}

// Entrypoint (the Docker CMD).
if (import.meta.url === `file://${process.argv[1]}`) {
  const port = Number(process.env.PORT || 4774);
  // Default 127.0.0.1 (D1: never publicly exposed). In a container
  // set HOST=0.0.0.0 and bind the host map to 127.0.0.1:<port> so the
  // user's browser reaches the SPA while the port stays loopback-only.
  const host = process.env.HOST || "127.0.0.1";
  const { server } = createApp();
  server.listen(port, host, () =>
    console.error(`spec-chat agent-service on ${host}:${port}`)
  );
}
