// The agent-service HTTP boundary. The browser (slice-4 pi-web-ui)
// talks here; this owns the pi Agent + OAuth + gate; NO credentials
// client-side (pi-sdk PATTERN §2). Endpoints:
//
//   POST /chat    {prompt}        -> runTurn result (incl.
//                                    pendingApprovals[{id,name,args}])
//   POST /approve {id}            -> APPLIES the held proposal
//                                    server-side from its captured
//                                    args, deterministically — no
//                                    second LLM turn (D2/D6/D8 HITL)
//   GET  /healthz
//
// The held-proposal list is the confirm channel: the model can only
// propose (and is always blocked); an explicit human /approve {id}
// is what actually applies it, via the same curator client.
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

// applyProposal applies a held mutation deterministically from its
// CAPTURED args (D8) — never a second LLM turn. The agent exposes its
// curator client so this is the exact same transport the tool used.
async function applyProposal(client, p) {
  if (p.name === "propose_kb_entry") return client.proposeKbEntry(p.args);
  if (p.name === "put_doc_spec") return client.putDocSpec(p.args.id, p.args.edits);
  throw new Error(`unknown pending proposal '${p.name}'`);
}

export function createApp(deps = {}) {
  const webRoot = deps.webRoot || process.env.WEB_ROOT || null;

  // The Agent needs server-side OAuth (~/.pi/agent). Build it LAZILY
  // on first /chat so the SPA + /healthz serve credential-free; an
  // unauth deployment then fails only /chat, cleanly (502), instead
  // of crashing the whole service at startup.
  let agent = deps.agent || null;
  // A chat session counts as "started" once the first /chat is
  // handled. 409-vs-404 keys off THIS, not off `agent` being null:
  // tests legitimately inject deps.agent eagerly, so agent identity
  // is not a faithful "no session yet" signal (the D8 lesson — never
  // let a harness-injected object decide a contract branch).
  let started = false;
  const getAgent = () => {
    if (!agent) agent = createAgent({ ...deps.agentDeps });
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
      // D8: apply the held proposal server-side from captured args.
      if (!payload.id) return send(400, { error: "id required" });
      if (!started || !agent)
        return send(409, { error: "no chat session yet — nothing to approve" });
      const p = agent.takePending(payload.id);
      if (!p) return send(404, { error: `no pending proposal '${payload.id}'` });
      try {
        const result = await applyProposal(agent.client, p);
        return send(200, { applied: p.name, id: p.id, result });
      } catch (e) {
        return send(502, { error: String(e?.message || e), id: p.id });
      }
    }
    if (req.url === "/chat") {
      if (!payload.prompt) return send(400, { error: "prompt required" });
      started = true;
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
  return { server, getAgent };
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
