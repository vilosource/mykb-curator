# test/contract/ — pyramid level 3

Contract tests: verify that interface contracts hold across every implementation, and that schemas (spec, IR) don't drift.

## What goes here

- `WikiTargetContractSuite` — run against every `WikiTarget` impl (mediawiki, markdown, future confluence). All impls must pass identical behavioural assertions.
- `KBSourceContractSuite` — same for `kb.Source` impls.
- `SpecStoreContractSuite` — same for `specs.Store`.
- Spec-schema and IR-schema validators against fixture files.

Build tag: `//go:build contract`. Run with `make test-contract`.

v0.0 status: empty. Suites land alongside each adapter implementation in v0.1+.
