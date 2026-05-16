---
title: Test/Vault_Projection
spec_hash: 2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39
---

# Test/Vault_Projection

## Vault Architecture

<!-- CURATOR:BEGIN block=s0-b0 zone=editorial provenance= -->

Centralised secrets manager — HashiCorp Vault on Raft HA cluster with Azure Key Vault auto-unseal

<!-- CURATOR:END block=s0-b0 -->

## Facts

<!-- CURATOR:BEGIN block=s1-b0 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f1: -->

Vault runs as an HA Raft cluster with 3 nodes

<!-- CURATOR:END block=s1-b0 -->

<!-- CURATOR:BEGIN block=s1-b1 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f2: -->

Auto-unseal uses Azure Key Vault

<!-- CURATOR:END block=s1-b1 -->

<!-- CURATOR:BEGIN block=s1-b2 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f3: -->

Deployed version is Vault 1.17.3

<!-- CURATOR:END block=s1-b2 -->

<!-- CURATOR:BEGIN block=s1-b3 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f4: -->

Storage backend is Integrated Storage (Raft); data lives at /vault/data on each node and is replicated by the Raft consensus protocol

<!-- CURATOR:END block=s1-b3 -->

<!-- CURATOR:BEGIN block=s1-b4 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f5: -->

The API and UI listen on port 8200 (TLS); the Raft cluster port is 8201; both are internal-only

<!-- CURATOR:END block=s1-b4 -->

<!-- CURATOR:BEGIN block=s1-b5 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f6: -->

Vault is reached internally at https://vault.acme.internal; Traefik routes requests to the active Raft leader; there is no public ingress

<!-- CURATOR:END block=s1-b5 -->

<!-- CURATOR:BEGIN block=s1-b6 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f7: -->

Workloads authenticate via the Kubernetes auth method; humans authenticate via OIDC against Azure Entra ID; CI pipelines use AppRole

<!-- CURATOR:END block=s1-b6 -->

<!-- CURATOR:BEGIN block=s1-b7 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f8: -->

Enabled secret engines: KV v2 for application secrets, Database for short-lived Postgres credentials, PKI for internal TLS issuance, and Transit for encryption-as-a-service

<!-- CURATOR:END block=s1-b7 -->

<!-- CURATOR:BEGIN block=s1-b8 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f9: -->

