# test/scenario/ — pyramid level 4

Full end-to-end scenarios: real curator binary, real MediaWiki (via testcontainers-go), real git, real Pi-harness. Run on release candidates and nightly CI.

## What goes here

Each scenario file is one named flow from `docs/engineering-principles.md §3.2 Level 4`:

- First render of a projection page
- Second render no-op (idempotency)
- Diff-driven re-render
- Human edit reconciliation (machine zone — overwrite with report)
- Human edit reconciliation (editorial zone — preserve)
- Editorial mode end-to-end
- KB maintenance PR flow
- Wiki API outage / retry
- Concurrent run / locking
- Spec validation failure
- Multi-wiki run

Build tag: `//go:build scenario`. Run with `make test-scenario`. Timeout `-timeout=30m` because containers + LLM replay add real time.

v0.0 status: empty. Scenarios land incrementally with each roadmap phase.
