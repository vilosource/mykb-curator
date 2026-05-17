// Pyramid unit level — the composed beforeToolCall gate.
// Pure JS, no SDK/network. Encodes design D2/D6 (mutation HITL) and
// D4 (preview spend bounds). $0, deterministic.
import { test } from "node:test";
import assert from "node:assert/strict";
import { makeGate } from "../src/gate.mjs";

const ctx = (name, args = {}) => ({ toolCall: { name }, args });

test("read tools always allowed", async () => {
  const { gate } = makeGate({});
  assert.equal(await gate(ctx("read_kb_area", { area: "vault" })), undefined);
  assert.equal(await gate(ctx("get_doc_spec", { id: "vault.doc.yaml" })), undefined);
});

test("unknown tool is default-denied (sandbox)", async () => {
  const { gate } = makeGate({});
  const r = await gate(ctx("rm_rf", {}));
  assert.equal(r?.block, true);
});

test("mutation tools blocked without explicit approval (D2/D6)", async () => {
  const { gate, state } = makeGate({});
  const r = await gate(ctx("put_doc_spec", { id: "x", edits: [{ op: "add_section_source" }] }));
  assert.equal(r?.block, true);
  assert.match(r.reason, /human|approv|HITL/i);
  // the pending proposal is recorded for the UI to confirm
  assert.equal(state.pending.length, 1);
  assert.equal(state.pending[0].name, "put_doc_spec");

  const r2 = await gate(ctx("propose_kb_entry", { area: "vault", type: "fact" }));
  assert.equal(r2?.block, true);
});

test("mutation tool allowed once the approval channel approves it", async () => {
  const approved = new Set();
  const approvals = {
    isApproved: (name, args) => approved.has(name + JSON.stringify(args)),
  };
  const { gate } = makeGate({ approvals });
  const args = { area: "vault", type: "fact", text: "t", source: "s" };

  assert.equal((await gate(ctx("propose_kb_entry", args)))?.block, true);
  approved.add("propose_kb_entry" + JSON.stringify(args)); // human ACKs out-of-band
  assert.equal(await gate(ctx("propose_kb_entry", args)), undefined);
});

test("D4: only one preview per turn", async () => {
  const { gate, beginTurn } = makeGate({});
  beginTurn();
  assert.equal(await gate(ctx("preview_spec", { id: "x" })), undefined);
  const r = await gate(ctx("preview_spec", { id: "x" }));
  assert.equal(r?.block, true);
  assert.match(r.reason, /per-turn|already previewed/i);
  // next turn resets the per-turn budget
  beginTurn();
  assert.equal(await gate(ctx("preview_spec", { id: "x" })), undefined);
});

test("D4: per-session preview ceiling (default 10) enforced", async () => {
  const { gate, beginTurn } = makeGate({});
  for (let i = 0; i < 10; i++) {
    beginTurn();
    assert.equal(await gate(ctx("preview_spec", { id: "x" })), undefined, `preview ${i}`);
  }
  beginTurn();
  const r = await gate(ctx("preview_spec", { id: "x" }));
  assert.equal(r?.block, true);
  assert.match(r.reason, /ceiling|session|budget/i);
  assert.match(r.reason, /remaining 0|0 remaining|10/i);
});

test("D4: ceiling is configurable", async () => {
  const { gate, beginTurn } = makeGate({ perSessionCeiling: 2 });
  beginTurn(); assert.equal(await gate(ctx("preview_spec", {})), undefined);
  beginTurn(); assert.equal(await gate(ctx("preview_spec", {})), undefined);
  beginTurn();
  assert.equal((await gate(ctx("preview_spec", {})))?.block, true);
});

test("cost accumulation is surfaced for budget telemetry", async () => {
  const { gate, beginTurn, recordCost, state } = makeGate({});
  beginTurn();
  await gate(ctx("preview_spec", {}));
  recordCost(0.12);
  beginTurn();
  await gate(ctx("preview_spec", {}));
  recordCost(0.08);
  assert.ok(Math.abs(state.sessionCostUSD - 0.2) < 1e-9);
  assert.equal(state.sessionPreviewCount, 2);
});
