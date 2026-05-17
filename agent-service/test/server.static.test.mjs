// Pyramid unit level — same-origin SPA serving + traversal guard.
// Deterministic, no SDK (fake agent injected).
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createApp } from "../src/server.mjs";

let dir, base, server;

before(async () => {
  dir = await mkdtemp(join(tmpdir(), "specchat-web-"));
  await writeFile(join(dir, "index.html"), "<!doctype html><title>spec-chat</title>");
  await mkdir(join(dir, "assets"));
  await writeFile(join(dir, "assets", "app.js"), "export const x=1;");
  const app = createApp({
    agent: { runTurn: async () => ({ text: "stub", pendingApprovals: [] }) },
    webRoot: dir,
  });
  server = app.server;
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});
after(() => server.close());

test("GET / serves index.html", async () => {
  const r = await fetch(base + "/");
  assert.equal(r.status, 200);
  assert.match(await r.text(), /spec-chat/);
});

test("GET asset serves it with js mime", async () => {
  const r = await fetch(base + "/assets/app.js");
  assert.equal(r.status, 200);
  assert.match(r.headers.get("content-type"), /javascript/);
});

test("unknown route falls back to SPA index", async () => {
  const r = await fetch(base + "/spec/vault");
  assert.equal(r.status, 200);
  assert.match(await r.text(), /spec-chat/);
});

test("path traversal blocked", async () => {
  const r = await fetch(base + "/..%2f..%2fetc%2fpasswd");
  assert.equal(r.status, 400);
});

test("/chat still routed to the agent (not static)", async () => {
  const r = await fetch(base + "/chat", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ prompt: "hi" }),
  });
  assert.equal(r.status, 200);
  assert.equal((await r.json()).text, "stub");
});
