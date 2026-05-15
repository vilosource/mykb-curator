---
title: Area/Vault_Architecture
spec_hash: d7850192396dc361e26b2613f5d1aafabb22dbffec7508ee021d431d03eb7e04
---

# Area/Vault_Architecture

## Vault Architecture

Centralised secrets manager — HashiCorp Vault on Raft HA cluster with Azure Key Vault auto-unseal

## Facts

Vault runs as an HA Raft cluster with 3 nodes

Auto-unseal uses Azure Key Vault

## Decisions

VAULT-001: Use Raft over Consul for HA backend
Why: Removes Consul as a hard dependency; Raft is bundled in Vault
Rejected: Consul cluster (operationally heavier)