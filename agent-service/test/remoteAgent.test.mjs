// Pyramid unit level — the browser->server transport + the human
// approval round-trip, deterministic against a stub agent-service.
// No pi-web-ui, no SDK: this is the verified, contract-grounded core
// of slice 4 (the ChatPanel binding is build-verified separately).
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { RemoteAgent } from "../web/src/remoteAgent.mjs";

let server, base, approved;

before(async () => {
  approved = [];
  server = createServer((req, res) => {
    let b = "";
    req.on("data", (c) => (b += c));
    req.on("end", () => {
      const body = JSON.parse(b || "{}");
      const send = (code, obj) => {
        res.writeHead(code, { "content-type": "application/json" });
        res.end(JSON.stringify(obj));
      };
      if (req.url === "/chat") {
        if (!body.prompt) return send(400, { error: "prompt required" });
        if (/widen/.test(body.prompt))
          return send(200, {
            text: "I need approval to apply this diff.",
            pendingApprovals: [{ id: "p1", name: "put_doc_spec", args: { id: "vault.doc.yaml" } }],
            ok: true, sessionCostUSD: 0, sessionPreviewCount: 0,
          });
        return send(200, { text: "Here is the vault area.", pendingApprovals: [], ok: true });
      }
      if (req.url === "/approve") {
        approved.push(body);
        return send(200, { applied: "put_doc_spec", id: body.id, result: { diff: "+x" } });
      }
      send(404, { error: "no route" });
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});
after(() => server.close());

test("send delegates a turn to /chat and returns text", async () => {
  const a = new RemoteAgent(base);
  const r = await a.send("show vault");
  assert.match(r.text, /vault area/i);
  assert.deepEqual(r.pendingApprovals, []);
});

test("pending mutation surfaced; approve() posts only the id (D8)", async () => {
  const a = new RemoteAgent(base);
  const r = await a.send("widen sources and save");
  assert.equal(r.pendingApprovals.length, 1);
  assert.equal(a.pending[0].id, "p1");

  const res = await a.approve(r.pendingApprovals[0]);
  assert.equal(approved.length, 1);
  assert.deepEqual(approved[0], { id: "p1" }, "approve sends ONLY the id; server applies from captured args");
  assert.equal(res.applied, "put_doc_spec");
});

test("approve requires a proposal id", async () => {
  const a = new RemoteAgent(base);
  await assert.rejects(() => a.approve({}), /id required/);
});

test("server error surfaced (faithful)", async () => {
  const a = new RemoteAgent(base);
  await assert.rejects(() => a.send(""), /400/);
});
