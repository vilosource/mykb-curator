# Reality-probe execution policy — DESIGN (slice 4b / adoption issue #12)

> Status: **DESIGN, awaiting approval** (design-first; no code until the
> open decisions in §7 are settled). Scope: the `cmd:` `ssh:` `az:`
> doc-spec source schemes. `git:` (read-only, offline, local) shipped
> in slice 4 and is explicitly out of scope here.

## 1. Problem

A doc-spec section may declare a non-kb source whose scheme is
`cmd:`, `ssh:` or `az:` — a *reality probe*: a command run against
real infrastructure to ground a wiki section in observed truth
(e.g. `ssh:host=infra-dsm-1 vault status`, `az:keyvault show ...`).

Today these schemes have **no resolver**; the architecture frontend
renders them as the honest `pending — no resolver configured for
this scheme` row and never fabricates. Building a resolver that
*executes declared commands* is a real capability escalation: a
future LLM-authored spec or an automated CI run could cause arbitrary
command execution against production. This must be gated to the same
standard as the standing terraform/ansible posture: **deny by
default, read-only enforced, per-invocation human ACK**.

Relevant standing constraints (non-negotiable, from operator memory):
- "Verify" never means *run a command to see what happens*; probe
  execution is a deliberate, gated, audited action, never a routine
  side effect of a publish run.
- Every potentially-mutating apply needs explicit per-invocation human
  ACK; standing/phase approvals do **not** authorise. Reversibility
  does not lower the bar.
- Dry-run / review before any execution; audit the exact command
  before it runs.

## 2. Goals / non-goals

**Goals**
- Ship the policy engine + resolvers such that, with **zero config,
  behaviour is byte-identical to today** (honest pending; nothing
  executes).
- Capability is strictly opt-in, **per scheme**, **allowlisted
  (deny-by-default)**, **statically read-only-enforced**, and
  **per-invocation HITL**, with a full audit trail on the run report
  and block provenance.
- SOLID/extensible: one `sources.Resolver` per scheme; a single
  shared `ExecutionPolicy` decision seam; the normal render path
  never spawns a process.

**Non-goals (explicit deferrals / exclusions)**
- Any mutating command. Never in scope — outside the tool's purpose.
- `terraform plan`, `kubectl exec`, raw arbitrary `sh -c` — excluded
  by construction (adoption doc §, lines 78/86).
- Gate-mode (a probe failure blocking a push). Out of scope; the
  Judge stays report-only; probes follow the same observable-first
  posture.
- Network mutation, credential writing, interactive remote shells.

## 3. Architecture

```
docspec.Source{Scheme: cmd|ssh|az}
        │
        ▼
  ExecResolver(scheme)                 implements sources.Resolver
        │  Resolve(ctx, s)
        ▼
  ExecutionPolicy.Decide(scheme, parsed)   ← pure, no side effects
        │
        ├─ Disabled         → ok=false, nil   (honest pending — as today)
        ├─ NotAllowlisted   → hard error      (loud: misconfig/attack, never silent)
        ├─ MutatingRejected → hard error      (defence-in-depth vs allowlist)
        └─ Allowed(argv)    → ProbeRequest
                               │
                               ▼
                       ProbeCache.Get(key)
                         ├─ hit  → Resolved (no execution)
                         └─ miss → ok=false + "pending: authorised probe required"
                                   (NORMAL run never executes)
```

A separate, explicit, interactive **`mykb-curator probe`** path is
the only place a process is ever spawned (see §5). It populates
`ProbeCache`; the render run only ever *consumes* the cache.

### 3.1 Components (each its own TDD unit)

- `internal/sources/exec/policy.go` — `ExecutionPolicy`: pure
  `Decide(scheme string, p ParsedProbe) Decision`. Deny-by-default.
  Allowlist + static read-only classifier. No I/O. Heavily unit-tested
  incl. negative/adversarial cases (per probe-negative-cases memory).
- `internal/sources/exec/parse.go` — per-scheme parsers turning
  `docspec.Source.Spec` into a typed `ParsedProbe` (no string
  matching downstream; structured fields only).
- `internal/sources/exec/{cmd,ssh,az}.go` — three `sources.Resolver`
  impls. Each: parse → policy.Decide → cache lookup. They **never
  exec**; execution lives only behind the probe command.
- `internal/sources/exec/runner.go` — the only code that spawns a
  process: `exec.CommandContext` with an explicit argv (never a
  shell), context timeout, byte-capped output, no env inheritance of
  secrets beyond what the scheme needs. Mirrors `git.Resolver.out`.
