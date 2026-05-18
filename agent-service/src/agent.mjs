// The canonical pi-SDK safe wiring (pi-sdk PATTERN §2/§3), adapted
// from pattern/scaffold/agent-service.mjs. Session-per-request,
// noTools:"all" + the 5 customTools, the composed gate as
// beforeToolCall, own timeout, dispose, stopReason branched, cost
// recorded into the gate's session budget (D4 telemetry).
import { AuthStorage, ModelRegistry, SessionManager, createAgentSession }
  from "@earendil-works/pi-coding-agent";
import { makeTools } from "./tools.mjs";
import { makeGate } from "./gate.mjs";
import { CuratorClient } from "./curatorClient.mjs";

// resolveModel: ALWAYS registry-resolve + validate (PATTERN §3 [E1]).
// Never static getModel for github-copilot.
function resolveModel(registry, provider, id) {
  const m = registry.getAvailable().find((x) => x.provider === provider && x.id === id);
  if (!m) {
    throw new Error(
      `model ${provider}/${id} not available (creds? id?). available: ` +
        registry.getAvailable().map((x) => x.provider + "/" + x.id).join(", ")
    );
  }
  return m;
}

// createAgent builds a reusable per-process harness. deps lets tests
// inject a faux model + an in-memory approvals channel + a stub
// CuratorClient (all $0, offline).
export function createAgent(deps = {}) {
  const authStorage = deps.authStorage || AuthStorage.create();
  const registry = deps.modelRegistry || ModelRegistry.create(authStorage);
  const client = deps.client || new CuratorClient();
  const { gate, beginTurn, recordCost, takePending, state } = makeGate({
    perSessionCeiling: deps.perSessionCeiling,
  });
  const tools = makeTools(client);

  // Dev default is the 0x model (PATTERN cost rule); tests pass a
  // faux model object via deps.model.
  const model =
    deps.model || resolveModel(registry, "github-copilot", "gpt-4.1");

  async function runTurn(prompt, { timeoutMs = 60_000 } = {}) {
    const { session } = await createAgentSession({
      sessionManager: SessionManager.inMemory(),
      authStorage,
      modelRegistry: registry,
      model,
      noTools: "all",
      customTools: tools,
      tools: tools.map((t) => t.name),
    });
    session.agent.beforeToolCall = gate;
    beginTurn();

    let final = null;
    const toolErrors = [];
    session.subscribe((e) => {
      if ((e.type === "turn_end" || e.type === "message_end") && e.message) final = e.message;
      if (e.type === "tool_execution_end" && e.isError) toolErrors.push(e.toolName);
    });

    const timer = setTimeout(() => session.abort(), timeoutMs);
    try {
      await session.prompt(prompt);
    } finally {
      clearTimeout(timer);
    }

    const text = Array.isArray(final?.content)
      ? final.content.filter((c) => c?.type === "text").map((c) => c.text).join("")
      : "";
    const cost = final?.usage?.cost?.total ?? 0;
    recordCost(cost);

    const result = {
      ok: final?.stopReason === "stop" || final?.stopReason === "toolUse",
      stopReason: final?.stopReason ?? null,
      errorMessage: final?.errorMessage ?? null,
      text,
      toolErrors,
      costTotal: cost,
      sessionCostUSD: state.sessionCostUSD,
      sessionPreviewCount: state.sessionPreviewCount,
      pendingApprovals: state.pending.slice(),
    };
    session.dispose();
    return result;
  }

  // takePending + client exposed so the server applies an approved
  // proposal deterministically from captured args (D8).
  return { runTurn, state, gate, takePending, client };
}
