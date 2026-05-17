// Pyramid unit level — the curator-api HTTP client against a stub
// server speaking the pinned wire contract. No SDK, no real curator.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { CuratorClient } from "../src/curatorClient.mjs";

let server, base, lastReq;

before(async () => {
  server = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      lastReq = { path: req.url, method: req.method, body: JSON.parse(body || "{}") };
      const send = (code, obj) => {
        res.writeHead(code, { "content-type": "application/json" });
        res.end(JSON.stringify(obj));
      };
      switch (req.url) {
        case "/v1/kb/area":
          return lastReq.body.area === "vault"
            ? send(200, { id: "vault", name: "Vault", summary: "s", entries: [] })
            : send(404, { error: "area not found: " + lastReq.body.area });
        case "/v1/doc-spec/get":
          return send(200, { id: lastReq.body.id, topic: "Vault", yaml: "topic: Vault\n", pages: [] });
        case "/v1/doc-spec/put":
          return send(200, { id: lastReq.body.id, yaml: "topic: Vault\n", diff: "+ x" });
        case "/v1/kb/propose-entry":
          return lastReq.body.source
            ? send(200, { entry_id: "id7", area: lastReq.body.area, zone: "incoming" })
            : send(400, { error: "source required (provenance is mandatory)" });
        case "/v1/preview":
          return send(200, {
            pages: [{ page: "P", markdown: "# P" }],
            all_pass: true, verdicts: [], ungrounded_claims: [], cost_usd: 0.01,
          });
        default:
          return send(404, { error: "no route" });
      }
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});

after(() => server.close());

test("readKbArea OK", async () => {
  const c = new CuratorClient(base);
  const a = await c.readKbArea("vault");
  assert.equal(a.id, "vault");
  assert.deepEqual(lastReq.body, { area: "vault" });
});

test("error body surfaced verbatim (faithful)", async () => {
  const c = new CuratorClient(base);
  await assert.rejects(() => c.readKbArea("nope"), /area not found: nope/);
});

test("putDocSpec sends the edit ops contract", async () => {
  const c = new CuratorClient(base);
  const edits = [{ op: "add_section_source", ref: "parent", section: "Deployment & Operations", source: "kb:area=disaster-recovery" }];
  const r = await c.putDocSpec("vault.doc.yaml", edits);
  assert.equal(r.diff, "+ x");
  assert.deepEqual(lastReq.body, { id: "vault.doc.yaml", edits });
});

test("proposeKbEntry without source -> 400 surfaced", async () => {
  const c = new CuratorClient(base);
  await assert.rejects(
    () => c.proposeKbEntry({ area: "vault", type: "fact", text: "t" }),
    /provenance is mandatory/
  );
});

test("previewSpec omits edits key when none", async () => {
  const c = new CuratorClient(base);
  const r = await c.previewSpec("vault.doc.yaml");
  assert.equal(r.cost_usd, 0.01);
  assert.deepEqual(lastReq.body, { id: "vault.doc.yaml" });
  await c.previewSpec("vault.doc.yaml", [{ op: "set_page_intent", ref: "parent", value: "x" }]);
  assert.ok(Array.isArray(lastReq.body.edits));
});
