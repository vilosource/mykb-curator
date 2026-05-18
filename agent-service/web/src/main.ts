// Spec-chat browser surface (design D7 option a): the browser holds
// NO credentials and runs NO inference — every turn is delegated to
// the server-side /chat via RemoteAgent (the real Agent + gate +
// OAuth + tools are server-side).
//
// HONESTY / SCOPE (verified vs not):
//  - spike-04 (E4/KU4) build+Playwright-verified pi-web-ui's NATIVE
//    in-browser-Agent path only. A ChatPanel driven WITHOUT an
//    in-browser Agent (our server-delegating shim) is NOT yet
//    spike-verified, so this v1 does NOT fabricate that binding. It
//    adopts pi-web-ui's stylesheet + lit and renders a conservative
//    transcript/composer + the D2/D6 approval panel over RemoteAgent.
//  - Full ChatPanel.setAgent adoption (rich tool-exec/streaming
//    rendering) is gated on a Playwright integration spike — the
//    same method spike-04 used. Tracked as the slice-5 live-UX close.
import { html, render } from "lit";
import "./app.css";
import { RemoteAgent } from "./remoteAgent.mjs";

type Msg = { role: "user" | "assistant"; text: string };
type Proposal = { id: string; name: string; args: unknown };

const agent = new RemoteAgent(""); // same-origin: served by the agent-service
const transcript: Msg[] = [];
let pending: Proposal[] = [];
let busy = false;

const root = document.getElementById("app")!;

function view() {
  render(
    html`
      <main class="mx-auto max-w-3xl p-4 flex flex-col gap-3">
        <h1 class="text-lg font-semibold">mykb-curator — spec chat</h1>
        <div class="flex flex-col gap-2">
          ${transcript.map(
            (m) => html`
              <div class="rounded p-2 ${m.role === "user" ? "bg-muted" : "bg-card border"}">
                <div class="text-xs opacity-60">${m.role}</div>
                <div class="whitespace-pre-wrap">${m.text}</div>
              </div>
            `,
          )}
        </div>
        ${pending.length
          ? html`
              <div class="rounded border border-amber-500 p-3 flex flex-col gap-2">
                <div class="font-medium">Approval required (D2/D6)</div>
                ${pending.map(
                  (p) => html`
                    <div class="flex items-center justify-between gap-2">
                      <code class="text-sm">${p.name} ${JSON.stringify(p.args)}</code>
                      <button class="border rounded px-2 py-1" @click=${() => approve(p)}>
                        Approve &amp; apply
                      </button>
                    </div>
                  `,
                )}
              </div>
            `
          : null}
        <form
          @submit=${(e: Event) => {
            e.preventDefault();
            const i = (e.target as HTMLFormElement).elements.namedItem("p") as HTMLInputElement;
            if (i.value.trim()) sendTurn(i.value.trim()), (i.value = "");
          }}
          class="flex gap-2"
        >
          <input
            name="p"
            ?disabled=${busy}
            placeholder="e.g. the Deployment & Operations section claims daily Raft→GRS but it's ungrounded — fix it"
            class="flex-1 border rounded px-2 py-1"
          />
          <button ?disabled=${busy} class="border rounded px-3 py-1">${busy ? "…" : "Send"}</button>
        </form>
      </main>
    `,
    root,
  );
}

async function sendTurn(prompt: string) {
  transcript.push({ role: "user", text: prompt });
  busy = true;
  view();
  try {
    const r = await agent.send(prompt);
    transcript.push({ role: "assistant", text: r.text });
    pending = r.pendingApprovals;
  } catch (e) {
    transcript.push({ role: "assistant", text: `⚠ ${(e as Error).message}` });
  } finally {
    busy = false;
    view();
  }
}

async function approve(p: Proposal) {
  // D8: the server applies the held proposal deterministically from
  // its captured args — no re-prompt / second LLM turn.
  try {
    const r: any = await agent.approve(p);
    transcript.push({
      role: "assistant",
      text: `✓ Applied ${p.name} — ${JSON.stringify(r.result ?? r)}`,
    });
  } catch (e) {
    transcript.push({ role: "assistant", text: `⚠ apply failed: ${(e as Error).message}` });
  }
  pending = pending.filter((x) => x !== p);
  view();
}

view();
