// Pyramid integration level — the HTTP boundary + the /approve HITL
// confirm channel, end to end with a faux model + stub curator-api.
// Proves the D2/D6 loop: mutation blocked -> human /approve -> same
// mutation now reaches the curator-api. $0, offline.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { registerFauxProvider, fauxAssistantMessage, fauxToolCall, fauxText }
  from "@earendil-works/pi-ai";
import { AuthStorage, ModelRegistry } from "@earendil-works/pi-coding-agent";
import { createApp, makeApprovals } from "../src/server.mjs";
import { createAgent } from "../src/agent.mjs";
import { CuratorClient } from "../src/curatorClient.mjs";

let curator, curatorBase, hits;
before(async () => {
  hits = [];
  curator = createServer((req, res) => {
    let b = "";
    req.on("data", (c) => (b += c));
    req.on("end", () => {
      hits.push(req.url);
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ id: "x", yaml: "topic: Vault\n", diff: "+ kb:area=disaster-recovery" }));
    });
  });
  await new Promise((r) => curator.listen(0, "127.0.0.1", r));
  curatorBase = `http://127.0.0.1:${curator.address().port}`;
});
after(() => curator.close());

async function appOnFaux(reg) {
  const authStorage = AuthStorage.create();
  authStorage.setRuntimeApiKey("faux", "faux-dummy");
  const approvals = makeApprovals();
  const agent = createAgent({
    authStorage,
    modelRegistry: ModelRegistry.create(authStorage),
    model: reg.getModel(),
    client: new CuratorClient(curatorBase),
    approvals,
  });
  const { server } = createApp({ agent, approvals });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}`;
  return { base, approvals, close: () => server.close() };
}

const putCall = () =>
  fauxAssistantMessage(
    [fauxToolCall("put_doc_spec", {
      id: "vault.doc.yaml",
      edits: [{ op: "add_section_source", ref: "parent", section: "Deployment & Operations", source: "kb:area=disaster-recovery" }],
    })],
    { stopReason: "toolUse" }
  );

test("healthz", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);
  const r = await fetch(base + "/healthz");
  assert.equal(r.status, 200);
  close();
  reg.unregister();
});

test("D2/D6 loop: blocked -> /approve -> mutation reaches curator-api", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);

  // Turn 1: model tries the mutation; gate blocks (no approval yet).
  reg.setResponses([putCall(), fauxAssistantMessage([fauxText("Need your OK to apply this diff.")])]);
  const before = hits.length;
  let res = await (await fetch(base + "/chat", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ prompt: "widen sources and save" }),
  })).json();
  assert.equal(res.pendingApprovals.length, 1);
  assert.ok(!hits.slice(before).includes("/v1/doc-spec/put"), "must not write pre-approval");

  // Human approves out-of-band through the confirm channel.
  const ap = await fetch(base + "/approve", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify(res.pendingApprovals[0]),
  });
  assert.equal(ap.status, 200);

  // Turn 2: same mutation now passes the gate and reaches curator-api.
  reg.setResponses([putCall(), fauxAssistantMessage([fauxText("Applied.")])]);
  const mark = hits.length;
  res = await (await fetch(base + "/chat", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ prompt: "now apply it" }),
  })).json();
  assert.ok(hits.slice(mark).includes("/v1/doc-spec/put"), "approved mutation should reach curator-api");
  assert.deepEqual(res.toolErrors, []);

  close();
  reg.unregister();
});

test("/chat requires a prompt", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);
  const r = await fetch(base + "/chat", {
    method: "POST", headers: { "content-type": "application/json" }, body: "{}",
  });
  assert.equal(r.status, 400);
  close();
  reg.unregister();
});
