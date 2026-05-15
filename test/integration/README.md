# test/integration/ — pyramid level 2

Real components talking to real components — but in-process or via testcontainers-go, not full end-to-end scenarios.

Build tag: `//go:build integration`. Run with `make test-integration`.

## v0.0 contents

- `orchestrator_skeleton_test.go` — walking-skeleton test that wires the Orchestrator with in-process fakes for every dependency. Proves the seams compile and the test harness works before any concrete adapter exists.

## v0.1+ contents (planned)

- Full Page Rendering pipeline against a fixture kb (ReplayClient, in-memory wiki)
- `GitKBSource` against a real local git repo (no container needed)
- `MediaWikiBackend` rendering against a real MediaWiki container (testcontainers-go)
- Cache round-trip (bbolt on real fs)
- Reconciler end-to-end against a real test MediaWiki page
- KB Maintenance PR flow against a local Gitea (testcontainers-go)
