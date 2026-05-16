# Wiki-Curation Adoption — Direction & Open Issues

**Status: discussion record. NOT a design doc.** It captures the
intent and the open issues we are working through *one at a time*.
Each issue gets a Resolution note appended as we settle it; no
solutions are pre-committed here.

Last updated: 2026-05-16.

---

## 1. The intent

Today humans are the curator: read mykb, walk each wiki page by
hand, judge "still true? / missing? / should go?", hand-edit the
wiki. Manual, doesn't scale, drifts between sweeps.

The intent is to invert this: **mykb is the source of truth;
mykb-curator is the standing curator that projects it into the
Optiscan wiki.** Humans move from *doing* curation to **authoring
specs + reviewing proposals**. Goal: parity with today's pages
first, then improvement (consistent, beginner-first, diagrammed,
always fresh). Motivating target: the Entra-gated
`wiki.optiscangroup.com` (e.g. the Azure_Infrastructure page) —
contents are NOT copied; the knowledge already lives in mykb.

## 2. The fact-checking-agent direction

Generalise "ask a human to read-only-probe the infra and confirm a
fact" into a **reality-probe maintenance check**: an agent gathers
primary evidence from live systems + IaC and emits a
`MutationProposal` (verify / deprecate / add) *with the raw evidence
attached* → PR → human reviews the evidence (not the verdict). Maps
to the already-designed-but-unbuilt DESIGN §6.1 "System reality
drift" check; reuses the existing MutationProposal IR, funding gate
(§6.4), PR backend, run-report sinks. Composes with the Judge:
reality-probe = evidence gathering; Judge = reasoning/quality.

## 3. Open issues — to discuss one by one

Status legend: **OPEN** (not yet discussed) · **DISCUSSING** ·
**RESOLVED** (with a one-line outcome).

1. **Output-correctness assurance (LLM-as-Judge).** Editorial pages
   are LLM-generated, currently single-pass + graceful-degrade
   floor; an authoritative wiki needs the Judge loop. — **OPEN**
2. **Grounding / hallucination boundary.** Enforce "no
   organisation-specific claim without a kb source"; provenance
   markers exist but are not enforcement. — **OPEN**
3. **Source-of-truth discipline (behavioural, not tooling).** The
   model only works if the team writes knowledge to mykb, not the
   wiki; otherwise the curator amplifies a stale brain. — **OPEN**
4. **Brain-vs-reality drift.** Keeping the wiki consistent with the
   brain ≠ keeping the brain consistent with reality; the
   fact-checking agent (§2) targets this. — **OPEN**
5. **Fact-checking agent safety.** "Read-only" is load-bearing:
   allow-listed verbs, scoped read-only identity, never
   `terraform apply`, `plan`/`kubectl exec` are not innocuous;
   typed scoped probes vs. raw shell. Highest blast radius in the
   system. — **OPEN**
6. **Agent reliability / evidence.** An LLM reading az/kubectl
   output can be wrong; proposals must carry primary evidence
   (exact command + raw output + timestamp); humans review evidence,
   not the verdict. — **OPEN**
7. **Probe-eligibility of knowledge.** Empirical facts (versions,
   topology, what's deployed) are checkable; decisions/rationale/
   intent are not. Scope the check (tags/zones) to avoid false
   deprecations on judgement-type knowledge. — **OPEN**
8. **Credentials, org-asset separation, Entra boundary.** The
   curator needs wiki-bot creds and (for §2) cloud read creds,
   under the Optiscan vs viloforge separation rule and the Entra
   App Proxy in front of the wiki. Where it runs, what identity,
   audit. — **OPEN**
9. **Non-determinism & cost.** Agentic exploration is variable and
   token/time-heavy; belongs behind the funding gate, bounded,
   cached, scheduled — not every render. — **OPEN**
10. **Reconciliation, trust, change management.** Direct wiki edits
    cause drift the reconciler only flags (3-way merge is v2+);
    moving to machine-regenerated pages is a trust/process shift
    needing phased rollout + review cadence. — **OPEN**
11. **Access path to the target wiki.** `wiki.optiscangroup.com`
    sits behind Entra ID App Proxy (verified: 302 →
    login.microsoftonline.com); the MediaWiki adapter needs a
    bot/API route not behind interactive SSO, or to run inside the
    boundary. — **OPEN**

## 4. Discussion log

(Append dated notes here as each issue is discussed; update the
status + Resolution line above.)
