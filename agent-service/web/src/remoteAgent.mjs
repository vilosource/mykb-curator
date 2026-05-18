// The server-delegating transport (design D7 option a). The browser
// holds NO credentials and runs NO inference: every turn is delegated
// to the server-side /chat (real Agent + gate + OAuth + tools live
// there). Pure fetch + plain data — unit-testable on any Node,
// independent of pi-web-ui. main.ts binds this to the ChatPanel.
//
// Speaks the slice-3 boundary contract:
//   POST /chat    {prompt} -> { text, pendingApprovals[{id,...}], ... }
//   POST /approve {id}      -> { applied, id, result }  (D8: the
//                              server applies it; no re-prompt)

export class RemoteAgent {
  constructor(baseURL = "") {
    this.baseURL = baseURL.replace(/\/+$/, "");
    this.lastPending = [];
  }

  async #post(path, body) {
    const res = await fetch(this.baseURL + path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const text = await res.text();
    let data;
    try {
      data = text ? JSON.parse(text) : {};
    } catch {
      throw new Error(`agent-service ${path}: non-JSON ${res.status}`);
    }
    if (!res.ok) throw new Error(`agent-service ${path} ${res.status}: ${data.error || text}`);
    return data;
  }

  // One conversational turn. Returns the assistant text plus any
  // mutations the gate is holding for human approval (D2/D6).
  async send(prompt) {
    const r = await this.#post("/chat", { prompt });
    this.lastPending = Array.isArray(r.pendingApprovals) ? r.pendingApprovals : [];
    return {
      text: r.text || "",
      pendingApprovals: this.lastPending,
      verdicts: r.verdicts || [],
      sessionCostUSD: r.sessionCostUSD ?? 0,
      sessionPreviewCount: r.sessionPreviewCount ?? 0,
      ok: r.ok !== false,
    };
  }

  // The human confirms a held mutation through the out-of-band
  // channel the model cannot forge.
  // D8: approve by stable id; the server applies it deterministically
  // from the captured args. No re-prompt, no second LLM turn.
  async approve(proposal) {
    if (!proposal || !proposal.id) throw new Error("approve: proposal.id required");
    return this.#post("/approve", { id: proposal.id });
  }

  get pending() {
    return this.lastPending;
  }
}
