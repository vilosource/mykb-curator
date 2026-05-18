// Pyramid integration level — the HTTP boundary + the D8 approval
// path: a faux model proposes a mutation, the gate blocks it, and
// POST /approve {id} APPLIES it server-side from the CAPTURED args
// (no second LLM turn). This is the live-grounded shape — it does
// NOT script an identical fauxToolCall on a second turn (the old
// test's flaw, which live testing exposed). $0, offline.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { registerFauxProvider, fauxAssistantMessage, fauxToolCall, fauxText }
  from "@earendil-works/pi-ai";
import { AuthStorage, ModelRegistry } from "@earendil-works/pi-coding-agent";
import { createApp } from "../src/server.mjs";
import { createAgent } from "../src/agent.mjs";
import { CuratorClient } from "../src/curatorClient.mjs";

let curator, curatorBase, calls;
before(async () => {
  calls = [];
  curator = createServer((req, res) => {
    let b = "";
    req.on("data", (c) => (b += c));
    req.on("end", () => {
      calls.push({ url: req.url, body: JSON.parse(b || "{}") });
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
  const agent = createAgent({
    authStorage,
    modelRegistry: ModelRegistry.create(authStorage),
    model: reg.getModel(),
    client: new CuratorClient(curatorBase),
  });
  const { server } = createApp({ agent });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  return { base: `http://127.0.0.1:${server.address().port}`, close: () => server.close() };
}

const EDITS = [{ op: "add_section_source", ref: "parent", section: "Deployment & Operations", source: "kb:area=disaster-recovery" }];
const putCall = () =>
  fauxAssistantMessage(
    [fauxToolCall("put_doc_spec", { id: "vault.doc.yaml", edits: EDITS })],
    { stopReason: "toolUse" }
  );
const post = (base, path, obj) =>
  fetch(base + path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(obj) });

test("healthz", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);
  assert.equal((await fetch(base + "/healthz")).status, 200);
  close();
  reg.unregister();
});

test("D8: blocked -> /approve {id} -> server applies from CAPTURED args, no 2nd LLM turn", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);

  // Turn 1: model proposes the mutation; gate blocks; curator untouched.
  reg.setResponses([putCall(), fauxAssistantMessage([fauxText("Need your OK.")])]);
  const mark = calls.length;
  const res = await (await post(base, "/chat", { prompt: "widen sources and save" })).json();
  assert.equal(res.pendingApprovals.length, 1);
  const p = res.pendingApprovals[0];
  assert.equal(p.name, "put_doc_spec");
  assert.ok(p.id, "proposal carries a stable id");
  assert.equal(calls.length, mark, "nothing reaches curator-api pre-approval");

  // Human approves by id. The SERVER applies it — no /chat, no LLM.
  const ap = await post(base, "/approve", { id: p.id });
  assert.equal(ap.status, 200);
  const apb = await ap.json();
  assert.equal(apb.applied, "put_doc_spec");

  // curator-api got exactly ONE call, with the CAPTURED args verbatim.
  assert.equal(calls.length, mark + 1);
  assert.equal(calls[mark].url, "/v1/doc-spec/put");
  assert.deepEqual(calls[mark].body, { id: "vault.doc.yaml", edits: EDITS });

  // Re-approving the same id is now a no-op (removed from pending).
  assert.equal((await post(base, "/approve", { id: p.id })).status, 404);

  close();
  reg.unregister();
});

test("/approve before any chat -> 409; unknown id -> 404; missing id -> 400", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);
  assert.equal((await post(base, "/approve", { id: "p1" })).status, 409);
  await post(base, "/chat", { prompt: "hi" }); // builds the agent (no mutation)
  assert.equal((await post(base, "/approve", { id: "nope" })).status, 404);
  assert.equal((await post(base, "/approve", {})).status, 400);
  close();
  reg.unregister();
});

test("/chat requires a prompt", async () => {
  const reg = registerFauxProvider();
  const { base, close } = await appOnFaux(reg);
  assert.equal((await post(base, "/chat", {})).status, 400);
  close();
  reg.unregister();
});
