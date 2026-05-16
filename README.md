# mykb-curator

Maintains human-facing wikis as curated, continuously-updated projections of a [mykb](https://github.com/vilosource/mykb) brain.

**Status:** v0.6 complete (as of 2026-05-16) — walking skeleton, editorial mode + KB maintenance, editorial reconcile + spec-hash cache all landed and CI-green across the full four-level test pyramid. Next: v1.0 production hardening (see [`docs/DESIGN.md` §17](docs/DESIGN.md#17-roadmap)).

**Language:** Go 1.25.

## What it is

mykb is the canonical brain — machine-shaped: JSONL + SQLite, optimised for LLM agents to read and write. Wikis are how humans browse the same knowledge. `mykb-curator` bridges them.

It reads specs that declare what each wiki page should be, reads the kb, synthesises pages via a compiler-style pipeline, pushes them to the wiki, and reconciles human edits when they happen.

## Architecture in one paragraph

Three pluggable backends (KB-source, Spec-store, Wiki-target) wrapped around two pipelines (Page Rendering, KB Maintenance). Each pipeline is compiler-shaped: frontend → IR → passes → backend. Intelligence is localised to the frontend; passes and backends are deterministic and testable. Wikis are soft-read-only — humans can edit; the curator detects drift and surfaces it in a structured run report.

## Docs

- [`docs/DESIGN.md`](docs/DESIGN.md) — architecture: principles, C4 diagrams, IR model, two-zone reconciliation, multi-tenancy, roadmap.
- [`docs/engineering-principles.md`](docs/engineering-principles.md) — engineering north star: SOLID concretized for this codebase, design-pattern map, TDD discipline, four-level testing pyramid up to scenario tests. Non-negotiable for every PR.

## Building

```bash
make build      # builds bin/mykb-curator and bin/pi-wrapper
make test       # unit tests (the dev inner loop)
make test-all   # full pyramid (containers required for integration+)
```

Requires Go 1.25+. Container-based test levels (integration / scenario) require Docker.

## License

MIT — see [LICENSE](LICENSE).
