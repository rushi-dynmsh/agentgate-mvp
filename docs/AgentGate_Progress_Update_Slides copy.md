<!-- agentgate-platform/docs/AgentGate_Progress_Update_Slides copy.md -->
---
marp: true
title: AgentGate — Progress Update
paginate: true
---

# From credential sharing to governing AI agents

What changed since the first presentation, why, and what's built so far.

> Request → Approve → Grant → Act → Audit — the same governance loop, now scoped to AI agents calling tools in production.

---

## Recap — The Original Idea

**AccessGraph: unified credential governance**

Engineering teams share production credentials informally — Slack messages, shared vaults, pasted `.env` files. No audit trail, no approval record, no idea who has access to what.

- **Frictionless access** — requesting a credential the right way should be faster than the informal path, not slower
- **Time-bounded windows** — grants expire by default; nothing lives forever in a shared vault
- **Access lineage** — a queryable graph of who approved whom, for what, and when

Framed as serving both human developers and AI agents — agents were positioned as the higher-urgency case, not the only case.

---

## Sharpening the Focus

The scope was narrowed — on purpose.

After the first round of feedback, the scope was narrowed: stop covering human credential sharing generally, and focus entirely on **AI agents calling tools in production**.

> The blocker isn't capability — it's that security and risk teams have no way to answer, and prove: what is this agent allowed to do, on whose behalf, was a high-risk action approved, and where is the record?

That reframing became the product's name and its test: an enforceable authorization + audit boundary an agent must pass through — not another dashboard.

---

## What We Did With That

Research first, then a working proof of concept.

Two parallel tracks: write down the design honestly (including what's unproven), and build enough of it to see the approval flow actually work end-to-end.

| Track | What it covers |
|---|---|
| Idea Brief | The narrowed scope, captured as concrete v1 requirements |
| Technical Architecture | Component design, identity model, policy engine choice, approval protocol |
| Verification Log | Every non-obvious claim checked against primary sources before it could drive a decision |
| Execution Plan + POC | Phase-by-phase build, each phase gated on a working demo, each its own commit |

---

## The Finding That Changed the Plan

The plan was to build a proxy. One already exists — and it's ahead of us.

**agentgateway** — an open-source MCP/A2A proxy, originally built by Solo.io, donated to the Linux Foundation's Agentic AI Foundation, the same body that now governs MCP itself.

- **Already solved** — transport, routing, TLS, basic auth, rate-limiting for MCP traffic, for free
- **Backed at scale** — Microsoft, AWS, Cisco, IBM, Red Hat, and others, verified against the primary announcement
- **Already extensible** — a documented hook that hands allow/deny decisions to an external service, per call

---

## The Decision

Build the governance layer, not the gate.

| | Build our own proxy from scratch | Extend an existing gateway |
|---|---|---|
| **Effort** | Custom parsing, transport, TLS, routing — months, before any governance logic exists | Governance layer only — the hard infra is already solved and maintained |
| **Credibility** | Competing with a Linux-Foundation project backed by major vendors | Interoperating with the category standard as it consolidates |
| **Differentiation** | Diluted — most effort goes to commodity plumbing | Sharp — all effort goes to identity, policy, approval, audit |

> Decision: AgentGate runs **behind** the existing open gateway, as the governance layer it deliberately doesn't provide.

---

## Architecture — Where AgentGate Sits

```
AI Agent Host  →  Open MCP Gateway  →  AgentGate (governance)  →  Real Tool / MCP Server
                   (not built by us)      ↓ allow / deny / forwarded only
                                       pending → human approval
```

- **Identity / on-behalf-of** — which agent, acting as which human — resolved from token claims, not trusted blindly
- **Policy engine** — schema-validated, formally-verified core; readable by risk/compliance, not just engineers
- **Relationship graph** — reserved for delegation and ownership chains only, not flat role checks

Decision → **allow** forwards · **deny** stops it at the gate · **pending** parks it for a human, with a re-check before it's ever forwarded.

---

## Proof of Concept — Status

All eight phases built and verified.

| Phase | What | Status |
|---|---|---|
| P0 | Skeleton — client → gateway → toy server, no governance yet | ✅ Done |
| P1 | Gateway calls AgentGate before every tool call | ✅ Done |
| P2 | Real identity — tokens carrying agent + on-behalf-of human | ✅ Done |
| P3 | Real decisions — role-based policies, destructive actions flagged | ✅ Done |
| P4 | Audit log — every decision written to an immutable table | ✅ Done |
| P5 | Human approval — async approval flow with a pre-execution re-check | ✅ Done |
| P6 | Relationship graph — per-record ownership under the role check | ✅ Done |
| P7 | Dashboard — live audit log, pending-approval queue, ownership graph | ✅ Done |

---

## Rigor

We didn't just trust the AI-written draft.

Every non-obvious claim in the technical doc — a competitor, a benchmark, a protocol feature — went through an explicit verification pass before it could shape the architecture.

- ✅ The open gateway is real, active, and governed by a credible standards body — checked against the primary announcement, not a summary
- ✅ Its external-authorization hook genuinely supports allow/deny decisions from an outside service — a real third party already uses it in production-style demos
- ⚠️ That hook is binary-only — no native "pause for a human" state — confirmed as a hard constraint the approval flow has to work around
- ❓ Specific benchmark decimals could not be confirmed against a primary source — flagged, not quoted as fact

---

## Open Risks — Named, Not Hidden

- **Approval race window** — approval and execution happen minutes apart; state can change in between. Mitigated with a pre-forward re-check and a short approval expiry — narrowed, not eliminated, and we say so.
- **Underlying protocol extension still stabilizing** — the async approval design leans on a still-maturing spec feature; a custom fallback exists in case the timeline slips.
- **On-behalf-of propagation has no single standard yet** — default path: enterprise identity-provider token exchange; fallback: a thin SDK for hosts that can't do a full exchange.
- **Category is consolidating fast** — multiple new entrants and at least one acquisition in the space in the first week of July 2026 alone. Speed to a real design partner matters more than more research.

---

## Roadmap — Next, In Order

1. **Real approval-channel integration** — code is ready; needs a bot token and a public endpoint
2. **Hosted control plane** — policy and dashboard centrally, enforcement stays inside the customer's network
3. **Compliance export** — regulatory logging/oversight mapping as a query over the existing audit table
4. **Design partner** — validate every stage above against one real customer scenario, not in the abstract

---

## What We Need To Keep Moving

- Sign-off to keep building on the existing open gateway as the foundation, rather than a from-scratch proxy
- Time to line up one real design-partner scenario — the open questions in this deck resolve fastest against a real integration, not more research
- A decision on deployment posture (hosted control plane vs. fully self-hosted) — it shapes the next two build phases

> The gate is the product. Everything else — dashboards, breadth, more frameworks — is roadmap.
