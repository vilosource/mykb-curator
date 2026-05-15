---
title: Area/Vault_Architecture
spec_hash: d7850192396dc361e26b2613f5d1aafabb22dbffec7508ee021d431d03eb7e04
---

# Area/Vault_Architecture

## Vault Architecture

<!-- CURATOR:BEGIN block=s0-b0 zone=editorial provenance= -->

Centralised secrets manager — HashiCorp Vault on Raft HA cluster with Azure Key Vault auto-unseal

<!-- CURATOR:END block=s0-b0 -->

## Facts

<!-- CURATOR:BEGIN block=s1-b0 zone=editorial provenance=d7850192396dc361e26b2613f5d1aafabb22dbffec7508ee021d431d03eb7e04:vault-f1: -->

Vault runs as an HA Raft cluster with 3 nodes

<!-- CURATOR:END block=s1-b0 -->

<!-- CURATOR:BEGIN block=s1-b1 zone=editorial provenance=d7850192396dc361e26b2613f5d1aafabb22dbffec7508ee021d431d03eb7e04:vault-f2: -->

Auto-unseal uses Azure Key Vault

<!-- CURATOR:END block=s1-b1 -->

## Decisions

<!-- CURATOR:BEGIN block=s2-b0 zone=editorial provenance=d7850192396dc361e26b2613f5d1aafabb22dbffec7508ee021d431d03eb7e04:vault-d1: -->

VAULT-001: Use Raft over Consul for HA backend
Why: Removes Consul as a hard dependency; Raft is bundled in Vault
Rejected: Consul cluster (operationally heavier)

<!-- CURATOR:END block=s2-b0 -->