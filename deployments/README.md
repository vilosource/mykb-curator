# deployments/ — test fixtures and runtime containers

This directory holds Dockerfiles and orchestration for the containers
the curator's tests (and eventual production deployment) rely on.

## What lives here

| Path | Purpose | Lifecycle |
|---|---|---|
| `mediawiki/` | Test MediaWiki fixture (SQLite-backed, disposable). Used by integration + scenario tests via testcontainers-go. | per-test-suite or local-by-hand |
| `pi-harness/` | Pi LLM harness — Pi installed + the `pi-wrapper` HTTP shim exposing `:8080`. Used by tests that exercise the `PiClient` LLM impl. | per-test-suite |

## Runtime vs test

- **Test fixtures** (this directory): containers tests spin up and tear down. Disposable, ephemeral, baked for fast startup.
- **Runtime image** (not yet — comes with the v0.1 PR): the production curator container that's deployed by users. Lives in the repo root `Dockerfile`.

## v0.0 status

Both `mediawiki/` and `pi-harness/` are **sketches**. They declare the file layout and the contract the test harness expects (image names, exposed ports, bot account, healthchecks) but the actual install + bootstrap steps are TBD until the adapter implementations land:

- MediaWiki bootstrap (DB tables + bot account creation) — lands with the MediaWiki adapter PR.
- Pi install + batch-mode flags — lands with the PiClient PR.

The walking-skeleton integration test does **not** rely on either container. It uses in-process fakes so the test infra works before either fixture is real.
