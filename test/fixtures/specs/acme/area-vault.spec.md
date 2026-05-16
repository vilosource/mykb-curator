---
wiki: acme
page: Area/Vault_Architecture
kind: editorial
version: 2
include:
  areas: [vault]
  workspaces: linked-to-areas
  exclude_zones: [incoming]
fact_check:
  link_rot: every-run
---

Write the definitive "Vault Architecture" page for our engineering
wiki. The reader may have **zero prior knowledge of HashiCorp Vault** —
lead them in from first principles, then go deep on how *we* run it.

Structure the page roughly as:

1. **What Vault is** — in plain language: a secrets manager, why
   hardcoded/sprawled secrets are a problem, and the core ideas a
   newcomer must grasp (sealing/unsealing, the seal/unseal flow,
   auth methods, secret engines, policies, leases/TTLs). Keep it
   short and concrete.
2. **Why we use it / key decisions** — narrate the architectural
   decisions (VAULT-001…VAULT-005): what we chose, what we rejected,
   and why. This is institutional memory; make the trade-offs clear.
3. **How our cluster is built** — the 3-node Raft HA topology,
   Integrated Storage, Azure Key Vault auto-unseal, version,
   listeners/ports, internal-only access via vault.acme.internal.
   Include a **mermaid diagram** of the cluster topology + how
   clients reach the active leader, and a **mermaid diagram** of the
   auto-unseal flow on node startup.
4. **Production setup** — be concrete and operator-useful: exactly
   which servers Vault runs on (the named swarm manager hosts and
   their specs), its relationship to the shared **infra Docker
   Swarm** (Swarm service, replica count, placement constraints
   pinning replicas to the managers, the overlay network, why
   ports are not host-published), and **how you actually reach
   Vault** end-to-end (Traefik on the swarm → TLS for
   vault.acme.internal → active leader; no direct node/container
   access). Include a **mermaid deployment diagram** showing the
   swarm managers, the pinned Vault replicas, the overlay network,
   Traefik ingress, and the client entry point. Cover decision
   VAULT-005 here.
5. **How an application gets a secret** — the Kubernetes-auth →
   short-lived token → scoped KV read path. Include a **mermaid
   sequence diagram** of that exchange. Mention dynamic database
   credentials.
6. **Access & security model** — auth methods (Kubernetes / OIDC /
   AppRole), least-privilege per-app policies, audit device
   (fail-closed), enabled secret engines and what each is for.
7. **Operations** — auto-unseal dependency on Azure KV, nightly
   Raft snapshot + retention + the DR runbook, telemetry/alerting,
   and the known gotchas (sealed-until-Azure-reachable,
   leader-only-writes, short token TTLs).
8. **References** — link out to the official docs and the internal
   DR runbook.

Diagrams: use fenced ```mermaid blocks (flowchart for topology +
unseal, sequenceDiagram for the secret-fetch path). They are
rendered to images and uploaded automatically — prefer a clear
diagram over a wall of text wherever one helps a newcomer.

Ground every organisation-specific claim in the supplied kb
content. General Vault background may be explained to orient the
reader, but do not invent our versions, topology, or decisions.
