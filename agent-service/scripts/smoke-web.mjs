// Deterministic live-UX harness (no OAuth, $0): the REAL built SPA +
// REAL static server + REAL RemoteAgent transport, with only the
// Agent/LLM faked. Scripts a normal turn and a pending-approval turn
// so a browser (Playwright) can verify D7a end to end.
import { createApp } from "../src/server.mjs";

const fakeAgent = {
  async runTurn(prompt) {
    if (/widen|grs|save|apply the approved/i.test(prompt)) {
      // first ask -> a held mutation; after /approve the UI re-prompts
      // with "Apply the approved change now." -> success.
      if (/apply the approved/i.test(prompt))
        return { text: "Applied: kb:area=disaster-recovery added to Deployment & Operations.", pendingApprovals: [], ok: true };
      return {
        text: "The 'daily Raft snapshot -> GRS' claim is ungrounded. I propose widening sources to kb:area=disaster-recovery.",
        pendingApprovals: [{ name: "put_doc_spec", args: { id: "vault.doc.yaml" } }],
        ok: true,
      };
    }
    return { text: "vault is a 3-node HA Raft cluster on Docker Swarm.", pendingApprovals: [], ok: true };
  },
};

const webRoot = process.env.WEB_ROOT;
const { server } = createApp({ agent: fakeAgent, webRoot });
const port = Number(process.env.PORT || 4778);
const host = process.env.HOST || "127.0.0.1";
server.listen(port, host, () => console.error(`smoke-web on http://${host}:${port}`));
