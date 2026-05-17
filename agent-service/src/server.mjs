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
import { createAgent } from "./agent.mjs";

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
  const agent = deps.agent || createAgent({ approvals, ...deps.agentDeps });

  async function handle(req, res) {
    const send = (code, obj) => {
      res.writeHead(code, { "content-type": "application/json" });
      res.end(JSON.stringify(obj));
    };
    if (req.method === "GET" && req.url === "/healthz") return send(200, { ok: true });

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
        const result = await agent.runTurn(payload.prompt);
        return send(200, result);
      } catch (e) {
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
  return { server, approvals, agent };
}

// Entrypoint (the Docker CMD).
if (import.meta.url === `file://${process.argv[1]}`) {
  const port = Number(process.env.PORT || 4774);
  const { server } = createApp();
  server.listen(port, "127.0.0.1", () =>
    console.error(`spec-chat agent-service on 127.0.0.1:${port}`)
  );
}
