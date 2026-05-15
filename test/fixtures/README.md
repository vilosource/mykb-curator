# test/fixtures/ — shared test data

| Subdir | What lives there | Generated how |
|---|---|---|
| `kb/` | Snapshot kb directories (areas + JSONL entries) used by integration + contract tests. | Hand-authored or `kb-export` against a dev brain; committed. |
| `specs/` | Sample spec files in the format the spec engine consumes (markdown + frontmatter). | Hand-authored; committed. |
| `llm/` | Recorded LLM responses keyed by sha256 of `(model | prompt | system | max_tokens)`. Used by `internal/llm.ReplayClient`. | Regenerated via `make refresh-llm-fixtures` (TBD) using `RecordingClient` against a real provider; reviewed on the PR that introduces them. |
| `golden/` | Expected outputs for golden-file tests (rendered wikitext, IR JSON dumps, run reports). | Regenerated via `go test -update`. Reviewed by humans on the PR that changes them. |

## Conventions

- One file per fixture; filename is the test or scenario it serves.
- Keep fixtures small — readability matters more than realism. A 12-row table is better than a 1000-row dump.
- Commit fixtures in the same PR as the test that uses them. A test without its fixture is broken; a fixture without a test is dead code.
- LLM fixture regeneration is **not free** — costs tokens and time. Avoid casual `make refresh-llm-fixtures`; treat it like a database migration.
