// The composed beforeToolCall policy gate (pi-sdk PATTERN §3, [E3]).
//
// Encodes two design decisions:
//   D2/D6 — put_doc_spec and propose_kb_entry mutate the spec/brain.
//           They are HARD HITL: blocked until an out-of-band human
//           approval (the slice-4 web UI's confirm channel) approves
//           THIS exact call. The agent cannot forge approval — it
//           flows through the injected `approvals` provider, not the
//           model.
//   D4    — preview_spec runs the paid Opus render+Judge. Bounded:
//           at most one preview per user turn, and a per-session
//           ceiling (default 10, configurable). Cost is accumulated
//           for telemetry; the ceiling itself is preview-count.
//
// Read tools (read_kb_area, get_doc_spec) are always allowed.
// Anything else is default-denied (the noTools:"all" sandbox spirit).

const READ_TOOLS = new Set(["read_kb_area", "get_doc_spec"]);
const MUTATION_TOOLS = new Set(["put_doc_spec", "propose_kb_entry"]);

// makeGate({ approvals?, perSessionCeiling? }) -> { gate, beginTurn,
// recordCost, state }. `gate` is the async beforeToolCall.
export function makeGate({ approvals, perSessionCeiling = 10 } = {}) {
  const state = {
    turnPreviewCount: 0,
    sessionPreviewCount: 0,
    sessionCostUSD: 0,
    pending: [], // proposed mutations awaiting human ACK (for the UI)
    ceiling: perSessionCeiling,
  };

  const isApproved =
    approvals && typeof approvals.isApproved === "function"
      ? (n, a) => approvals.isApproved(n, a)
      : () => false;

  function beginTurn() {
    state.turnPreviewCount = 0;
  }

  function recordCost(usd) {
    if (typeof usd === "number" && isFinite(usd)) state.sessionCostUSD += usd;
  }

  async function gate(ctxArg) {
    const name = ctxArg?.toolCall?.name;
    const args = ctxArg?.args ?? {};

    if (READ_TOOLS.has(name)) return undefined;

    if (MUTATION_TOOLS.has(name)) {
      if (isApproved(name, args)) return undefined;
      state.pending.push({ name, args, at: Date.now() });
      return {
        block: true,
        reason:
          `HITL (design D2/D6): '${name}' mutates the ` +
          `${name === "propose_kb_entry" ? "knowledge brain" : "doc-spec"}. ` +
          `Surface the proposed change (the diff) to the human and obtain ` +
          `explicit approval through the confirm channel before applying.`,
      };
    }

    if (name === "preview_spec") {
      if (state.turnPreviewCount >= 1) {
        return {
          block: true,
          reason:
            "D4: already previewed once this turn — only one " +
            "preview_spec per user message (no autonomous render→tweak loop).",
        };
      }
      if (state.sessionPreviewCount >= state.ceiling) {
        return {
          block: true,
          reason:
            `D4: per-session preview ceiling reached ` +
            `(${state.sessionPreviewCount}/${state.ceiling}, remaining 0). ` +
            `Ask the human to raise the budget to preview again.`,
        };
      }
      state.turnPreviewCount += 1;
      state.sessionPreviewCount += 1;
      return undefined;
    }

    return {
      block: true,
      reason: `sandbox: tool '${name}' is not on the allowlist`,
    };
  }

  return { gate, beginTurn, recordCost, state };
}
