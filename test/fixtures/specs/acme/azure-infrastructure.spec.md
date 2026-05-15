---
wiki: acme
page: Azure_Infrastructure
kind: editorial
version: 1
include:
  areas: [networking, vault, harbor, gitlab, hetzner, dr, iac, observability]
  workspaces: [dr, hetzner]
  exclude_zones: [incoming, archived]
fact_check:
  link_rot: every-run
  external_truth: quarterly
protected_blocks: [executive-summary]
---

Azure Infrastructure hub page. Cover:

- Core infrastructure foundations: tenant + subscriptions, identity,
  networking (hub-and-spoke + WG S2S), compute platforms, DR, backups
- Platform service automation: wildcard SSL, observability, cost
  management, IaC conventions
- Infrastructure service stacks: vault, traefik, gitlab, runners,
  harbor, nexus, sonarqube, mediawiki, event-router

Maintain a sidebar of operational runbooks pulled from the
infra-runbooks area.
