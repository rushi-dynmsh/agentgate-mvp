# AgentGate
## The Authorization, Approval, and Audit Layer for AI Agents

**A Product Whitepaper**
**Author:** Rushikesh Surve
**Date:** July 2026
**Status:** Concept whitepaper — synthesizes management's product brief, prior research on AccessGraph, and independent technical validation. Baseline for engineering and go-to-market planning.

---

## Table of Contents

**Part 1 — The Vision**
1. [Executive Summary](#1-executive-summary)
2. [From AccessGraph to AgentGate: Why the Pivot Makes Sense](#2-from-accessgraph-to-agentgate-why-the-pivot-makes-sense)
3. [The Problem: Enterprises Can Build Agents, They Can't Ship Them](#3-the-problem-enterprises-can-build-agents-they-cant-ship-them)
4. [What AgentGate Is](#4-what-agentgate-is)

**Part 2 — The Model**
5. [How AgentGate Works](#5-how-agentgate-works)
6. [Two Deployment Scenarios](#6-two-deployment-scenarios)
7. [Regulatory Alignment: The EU AI Act](#7-regulatory-alignment-the-eu-ai-act)
8. [What AgentGate Is Not](#8-what-agentgate-is-not)

**Part 3 — The Reality Check**
9. [Honest Limitations](#9-honest-limitations)
10. [Competitive Landscape and B2B Feasibility](#10-competitive-landscape-and-b2b-feasibility)
11. [v1 Scope and Roadmap](#11-v1-scope-and-roadmap)
12. [Open Questions Requiring Decisions](#12-open-questions-requiring-decisions)

*A companion document, [AgentGate_Technical_Architecture.md](./AgentGate_Technical_Architecture.md), covers implementation-level detail: proxy design, identity propagation, policy schemas, and the async approval protocol.*

---

# Part 1 — The Vision

## 1. Executive Summary

Enterprises have spent the last two years building AI agents that can query databases, call internal APIs, and take real actions — not just answer questions. The engineering problem is largely solved. What is not solved is the governance problem: **security and risk teams cannot answer, and cannot prove, what an agent is allowed to do, on whose behalf, whether a risky action was approved by a human, and where the record of that approval lives.** Until those four questions have provable answers, agents stay in pilot. They do not go to production.

AgentGate is a control plane that sits at the point where an AI agent calls a tool — typically via the Model Context Protocol (MCP), which has become the standard way agents connect to databases, APIs, and internal systems. At that interception point, AgentGate:

- Identifies the agent and the human (or system) it is acting on behalf of.
- Checks the requested action against a policy: allow, deny, or require human approval.
- Routes anything requiring approval to a human reviewer in Slack or Teams, resolved in seconds to minutes, without breaking the agent's execution loop.
- Writes an immutable, structured record of every decision — the basis for both internal audit and external compliance reporting.

This is the same governance pattern — relationship-based access, human approval, immutable lineage — that was originally designed for engineering credential sharing under the AccessGraph concept. AgentGate is that same underlying model, redirected at a different, more urgent buyer problem: getting AI agents past the enterprise risk committee.

This whitepaper lays out what AgentGate is, how it would work technically, where it sits relative to a genuinely crowded and fast-moving market of "MCP gateway" competitors, what it honestly cannot do, and what a realistic v1 looks like. The short version: **the problem is real and the timing is right, but AgentGate is entering a market that already has several funded, shipping competitors covering similar ground — differentiation has to come from doing the relationship-graph-plus-human-approval part deeper and better, not from being first.**

---

## 2. From AccessGraph to AgentGate: Why the Pivot Makes Sense

AccessGraph, as originally conceived, governed how engineers share production credentials with each other — who approved whom, for how long, with what audit trail. It was built around three pillars: frictionless authorized access, time-bounded access windows, and a queryable relationship (ReBAC) lineage graph, backed by PostgreSQL for metadata and SpiceDB for the relationship graph.

Management's redirection toward AgentGate keeps all three pillars but changes the actor and the trigger. Instead of *a developer requesting a database password*, the actor is *an AI agent invoking a tool*, and the trigger is *every single tool call*, not an occasional credential request. This is a meaningful shift, not a cosmetic one:

| | AccessGraph (original) | AgentGate (pivot) |
|---|---|---|
| **Who is acting** | A human developer, occasionally a machine identity | An AI agent, acting non-deterministically |
| **Frequency of the governed event** | Occasional (a developer requests access a few times a week) | Continuous (every tool call in an agent's reasoning loop) |
| **What's being protected** | A specific credential value | An action against a live production system |
| **Why access might be misused** | A person deliberately shares or retains a credential | A model is manipulated (via prompt injection) into taking an action nobody intended |
| **What "approval" looks like** | A one-time grant with a time window | A per-action or per-session policy decision, often needing to resolve in seconds inside an agent's execution loop |

The reused infrastructure — a relationship graph engine, a policy engine, an approval workflow, an immutable audit log — is genuinely portable. What is **not** portable without real design work is the interaction model: AgentGate has to make policy and approval decisions at the speed and volume of machine-driven tool calls, not human-driven credential requests. That is a harder, more latency-sensitive engineering problem, and it is the main reason this cannot be treated as "AccessGraph with different words on the landing page."

The market timing argument for the pivot is sound: credential sharing is a real but slow-moving, already-served problem (Vault, Doppler, Teleport, Opal all exist and are mature). AI agent governance is a fast-moving, urgent, currently underserved-at-depth problem where enterprises are actively blocked from shipping. That is a legitimate reason to redirect a small team's roadmap toward it.

---

## 3. The Problem: Enterprises Can Build Agents, They Can't Ship Them

The Model Context Protocol (MCP) has become the de facto standard connecting AI agents to external tools and data — adopted across Anthropic, OpenAI, Google, and Microsoft's agent ecosystems, and now governed by the Agentic AI Foundation under the Linux Foundation. That standardization is exactly what has made agentic AI practical to build quickly. It has not made it safe to run unsupervised.

Independent security research through 2025 and 2026 has documented a consistent, narrow set of failure modes in MCP deployments:

- **Prompt injection and tool poisoning.** Because an agent's tool-calling decisions are driven by whatever text it has ingested — including untrusted content like emails, web pages, or tool descriptions themselves — an attacker who can influence that text can often steer the agent into calling a tool it was never meant to call. Security researchers (Invariant Labs, Simon Willison, and others) have demonstrated this class of attack repeatedly since April 2025, and OWASP now tracks it as a named risk category for agentic applications.
- **The confused deputy problem.** Agents are frequently wired to a single, broadly-privileged service account rather than the narrower rights of the specific human they're acting for. When the agent is manipulated, the blast radius is the service account's full privilege set, not the individual user's.
- **Weak or absent authentication and audit.** Independent scans of public MCP servers through 2025–2026 have repeatedly found large fractions with no authentication by default, command-injection flaws, and plaintext credential handling — the tooling ecosystem grew faster than its security practices did.

None of this is exotic. It is the standard shape of a new integration layer growing faster than its guardrails — the same pattern seen with early REST APIs and early OAuth deployments. What makes it urgent for enterprises specifically is that **security, risk, and legal teams have veto power over production deployment, and today they have no way to exercise informed judgment.** Observability tools tell you what an agent did after the fact. They do not tell you what it is currently authorized to do, and they cannot stop the action before it happens.

That is the actual product gap: not "can we watch our agents" (increasingly, yes) but **"can we enforce and prove a boundary around what they're allowed to do, on whose authority, with a human check on the risky parts."** That enforceable, provable boundary is what AgentGate sells.

---

## 4. What AgentGate Is

At the moment an agent calls a tool through an MCP server, AgentGate provides four things:

### Identity and on-behalf-of context
Every action is tied to *which agent* is acting *as which human or service*. This is the piece that turns "the sales-agent service account did something" into "Bob's agent, acting on Bob's authority, did something" — the distinction that determines both the correct scope of authorization and who is accountable afterward.

### Scoped, relationship-based authorization
Rather than a flat allow-list per agent, access is modeled as a relationship graph: this agent, acting for this user, has this relationship to this resource, which permits this class of action. Combined with fast, attribute-level rule checks (time of day, transaction size, environment), this lets a policy answer both "is this kind of action ever allowed" and "is this specific relationship in place right now."

### Human-in-the-loop for the risky fraction
Most tool calls are routine and should never reach a person. The minority that are flagged as high-risk — a production data mutation, a payment, a destructive operation — pause and route to a designated human approver over Slack or Teams, with enough context to make a real decision, resolving in seconds to minutes without holding the agent's connection open indefinitely.

### Immutable audit and lineage
Every decision — allowed, denied, escalated, approved, expired — is written to an audit log that answers, for any action: *which agent, on whose behalf, against what resource, under what policy version, approved by whom, at what time.* The same lineage-graph approach from AccessGraph applies here: a security team can ask "if we revoke this agent's access, what breaks" and get a real answer instead of a multi-day investigation.

These four pillars mirror AccessGraph's original three (frictionless access, time-bounded windows, lineage graph) with one addition specific to agents: **the policy decision itself has to happen inline, in the path of a non-deterministic actor, fast enough not to break the user experience.** That is the crux of the engineering problem and the subject of the technical companion document.

---

# Part 2 — The Model

## 5. How AgentGate Works

At a conceptual level, AgentGate is a proxy that sits between the AI agent's MCP client and the MCP server that actually holds the tools:

```
AI Agent (MCP Client)
        │
        ▼
   AgentGate Proxy  ──── Identity & OBO context attached
        │
        ├──► Policy check (Cedar-style rules): allow / deny / needs-approval
        ├──► Relationship check (SpiceDB-style graph): does this relation exist?
        │
        ├── allowed ──────────────────────► MCP Server → real tool / database / API
        ├── denied ───────────────────────► rejected, logged
        └── needs approval ──► Slack/Teams approver ──► approved/denied ──► resumed or rejected
        │
        ▼
  Immutable audit log + relationship lineage graph
```

For every call, the proxy resolves two questions in parallel: an **attribute-based check** (is this action within policy limits — size, time, environment, action type) and a **relationship-based check** (does this agent-acting-for-this-user actually have the standing relationship to this resource that the policy requires). Both have to clear for the call to proceed unconditionally; either one flagging "needs review" routes the call to a human.

The part that most resembles genuinely new engineering — as opposed to reused AccessGraph plumbing — is what happens when a call needs a human decision. Holding a live network connection open while a Slack approval is pending does not scale and breaks under normal infrastructure timeouts. The Model Context Protocol itself has, over the course of 2025–2026, grown native mechanisms for exactly this pattern — **elicitation** (a server can pause and ask the connected client a structured question) and, more recently, an official **Tasks extension** that lets a server hand back a durable task handle instead of blocking, so the client can disconnect and poll or resume later. AgentGate's approval flow should be built on top of these emerging native mechanisms rather than a bespoke workaround — with the caveat, discussed in Section 9, that some of this protocol surface is very new and still stabilizing as of mid-2026.

## 6. Two Deployment Scenarios

**A. Internal enterprise tooling.** An enterprise runs an internal AI agent (an HR copilot, a data-analysis assistant, an internal ops bot) wired to MCP servers that reach real infrastructure. The enterprise deploys AgentGate as a proxy in its own network, points it at its MCP servers, and configures policy and relationships through an admin dashboard. Employees authenticate through the company's existing SSO; that identity flows through to every agent action the employee triggers. A designated manager or platform-owner approves or denies flagged actions. This is the clean, well-scoped case, and it is the case management's brief actually describes for v1 — a human-in-the-loop approver who is an internal employee, not an anonymous end consumer.

**B. Public-facing, consumer-triggered agents** (e.g., a prompt-driven booking or shopping assistant). Here the natural "approver" for a risky action is not an internal risk team member — it is the consumer themselves, via something closer to a step-up confirmation or 2FA prompt than a Slack approval. This is a materially different workflow (different UI, different latency expectations, different regulatory framing — it's closer to transaction confirmation than to enterprise human oversight) and it should **not** be assumed into v1 scope. It is a legitimate second product surface, but conflating it with the enterprise HITL flow this early is a scope risk worth naming explicitly rather than hand-waving past.

## 7. Regulatory Alignment: The EU AI Act

The EU AI Act's high-risk provisions are directly relevant to two of AgentGate's core capabilities:

- **Article 12 (record-keeping):** requires automatic, comprehensive logging across a high-risk AI system's lifecycle, to allow post-hoc tracing of its behavior. AgentGate's immutable audit log — capturing agent, on-behalf-of identity, action, resource, policy version, and outcome for every call — is a direct fit for this requirement.
- **Article 14 (human oversight):** requires a real technical mechanism for a human to supervise, intervene in, or halt a high-risk system's actions. AgentGate's human-in-the-loop approval gate is a literal implementation of that requirement, not just a compliance narrative wrapped around unrelated features.

As of this writing (July 2026), the compliance timeline itself is in motion: the EU's "Digital Omnibus" reform, agreed in principle by the Council and Parliament in May 2026 and formally endorsed through June 2026, defers the compliance deadline for standalone high-risk (Annex III) AI systems from August 2026 to **December 2, 2027**, with systems embedded in regulated products (Annex I) deferred to August 2028. Formal publication in the EU's Official Journal was expected shortly after that endorsement. This is worth stating plainly to prospects: the deadline has moved out, which removes near-term urgency as a sales lever, but the underlying obligations have not changed in substance, and enterprises with EU exposure are advised by their own counsel to keep building compliance infrastructure on the original timeline rather than waiting. AgentGate should present the EU AI Act as a durable, structural reason to adopt the product — not as a ticking clock, since the clock has already been reset once and could move again.

## 8. What AgentGate Is Not

Stating scope boundaries clearly is as important as stating capabilities — this discipline is inherited directly from the original AccessGraph whitepaper and should carry over:

- **AgentGate is not an LLM guardrails or content-moderation product.** It does not inspect model outputs for toxicity, hallucination, or bias. It governs whether a specific tool call is authorized — a narrower and more mechanical question.
- **AgentGate is not a general AI observability platform.** It does not aim to replace tracing, cost-monitoring, or prompt-analytics tools. It is the enforcement and audit layer, not the dashboard-for-everything layer, though it should expose data that observability tools can consume.
- **AgentGate is not a prompt-injection detector.** It does not try to determine whether a given piece of input text is a disguised attack. It assumes injection is possible and limits the *damage* an injected instruction can do, by constraining what any call — injected or legitimate — is authorized to execute without a human check.
- **AgentGate is not an identity provider.** It delegates authentication to the enterprise's existing SSO (Okta, Entra ID, etc.). It is authorization and audit, not authentication.
- **AgentGate is not a network-layer security product.** It does not replace a WAF, a zero-trust network gateway, or TLS termination infrastructure. It operates at the semantic layer of "which tool, with which arguments, on whose behalf" — above the network layer, below the model's reasoning.

---

# Part 3 — The Reality Check

## 9. Honest Limitations

A credible whitepaper says what the product cannot do as clearly as what it can. These limitations are structural, not implementation bugs to be fixed away:

**It reduces blast radius; it does not make injection impossible.** AgentGate's entire value proposition rests on the assumption that some fraction of agent behavior will be manipulated or wrong. It cannot detect that an instruction was injected — it can only ensure that whatever the agent decides to do, the *action itself* is checked against a policy and, if risky, checked by a human. A sufficiently narrow, low-risk action that a policy author didn't think to restrict can still slip through unapproved. The tighter and more thoughtfully authored the policy, the smaller this gap — but it is never zero, and no vendor in this space can honestly claim otherwise.

**Policy authoring is real, ongoing work, not a one-time setup step.** Cedar- or OPA-style rules and a relationship graph are only as good as the humans who write and maintain them. An enterprise that writes an overly permissive policy to reduce approval fatigue has simply moved the risk, not removed it. This is a customer-success and onboarding problem as much as an engineering one — v1 needs to ship with sane, restrictive defaults and clear guidance, not a blank policy canvas.

**Latency is a hard constraint, not a nice-to-have.** Every unflagged tool call now has a proxy, a policy check, and a relationship-graph query in its path before it reaches the real tool. If this adds meaningful latency to routine calls, it will be disabled or bypassed by frustrated engineering teams — a well-documented failure mode in the credential-sharing analogy that motivated AccessGraph's "frictionless authorized path" design principle in the first place. The same principle has to hold here: the governed path must be faster or comparably fast to the ungoverned one, or adoption fails regardless of how sound the model is.

**The proxy pattern has protocol-shape limits.** MCP is most cleanly proxied when servers run over the network (Streamable HTTP transport). A large fraction of today's MCP usage — especially in developer tools — runs over local stdio transport between a client and a server on the same machine, which is architecturally harder to interpose on transparently. v1 should be explicit that it targets remote, production, network-deployed MCP servers, not local developer-tool usage, and should not overclaim coverage it doesn't have.

**Some of the cleanest technical building blocks are very new.** The MCP mechanisms best suited to AgentGate's async human-approval flow — the Tasks extension, multi-round-trip elicitation — are, as of mid-2026, recent additions moving through the protocol's specification process, with the current major spec revision only reaching release-candidate status in the months around this writing. Building v1 on these means building against a still-settling foundation. That is a reasonable bet given the direction the protocol is heading, but it is a real dependency risk, not a solved problem, and should be tracked as such rather than assumed away.

**No tool in this category eliminates insider risk or perfectly-scoped abuse.** A person with legitimate approval authority who rubber-stamps requests, or an agent operating entirely within its granted scope but toward a bad outcome nobody anticipated, is outside what any authorization layer can catch. AgentGate makes misuse attributable and bounded. It does not make it impossible — the same honest caveat AccessGraph made about credentials applies here to agent actions.

**The approval gap is a real race condition, not just a UX delay.** Any time a tool call is flagged, paused, and resumed later, there is a window between the moment AgentGate checked the world (account balance, permissions, resource state) and the moment the approved action actually executes. If that window is seconds, the risk is small. If a human approver takes minutes or hours to respond — which happens routinely — the underlying resource can change in the meantime: a balance can be drained by a parallel transaction, a permission can be revoked, a file can be modified. Approving a stale check and then blindly executing it is a textbook time-of-check-to-time-of-use (TOCTOU) problem, and it is a genuine architectural gap in the design as described so far, not a hypothetical one — independent 2026 security research on AI agents (including a controlled study of exactly this class of check/execute timing mismatch, published by Ohio State researchers in February 2026) confirms this is a broadly reproducible failure mode in agent systems generally, not an edge case specific to AgentGate. The mitigation is straightforward in principle and is now a required v1 primitive, not a nice-to-have: **AgentGate must re-verify the relevant resource state immediately before forwarding an approved call, not just at the moment it was originally flagged**, and abort with a clear "state changed, please re-evaluate" response if the two don't match — the technical companion document covers this design and its real limits. It is worth being honest that this narrows the vulnerability window; it does not eliminate it, because AgentGate generally cannot guarantee true atomicity for an arbitrary downstream tool or API that wasn't built to support a conditional, all-or-nothing write.

## 10. Competitive Landscape and B2B Feasibility

This is the section most important to get right before committing real engineering time, because the honest picture is more crowded than the original brief implies.

**The market is more crowded than a first pass suggests, and it is consolidating fast.** By mid-2026, "MCP gateway" / "agent gateway" is not just an active category — it is actively being standardized around shared open infrastructure. Solo.io donated its `agentgateway` project — a purpose-built, open-source MCP/A2A proxy — to the Linux Foundation in August 2025; it now sits under the Linux Foundation's Agentic AI Foundation (the same body that governs MCP itself), with contributors and backers including Microsoft, AWS, Cisco, IBM, Red Hat, Akamai, T-Mobile, Dell, and CoreWeave. Independent reporting in early July 2026 describes new dedicated agent-gateway entrants arriving on a near-weekly cadence (Nutanix shipped a generally-available agent gateway in its enterprise AI platform; Arcade and Manufact both launched competing offerings within the same week; Palo Alto Networks acquired the AI-gateway vendor Portkey outright), on top of identity vendors (Okta, Aembit) already shipping AI-agent-specific token-exchange and authorization features. Any pitch that frames AgentGate as filling a wide-open gap will not survive five minutes of a prospect's vendor research — and worse, it invites the reasonable follow-up question of why a small team should build its own low-level network proxy at all when a free, multi-vendor-backed one already exists.

**That reality changes the recommended build strategy, not just the marketing.** Competing at the network-proxy layer — parsing JSON-RPC, terminating TLS, translating transports, hitting sub-millisecond overhead at scale — is exactly the kind of infrastructure problem that a Linux-Foundation-governed, corporate-backed open-source project is built to commoditize, and AgentGate has no realistic path to out-engineer that with a small team. The better strategy is to **not build a competing proxy**: deploy on top of (or alongside) an existing open gateway like `agentgateway` for the raw transport/routing/authentication plumbing, and put AgentGate's engineering effort entirely into the layer that gateway is not built to provide — a genuine relationship/delegation lineage graph, a structured human-approval workflow with re-verified state at resume time (see Section 9), and audit output mapped explicitly to regulatory articles. This is a meaningful revision from treating AgentGate as "a proxy with governance bolted on" to treating it as "a governance and approval control plane that sits behind whichever proxy the customer already has or adopts." The technical companion document reflects this revised architecture.

**Where a real gap still exists.** Even the well-backed open gateways in this space are, as of mid-2026, focused on authentication, coarse role-based tool access, rate-limiting, and traffic-level observability — closer to an API gateway adapted for MCP traffic than to a genuine relationship-based access model with human oversight. Few, if any, combine: (a) a true relationship/lineage graph that can answer "what breaks if this identity is revoked," inherited directly from a mature ReBAC design; (b) a first-class, structured human-approval workflow tied to specific risky actions, with the state re-verified at resume time rather than trusted from flag time; and (c) audit output explicitly mapped to regulatory articles (EU AI Act Article 12/14) rather than generic "compliance-ready" marketing language. That combination is the defensible wedge — and it is now clearly a wedge *on top of* commodity infrastructure, not a replacement for it.

**The AccessGraph reuse is a real asset, with a caveat.** Having already thought through ReBAC delegation modeling, audit invariants, and a Cedar/SpiceDB-style dual-engine approach for a different governance problem is a genuine head start on the *governance* layer specifically — which, per the revision above, is now the entire scope of what needs to be built. It is not a head start on the *buyer conversation* — the AgentGate buyer (a CISO or AI risk lead worried about agent blast radius) is different from the AccessGraph buyer (a platform engineering lead worried about credential sprawl), and the sales motion, onboarding flow, and UI need to be built for that buyer specifically, not inherited by default.

**Honest verdict on feasibility.** The problem is real, the urgency is real (regulatory deadlines aside, enterprises are being blocked *today* by their own risk committees), and the narrowed technical approach — governance and approval layer on top of commodity proxy infrastructure — is sound and meaningfully more capital-efficient than the original "build everything" framing. This is still not a blue-ocean opportunity; it is a real, technically credible entrant into a fast-consolidating category. Success now depends specifically on (a) shipping the relationship-graph-plus-approval depth that current gateway players lack, (b) integrating cleanly with the open gateways enterprises are already adopting rather than asking them to rip one out, and (c) being disciplined about never spending engineering time re-solving a problem the Linux Foundation ecosystem has already solved for free.

## 11. v1 Scope and Roadmap

Consistent with management's brief, and revised per Section 10's finding that the network-proxy layer is now commodity infrastructure, v1 should stay narrow:

1. **Integrate with an existing open MCP gateway (e.g., `agentgateway`) for transport, routing, and basic authentication, rather than building a competing proxy.** AgentGate's own engineering effort starts one layer up: identity/on-behalf-of resolution and policy decisioning. Local/stdio MCP is explicitly out of scope for v1.
2. **Per-action policy check** — attribute rules (Cedar or OPA) for allow / deny / require-approval, with restrictive, opinionated defaults rather than a blank policy canvas.
3. **A relationship graph** for the subset of resources where "does this relationship exist" genuinely matters (ownership, delegation, team membership) — not an attempt to model every possible enterprise relationship on day one.
4. **Human approval in Slack/Teams**, built on MCP's native async mechanisms (elicitation / Tasks extension), resolving without holding the agent's connection open, **and re-verifying resource state immediately before forwarding an approved call** — the TOCTOU mitigation from Section 9 is a v1 requirement, not a later hardening pass.
5. **An immutable audit log**, structured so that an EU AI Act Article 12/14 export is a report, not a re-engineering effort.

Explicitly **out of v1**: the public consumer/2FA approval scenario, ML-based risk flagging (deterministic policy first — an ML classifier's false negatives are worse than no classifier, per the same concern raised during early scoping), approval batching, and advanced reviewer controls like time-boxed blanket approvals or agent-wide blocking. These are reasonable roadmap items once v1 has real customer usage to learn from, not prerequisites for a first deployable version.

## 12. Open Questions Requiring Decisions

These are genuine open decisions, not settled facts, and are addressed with recommendations (not final answers) in the technical companion document:

- Does AgentGate need a thin client-side SDK to carry on-behalf-of identity, or can this ride entirely on MCP's own emerging OAuth/authorization surface?
- Which relationships actually need to live in a graph database versus a simpler relational table, given v1's narrower scope?
- What is the acceptable latency budget for an unflagged call, and what does the policy/relationship check path need to look like to hit it?
- How much of the "flagging" logic should be static policy versus any form of learned risk scoring, and how is false-negative risk managed either way?
- What is the deployment model enterprises will actually accept — a proxy inside their VPC, a sidecar per agent host, or a hosted control plane with data-plane components on-prem? This has large implications for both engineering effort and sales cycle length.

---

*AgentGate — Product Whitepaper*
*July 2026*
