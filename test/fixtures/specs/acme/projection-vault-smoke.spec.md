---
wiki: acme
page: Test/Vault_Projection
kind: projection
version: 1
include:
  areas: [vault]
  workspaces: linked-to-areas
  exclude_zones: [incoming]
---

Deterministic projection of the vault area. This is the L4
projection smoke fixture (page under Test/ on purpose) — it keeps
projection→real-MediaWiki coverage now that area-vault.spec.md is
an editorial page. Do not delete without retargeting
first_render_test.go.
