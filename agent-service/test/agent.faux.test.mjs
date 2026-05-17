// Pyramid integration level — the real agent wiring (createAgentSession
// + customTools + composed gate) driven by a scripted FAUX provider:
// $0, offline, deterministic (pi-sdk PATTERN §3 test rule). The
// curator-api is a local stub speaking the pinned wire contract.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { registerFauxProvider, fauxAssistantMessage, fauxToolCall, fauxText }
  from "@earendil-works/pi-ai";
import { AuthStorage, ModelRegistry } from "@earendil-works/pi-coding-agent";
import { createAgent } from "../src/agent.mjs";
import { CuratorClient } from "../src/curatorClient.mjs";

let server, base, hits;

before(async () => {
  hits = [];
  server = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      hits.push(req.url);
      res.writeHead(200, { "content-type": "application/json" });
      if (req.url === "/v1/kb/area")
        return res.end(JSON.stringify({ id: "vault", name: "Vault", summary: "s", entries: [] }));
      if (req.url === "/v1/doc-spec/put")
        return res.end(JSON.stringify({ id: "x", yaml: "topic: Vault\n", diff: "+ y" }));
      res.end(JSON.stringify({}));
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});
after(() => server.close());

function fauxHarness(deps) {
  const reg = registerFauxProvider();
  const authStorage = AuthStorage.create();
  authStorage.setRuntimeApiKey("faux", "faux-dummy"); // faux gate (e6 finding)
  const agent = createAgent({
    authStorage,
    modelRegistry: ModelRegistry.create(authStorage),
    model: reg.getModel(),
    client: new CuratorClient(base),
    ...deps,
  });
  return { reg, agent };
}

test("scripted read_kb_area: gate allows, tool hits curator-api, $0", async () => {
  const { reg, agent } = fauxHarness();
  reg.setResponses([
    fauxAssistantMessage([fauxToolCall("read_kb_area", { area: "vault" })], { stopReason: "toolUse" }),
    fauxAssistantMessage([fauxText("Here is the vault area.")]),
  ]);
  const r = await agent.runTurn("show me the vault kb area");
  assert.ok(hits.includes("/v1/kb/area"), `curator-api not called; hits=${hits}`);
  assert.equal(r.costTotal, 0, "faux must be $0");
  assert.deepEqual(r.toolErrors, []);
  assert.match(r.text, /vault area/i);
  reg.unregister();
});

test("scripted put_doc_spec: gate BLOCKS (no approval), tool never runs", async () => {
  const before = hits.length;
  const { reg, agent } = fauxHarness();
  reg.setResponses([
    fauxAssistantMessage(
      [fauxToolCall("put_doc_spec", { id: "vault.doc.yaml", edits: [{ op: "add_section_source", ref: "parent", section: "Deployment & Operations", source: "kb:area=disaster-recovery" }] })],
      { stopReason: "toolUse" }
    ),
    fauxAssistantMessage([fauxText("I need your approval to apply that edit.")]),
  ]);
  const r = await agent.runTurn("widen the sources and save");
  assert.ok(!hits.slice(before).includes("/v1/doc-spec/put"), "mutation must NOT reach curator-api without approval");
  assert.equal(r.pendingApprovals.length, 1);
  assert.equal(r.pendingApprovals[0].name, "put_doc_spec");
  reg.unregister();
});

test("approved mutation flows through to curator-api", async () => {
  const before = hits.length;
  const approvals = { isApproved: (name) => name === "put_doc_spec" };
  const { reg, agent } = fauxHarness({ approvals });
  reg.setResponses([
    fauxAssistantMessage(
      [fauxToolCall("put_doc_spec", { id: "vault.doc.yaml", edits: [{ op: "add_section_source", ref: "parent", section: "Deployment & Operations", source: "kb:area=disaster-recovery" }] })],
      { stopReason: "toolUse" }
    ),
    fauxAssistantMessage([fauxText("Applied.")]),
  ]);
  const r = await agent.runTurn("apply the approved edit");
  assert.ok(hits.slice(before).includes("/v1/doc-spec/put"), "approved mutation should reach curator-api");
  assert.deepEqual(r.toolErrors, []);
  reg.unregister();
});