Access is least-privilege: every application has a dedicated policy scoped to its own KV path (secret/data/<app>/*); there are no wildcard read policies

<!-- CURATOR:END block=s1-b8 -->

<!-- CURATOR:BEGIN block=s1-b9 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f10: -->

A file audit device is enabled on every node and shipped to the central log stack; if audit logging cannot write, Vault fails requests closed by design

<!-- CURATOR:END block=s1-b9 -->

<!-- CURATOR:BEGIN block=s1-b10 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f11: -->

A Raft snapshot is taken nightly and written to an Azure Blob container with 30-day retention; restore is documented in the vault-dr runbook

<!-- CURATOR:END block=s1-b10 -->

<!-- CURATOR:BEGIN block=s1-b11 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f12: -->

Telemetry is exported in Prometheus format and scraped into the observability stack; unseal status and Raft leadership changes are alerted on

<!-- CURATOR:END block=s1-b11 -->

<!-- CURATOR:BEGIN block=s1-b12 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f13: -->

Vault runs on the shared infra Docker Swarm (the same cluster that hosts Harbor and Traefik), deployed as the Swarm service named 'vault' with 3 replicas

<!-- CURATOR:END block=s1-b12 -->

<!-- CURATOR:BEGIN block=s1-b13 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f14: -->

The 3 Vault replicas are pinned via Swarm placement constraints to the 3 swarm manager nodes infra-swarm-mgr-1, infra-swarm-mgr-2 and infra-swarm-mgr-3, so the Raft quorum maps 1:1 onto the managers (losing one manager keeps quorum)

<!-- CURATOR:END block=s1-b13 -->

<!-- CURATOR:BEGIN block=s1-b14 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f15: -->

Each swarm manager node is an Ubuntu 22.04 VM (4 vCPU / 8 GB RAM) with a dedicated local volume bind-mounted at /vault/data for the node's Raft store

<!-- CURATOR:END block=s1-b14 -->

<!-- CURATOR:BEGIN block=s1-b15 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f16: -->

Vault attaches to the encrypted Swarm overlay network 'infra-net'; ports 8200/8201 are NOT published on the host — they are reachable only inside the overlay, never on a node IP

<!-- CURATOR:END block=s1-b15 -->

<!-- CURATOR:BEGIN block=s1-b16 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-f17: -->

Clients reach Vault only through Traefik, which also runs on the swarm: Traefik terminates TLS for https://vault.acme.internal and load-balances across the healthy Vault replicas, redirecting to the active Raft leader. Apps, human operators and CI all use that single hostname; there is no direct node or container access

<!-- CURATOR:END block=s1-b16 -->

## Decisions

<!-- CURATOR:BEGIN block=s2-b0 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-d1: -->

VAULT-001: Use Raft over Consul for HA backend
Why: Removes Consul as a hard dependency; Raft is bundled in Vault
Rejected: Consul cluster (operationally heavier)

<!-- CURATOR:END block=s2-b0 -->

<!-- CURATOR:BEGIN block=s2-b1 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-d2: -->

VAULT-002: Auto-unseal with Azure Key Vault
Why: No human-held unseal keys; nodes recover unattended after a restart
Rejected: Shamir manual unseal (operational toil, 3 keyholders); Transit auto-unseal (needs a second Vault)

<!-- CURATOR:END block=s2-b1 -->

<!-- CURATOR:BEGIN block=s2-b2 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-d3: -->

VAULT-003: Kubernetes auth method as the primary workload identity
Why: Pods present their ServiceAccount token; no long-lived secrets to distribute or rotate
Rejected: AppRole for all workloads (secret-zero distribution problem)

<!-- CURATOR:END block=s2-b2 -->

<!-- CURATOR:BEGIN block=s2-b3 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-d4: -->

VAULT-004: KV v2 as the default secret engine
Why: Versioned secrets + soft-delete give an audit trail and accidental-overwrite recovery
Rejected: KV v1 (no versioning)

<!-- CURATOR:END block=s2-b3 -->

<!-- CURATOR:BEGIN block=s2-b4 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-d5: -->

VAULT-005: Run Vault on the shared infra Docker Swarm with replicas pinned to the manager nodes
Why: Reuses the existing managed swarm (Traefik ingress, overlay networking, deploy tooling) instead of operating a separate cluster; pinning to the 3 managers makes the Raft quorum coincide with the swarm control-plane quorum
Rejected: A dedicated 3-VM Vault cluster outside the swarm (more infrastructure to run and patch for no isolation benefit at our scale)

<!-- CURATOR:END block=s2-b4 -->

## Gotchas

<!-- CURATOR:BEGIN block=s3-b0 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-g1: -->

After a full cluster restart, nodes stay sealed until they can reach Azure Key Vault. If the Azure KV firewall or network path is down, Vault will not auto-unseal — check network egress before suspecting Vault itself.

<!-- CURATOR:END block=s3-b0 -->

<!-- CURATOR:BEGIN block=s3-b1 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-g2: -->

Only the Raft leader serves writes; clients hitting a standby get a redirect. Tooling that pins a node IP instead of the vault.acme.internal endpoint breaks on leader election.

<!-- CURATOR:END block=s3-b1 -->

<!-- CURATOR:BEGIN block=s3-b2 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-g3: -->

Kubernetes-auth tokens are short-lived by design; jobs that run longer than the token TTL must re-authenticate rather than cache the first token for the whole run.

<!-- CURATOR:END block=s3-b2 -->

## Patterns

<!-- CURATOR:BEGIN block=s4-b0 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-p1: -->

App reads a secret: the pod's Kubernetes ServiceAccount token is exchanged at the Kubernetes auth endpoint for a short-lived Vault token, which is then used to read secret/data/<app>/* — scoped by that app's policy. No static Vault token is ever baked into an image.

<!-- CURATOR:END block=s4-b0 -->

<!-- CURATOR:BEGIN block=s4-b1 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-p2: -->

Onboarding a new app: add a KV path secret/<app>, write a least-privilege HCL policy, bind a Kubernetes auth role to the app's ServiceAccount, all via the vault-policy GitOps repo (CI applies with terraform-vault). No manual UI changes in production.

<!-- CURATOR:END block=s4-b1 -->

<!-- CURATOR:BEGIN block=s4-b2 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-p3: -->

Dynamic database credentials: services request a Postgres role from the Database engine and receive a username/password with a 1-hour TTL that Vault revokes automatically; long-lived DB passwords are not stored anywhere.

<!-- CURATOR:END block=s4-b2 -->

## Links

<!-- CURATOR:BEGIN block=s5-b0 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-l1: -->

HashiCorp Vault — official documentation
→ https://developer.hashicorp.com/vault/docs

<!-- CURATOR:END block=s5-b0 -->

<!-- CURATOR:BEGIN block=s5-b1 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-l2: -->

Integrated Storage (Raft) concepts
→ https://developer.hashicorp.com/vault/docs/concepts/integrated-storage

<!-- CURATOR:END block=s5-b1 -->

<!-- CURATOR:BEGIN block=s5-b2 zone=editorial provenance=2bf2166925a1e48221f6434ab92d809c95d4f9c3d858d4554e1325ff85718a39:vault-l3: -->

Internal: vault-dr runbook (snapshot + restore)
→ https://wiki.acme.internal/Runbook/Vault_DR

<!-- CURATOR:END block=s5-b2 -->