---
marp: true
title: AgentGate — Progress Update
paginate: true
---

# From credential sharing to governing AI agents

What changed since the first presentation, why, and where the plan stands now.

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

## What the Idea Actually Requires

For every tool call an agent makes, someone needs to be able to answer, and prove, four things:

1. **Identity** — which agent is acting, and as which human or service
2. **Authorization** — is this specific action allowed, scoped to that identity
3. **Human oversight** — does this action need a person to sign off before it happens
4. **Audit** — an immutable record of what was decided, by whom, and why

That's the whole product, at the idea level. Everything else is implementation detail in service of these four.

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
| **Differentiation** | Diluted — most effort goes to commodity plumbing | Sharp — all effort goes towards identity, policy, approval, audit |

> Decision: AgentGate sits **behind** the existing open gateway, as the governance layer it deliberately doesn't provide.

---

## Architecture — Where AgentGate Sits

```
AI Agent Host  →  Open MCP Gateway  →  AgentGate (governance)  →  Real Tool / MCP Server
                   (not built by us)      ↓ allow / deny / forwarded only
                                       pending → human approval
```

- **Identity / on-behalf-of** — which agent, acting as which human — resolved from token claims, not trusted blindly
- **Policy engine** — a schema-validated, formally-verified core; readable by risk/compliance, not just engineers
- **Relationship graph** — reserved for delegation and ownership chains only, not flat role checks

Every call resolves to one of three outcomes: **allow** (forwarded), **deny** (stopped at the gate), or **pending** (parked for a human, re-checked before it's ever forwarded).

---

## Components We Will Build

The gateway itself is not one of them — that's the existing open project. Everything below is ours.

| Component | Job |
|---|---|
| **Identity / OBO resolver** | Extracts and verifies which agent, acting as which human or service, made the call |
| **Policy engine integration** | Evaluates every call against allow / deny / needs-approval rules |
| **Relationship graph integration** | Resolves ownership and delegation questions a flat rule can't — "does this employee, through this agent, own this record" |
| **Approval orchestrator** | Runs the pause → notify a human → resume lifecycle, including re-checking state before resuming |
| **Audit store** | Writes one immutable record per decision |
| **Admin dashboard** | Policy authoring, pending-approval queue, lineage browsing |

---

## Tech Stack

| Layer | Technology | Why this one |
|---|---|---|
| MCP gateway | **agentgateway** (Rust, open source) | Already solves transport, routing, TLS, rate-limiting — not ours to rebuild |
| Integration point | **External-authorization callout** (gRPC, Envoy-compatible) | A documented hook agentgateway already exposes; a real third party already uses it this way in production |
| Identity / on-behalf-of | **OAuth 2.1 / OIDC token exchange** against the enterprise identity provider, with a thin SDK as fallback | Matches how enterprises already do on-behalf-of flows for other internal apps — no bespoke protocol to adopt |
| Policy engine | **Cedar** (AWS's open-source authorization language) | Formally-verified core, schema-validated, readable by risk/compliance staff — not just engineers |
| Relationship graph | **SpiceDB** (Zanzibar-style) | Used narrowly — only for delegation and ownership chains, not as a general permission store |
| Approval orchestrator | Durable task record + **Slack / Teams** for the human step | Avoids holding a live connection open for however long a person takes to respond |
| Audit store | **PostgreSQL**, append-only table | One system of record; a compliance export is a view over it, not a second pipeline |
| Admin dashboard | Standard web UI | Policy authoring + approval queue + lineage graph, nothing exotic |

---

## Deployment Model

Three ways to run it — in increasing order of how much lives inside the customer's own network:

| Model | What runs where | Trade-off |
|---|---|---|
| Fully self-hosted | Everything, including the gateway, inside the customer's network | Highest trust, highest support burden |
| **Hosted control plane, on-prem enforcement** | Policy authoring, dashboard, audit store hosted centrally; the actual allow/deny decision runs inside the customer's network | Sensitive traffic and resource state never leave the customer's side — the recommended target |
| Fully hosted | All traffic routes through our cloud | Fastest to build, hardest to sell to a buyer sophisticated enough to be evaluating this category |

---

## Validating the Idea

Before committing further, the idea needed a sanity check: does this actually work end to end, or does it fall apart in practice?

A small proof-of-concept was built to test exactly that — not a product, just a way to see the core loop run for real: an agent's call gets identified, checked against policy, either allowed, blocked, or parked for a human, and every outcome lands in an audit trail.

**The result: the approach holds up.** The loop works end-to-end, which is what justifies moving from "idea on paper" to a real plan with real next steps.

---

## Rigor

We didn't just trust the AI-written research draft.

Every non-obvious claim in the research — a competitor, a benchmark, a protocol feature — went through an explicit verification pass before it could shape the plan.

- ✅ The open gateway is real, active, and governed by a credible standards body — checked against the primary announcement, not a summary
- ✅ Its external-authorization hook genuinely supports allow/deny decisions from an outside service — a real third party already uses it in production-style demos
- ⚠️ That hook is binary-only — no native "pause for a human" state — confirmed as a hard constraint the approval flow has to work around
- ❓ Specific benchmark decimals could not be confirmed against a primary source — flagged, not quoted as fact

---

## Open Risks — Named, Not Hidden

- **Approval race window** — approval and execution happen minutes apart; state can change in between. Mitigated with a pre-forward re-check and a short approval expiry — narrowed, not eliminated, and we say so.
- **Underlying protocol extension still stabilizing** — the async approval design leans on a still-maturing spec feature; a fallback approach exists in case the timeline slips.
- **On-behalf-of propagation has no single standard yet** — default path: enterprise identity-provider token exchange; fallback: a thin SDK for hosts that can't do a full exchange.
- **Category is consolidating fast** — multiple new entrants and at least one acquisition in the space in the first week of July 2026 alone. Speed to a real design partner matters more than more research.

---

## Implementation Plan — Build Sequence

Each stage ships against one real scenario, not in the abstract, and is gated before the next one starts.

1. **Gateway integration + identity resolution** — stand up the open gateway in front of one real MCP server; wire identity/OBO resolution into its authorization callout
2. **Policy engine** — embed Cedar; flat role/permission checks first, relationship graph deferred
3. **Audit log** — every decision, even a trivial "allowed", writes to the immutable log from day one
4. **Relationship graph** — add SpiceDB once there's a concrete delegation/ownership scenario that needs it
5. **Async approval + re-check** — build the pause/notify/resume flow, with the pre-forward state re-check as a required part of this stage, not an add-on later
6. **Dashboard** — policy authoring, pending-approval queue, lineage browser

**Alongside the build:** a compliance export (regulatory logging/oversight mapping) as a query over the audit log, and a design partner to validate the sequence against real integration constraints.

---

## What We Need To Keep Moving

- Sign-off to keep building the plan on top of the existing open gateway, rather than a from-scratch proxy
- Time to line up one real design-partner scenario — the open questions in this deck resolve fastest against a real integration, not more research
- A decision on deployment posture (hosted control plane vs. fully self-hosted) — it shapes what gets prioritized next

> The gate is the product. Everything else — dashboards, breadth, more frameworks — is roadmap.
