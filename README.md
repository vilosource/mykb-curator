# mykb-curator

Maintains human-facing wikis as curated, continuously-updated projections of a [mykb](https://github.com/vilosource/mykb) brain.

**Status:** Design phase — see [`docs/DESIGN.md`](docs/DESIGN.md). No code yet.

## What it is

mykb is the canonical brain — machine-shaped: JSONL + SQLite, optimised for LLM agents to read and write. Wikis are how humans browse the same knowledge. `mykb-curator` bridges them.

It reads specs that declare what each wiki page should be, reads the kb, synthesises pages via a compiler-style pipeline, pushes them to the wiki, and reconciles human edits when they happen.

## Architecture in one paragraph

Three pluggable backends (KB-source, Spec-store, Wiki-target) wrapped around two pipelines (Page Rendering, KB Maintenance). Each pipeline is compiler-shaped: frontend → IR → passes → backend. Intelligence is localised to the frontend; passes and backends are deterministic and testable. Wikis are soft-read-only — humans can edit; the curator detects drift and surfaces it in a structured run report.

For the full design — principles, C4 diagrams, IR model, two-zone reconciliation, multi-tenancy, roadmap — see [`docs/DESIGN.md`](docs/DESIGN.md).

## License

MIT — see [LICENSE](LICENSE).