- `internal/sources/exec/cache.go` — content-addressed
  `ProbeCache` keyed by `(scheme, canonical-argv, host)`; stores
  output + exit + timestamp + the ACKing principal + policy decision.
- `cmd/mykb-curator probe` — the HITL subcommand (§5).
- `internal/config`: new `SourcesConfig.Exec` block (§4); absent =
  every scheme disabled.

### 3.2 Why deny-by-default differs from git's `ok=false`

git: an unknown repo is *benign-absent* → `ok=false` → pending.
exec: a command **not on the allowlist** is a *policy violation*
(misconfigured or adversarial spec) → **hard error, abort the page**.
Silent degradation here would hide an attempt to run something
unsanctioned. Only a *disabled scheme* or a *cache miss for an
allowlisted command* yields the benign pending placeholder.

## 4. Config (deny-by-default; absent = today's behaviour)

```yaml
sources:
  git: { root: ... }            # unchanged
  exec:                         # entire block optional; absent = all schemes pending
    az:
      enabled: true
      allow:                    # structured, not substring
        - service: keyvault     # read verbs only — enforced statically too
          verbs: [show, list]
        - service: account
          verbs: [show]
    ssh:
      enabled: true
      hosts: ["infra-dsm-*.prod.optiscangroup.com"]
      commands:                 # exact argv templates, read-only
        - ["vault", "status"]
        - ["systemctl", "status", "vault"]
    cmd:
      enabled: false            # default
```

The allowlist is intentionally **committed configuration**, not
free-form per-page spec input — the spec declares *what to probe*,
the policy (reviewed, version-controlled) decides *whether it may*.

## 5. The HITL seam — `mykb-curator probe`

A normal `mykb-curator run`:
- exec sources with a **cache hit** → grounded normally.
- exec sources with a **cache miss** → honest pending row
  `pending — authorised reality-probe required (run: mykb-curator probe)`.
  No execution. An automated/CI publish is therefore always
  execution-free and safe.

`mykb-curator probe --wiki <w> [--spec <id>]`:
1. Resolves every exec source, runs `ExecutionPolicy.Decide`.
2. For each `Allowed` probe, prints the **exact argv + host + policy
   decision + scheme** and prompts `Run this read-only probe? [y/N]`
   — per-invocation HITL, one ACK per command (terraform-apply-grade;
   a single bulk "yes" is not offered).
3. Runs only ACKed probes via `runner.go` (read-only, timeout,
   byte-capped), writes results + the ACK trail into `ProbeCache`.
4. Never touched on `NotAllowlisted`/`MutatingRejected` — those are
   reported as policy errors, never prompted.

This mirrors `terraform plan` → human review → explicit `apply`
ACK, and keeps the render pipeline free of any execution authority.

## 6. Testing (full four-level pyramid)

- **unit**: `ExecutionPolicy.Decide` truth table incl. adversarial
  (mutating verb smuggled via casing/aliases, host-pattern escape,
  argv injection, allowlist bypass attempts); per-scheme parsers;
  cache key canonicalisation; runner argv-not-shell + timeout +
  byte-cap; the honest-pending vs hard-error split.
- **integration**: resolver→policy→cache miss yields pending, never
  executes; cache hit grounds; `NotAllowlisted` aborts the page.
- **contract**: each exec resolver satisfies `sources.Resolver`
  (read-only, never-fabricate, ok=false-vs-error semantics).
- **scenario**: a `.doc.yaml` with a cache-miss exec source publishes
  with the honest pending row and zero process spawned (asserted);
  a pre-seeded cache grounds the section. Probe-command HITL prompt
  is driven with a scripted decline + accept.

No scenario ever executes a real production command — probes are
exercised against a local fixture/echo binary; faithfulness flows
from the policy contract, never from a live host.

## 7. Open design decisions (need a call before coding)

1. **HITL mechanism** — (a) interactive `mykb-curator probe`
   subcommand that prompts per command and seeds the cache
   [recommended: explicit, auditable, render path stays
   execution-free], vs (b) operator runs probes fully out-of-band by
   hand and the tool only ever reads a cache it never writes.
2. **First-cut scope** — ship the policy engine + all three schemes,
   vs land the engine + the single safest scheme first
   (`az` read-verbs, or `ssh` fixed read-commands) and defer the
   others behind the same gate.
3. **Allowlist location** — committed repo policy file (versioned,
   reviewable) [recommended] vs per-tenant `personal.yaml`.
4. **Cache trust window** — does a cached probe expire (staleness ⇒
   re-ACK), and if so by time or by kb/spec change hash?

These are genuine forks; to be walked through conversationally
(one point at a time) per the design-discussion norm, not bundled.
```
