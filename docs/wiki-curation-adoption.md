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
   wiki; otherwise the curator amplifies a stale brain. —
   **RESOLVED** (2026-05-16): accepted model — page genesis =
   human+agent architect the page AND backfill knowledge into mykb
   (always step 0, intrinsic to authoring, not a separate project);
   mykb-curator's page pipeline keeps the wiki a projection;
   mykb's built-in freshness/validity metadata + the maintenance
   pipeline (staleness/link-rot/external-truth → MutationProposal →
   PR, nightly) self-curate the brain. Human role = architect new
   pages + review proposals; NOT a manual brain-curator. The
   "backfill might be a surprise project" concern is dissolved.
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

### 2026-05-16 — Issue #3 (source-of-truth discipline) — RESOLVED

Adoption model agreed: every curated wiki page starts with a
human+agent **architecting pass that also backfills the knowledge
into mykb**; from then on mykb-curator's page pipeline keeps the
wiki a projection of the brain. Brain freshness is itself
automated, by original design: `kb.Entry` carries
`Zone`/`Created`/`Updated`/`Provenance.Status`
(verified|unverified); the maintenance pipeline (StalenessCheck,
LinkRotCheck, ExternalTruthCheck) already turns that into
MutationProposals → PR via prbackend, intended to run nightly. So
the brain self-curates; the human architects new pages + reviews
proposals.

Two corrections recorded for faithfulness:
- The earlier "Vault demo proves a brain-vs-wiki richness gap"
  claim is **withdrawn**: that demo ran against the synthetic
  `test/fixtures/kb/acme` fixture (deliberately ~3 facts), never
  the real `~/.mykb`; it is not evidence about the real brain.
- The "human becomes the brain-curator" framing was **wrong**:
  brain curation is the maintenance pipeline's job (built, v1.0),
  not a new manual human burden. Adoption *relocates* effort to
  page-architecting + proposal review, and shrinks it further as
  the maintenance/Judge/reality-probe loops mature.
