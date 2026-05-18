// The composed beforeToolCall policy gate (pi-sdk PATTERN §3, [E3]).
//
// Encodes two design decisions:
//   D2/D6 — put_doc_spec and propose_kb_entry mutate the spec/brain.
//           They are HARD HITL and are NEVER applied through the LLM:
//           the gate ALWAYS blocks them, records the proposal with a
//           stable id, and the human applies it out-of-band via
//           server-side /approve {id} (D8 fix — apply is a
//           deterministic server action on the captured args, not a
//           second stateless LLM turn that must re-issue them).
//   D4    — preview_spec runs the paid Opus render+Judge. Bounded:
//           at most one preview per user turn, and a per-session
//           ceiling (default 10, configurable). Cost is accumulated
//           for telemetry; the ceiling itself is preview-count.
//
// Read tools (read_kb_area, get_doc_spec) are always allowed.
// Anything else is default-denied (the noTools:"all" sandbox spirit).

const READ_TOOLS = new Set(["read_kb_area", "get_doc_spec"]);
const MUTATION_TOOLS = new Set(["put_doc_spec", "propose_kb_entry"]);

// makeGate({ perSessionCeiling? }) -> { gate, beginTurn, recordCost,
// takePending, state }. `gate` is the async beforeToolCall.
export function makeGate({ perSessionCeiling = 10 } = {}) {
  const state = {
    turnPreviewCount: 0,
    sessionPreviewCount: 0,
    sessionCostUSD: 0,
    seq: 0,
    pending: [], // proposed mutations awaiting human ACK (id'd, for /approve)
    ceiling: perSessionCeiling,
  };

  function beginTurn() {
    state.turnPreviewCount = 0;
  }

  // Remove + return a held proposal by id (the server applies it
  // deterministically on /approve; D8). Returns undefined if absent.
  function takePending(id) {
    const i = state.pending.findIndex((p) => p.id === id);
    if (i < 0) return undefined;
    return state.pending.splice(i, 1)[0];
  }

  function recordCost(usd) {
    if (typeof usd === "number" && isFinite(usd)) state.sessionCostUSD += usd;
  }

  async function gate(ctxArg) {
    const name = ctxArg?.toolCall?.name;
    const args = ctxArg?.args ?? {};

    if (READ_TOOLS.has(name)) return undefined;

    if (MUTATION_TOOLS.has(name)) {
      // ALWAYS blocked at the agent layer — mutations are never
      // applied via the LLM. Record with a stable id; the human
      // applies it via server-side /approve {id} (D8).
      const id = `p${++state.seq}`;
      state.pending.push({ id, name, args, at: Date.now() });
      return {
        block: true,
        reason:
          `HITL (design D2/D6/D8): '${name}' mutates the ` +
          `${name === "propose_kb_entry" ? "knowledge brain" : "doc-spec"}. ` +
          `Recorded as pending proposal '${id}'. Surface the proposed ` +
          `change (the diff) to the human; it is applied out-of-band ` +
          `via /approve once they confirm — never re-issued by you.`,
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

  return { gate, beginTurn, recordCost, takePending, state };
}
