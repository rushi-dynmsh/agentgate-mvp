# AgentGate — Technical Architecture Companion

**Companion to:** AgentGate_Whitepaper.md
**Audience:** Engineering, technical founders, anyone implementing v1
**Date:** July 2026

This document goes one level deeper than the whitepaper: component architecture, identity propagation options, policy and relationship schemas, the async approval protocol, audit log design, and the open engineering decisions that still need to be made before v1 starts. Where the whitepaper asserts a design choice, this document shows the reasoning and the alternatives that were considered and rejected.

A note on sourcing: several specifics below (the MCP Tasks extension, the 2026-07-28 MCP spec revision, current MCP security research) reflect protocol and industry state as of mid-2026. This space is moving fast enough that some details here should be re-verified against current MCP specification documents before implementation begins.

---

## 1. Component Architecture

**Revision note:** an earlier draft of this document assumed AgentGate would build and run its own network reverse proxy (custom JSON-RPC parsing, transport handling, TLS termination). That assumption should be dropped. As of mid-2026, `agentgateway` — a purpose-built, open-source MCP/A2A proxy originally from Solo.io — is governed by the Linux Foundation's Agentic AI Foundation (the same body that governs MCP itself) and backed by Microsoft, AWS, Cisco, IBM, Red Hat, and others. It already solves transport, routing, basic authentication, and rate-limiting for MCP traffic, for free, with more engineering resources behind it than a small team can match. Building a competing proxy from scratch is not a good use of engineering time. AgentGate's architecture below assumes it runs **behind or alongside an existing open gateway**, and confines its own scope to the governance layer that gateway doesn't provide: identity/OBO resolution, relationship-based authorization, human approval orchestration with state re-verification, and regulatory-mapped audit.

```
┌───────────────────────────┐
│   AI Agent Host / App     │
│   (MCP Client)            │
└──────────────┬────────────┘
               │ MCP (JSON-RPC 2.0 over Streamable HTTP)
               ▼
┌─────────────────────────────────────────────────────────────┐
│   OPEN-SOURCE MCP GATEWAY (e.g. agentgateway)                │
│   Transport, routing, TLS, basic auth, rate-limiting          │
│   — not built or maintained by AgentGate                     │
└──────────────────────────────┬────────────────────────────────┘
                                │ authorization callout (per-call)
                                ▼
┌─────────────────────────────────────────────────────────────┐
│                  AGENTGATE GOVERNANCE LAYER                  │
│                                                               │
│  ┌───────────────┐   ┌──────────────────┐   ┌─────────────┐ │
│  │ Identity /     │   │ Policy Engine    │   │ Relationship │ │
│  │ OBO Resolver   │──▶│ (Cedar / OPA)    │──▶│ Graph        │ │
│  └───────────────┘   └──────────────────┘   │ (SpiceDB)    │ │
│                              │               └─────────────┘ │
│                              ▼                                │
│                     ┌─────────────────┐                       │
│                     │ Decision:       │                       │
│                     │ allow/deny/     │                       │
│                     │ needs-approval  │                       │
│                     └────────┬────────┘                       │
│           ┌──────────────────┼──────────────────┐             │
│           ▼                  ▼                  ▼             │
│      [forward]          [reject]        [approval flow +      │
│                                          state re-verify]      │
└───────────┼──────────────────┼──────────────────┼─────────────┘
            │                  │                  │
            ▼                  ▼                  ▼
   ┌─────────────────┐   ┌───────────┐    ┌──────────────────┐
   │ back to gateway  │   │ Audit log │    │ Slack / Teams     │
   │ → real tool/DB   │   │ (Postgres)│    │ approver          │
   └─────────────────┘   └───────────┘    └────────┬──────────┘
                                                     │
                                            approve/deny callback
                                                     ▼
                                     resume (re-verify state, see §9)
                                     → reject or forward via MCP Tasks
```

The integration point between the gateway and AgentGate's governance layer is a standard external-authorization callout pattern (the gateway asks "allow, deny, or hold for approval?" before forwarding each call) — conceptually similar to how API gateways commonly delegate authorization decisions to an external policy service rather than embedding all logic in the gateway itself. The exact mechanism (a native plugin, an external-processing hook, or a simple synchronous webhook) depends on what the chosen gateway supports and should be confirmed against its current documentation before committing to an integration design — don't assume a specific extension mechanism without checking it first.

### Core components

| Component | Role | Notes |
|---|---|---|
| **MCP gateway (not built by AgentGate)** | Transport, routing, basic auth, rate-limiting | Use an existing open project (e.g. `agentgateway`); do not build this |
| **Identity / OBO resolver** | Extracts and verifies which agent + which human/service the call is made on behalf of | See Section 2 |
| **Policy engine** | Fast attribute-based rule evaluation (allow / deny / needs-approval) | Cedar or OPA; see Section 3 |
| **Relationship graph** | Resolves "does this relation exist" queries (ownership, delegation) | SpiceDB (Zanzibar-style); see Section 4 |
| **Approval orchestrator** | Manages the pause/resume lifecycle for flagged calls, including state re-verification at resume | Built on MCP's async primitives; see Sections 5 and 9 |
| **Audit store** | Immutable log of every decision | PostgreSQL, append-only table + periodic export; see Section 6 |
| **Admin dashboard** | Policy authoring, pending approvals, lineage graph browsing | Out of scope for this document's depth; standard CRUD + graph viz |

---

## 2. Identity and On-Behalf-Of (OBO) Propagation

This is the single most important open technical question, and the one your own notes correctly flagged as uncertain. Here is the actual state of the protocol and the realistic options.

### What MCP gives you natively

Since the 2025-06-18 specification revision, MCP has a real OAuth 2.1-based authorization model for remote (Streamable HTTP) servers: a client authenticates to a server, gets a scoped token, and that token identifies **the calling client/agent** to the server. This solves *agent authentication* — it does not, by itself, solve **on-behalf-of** identity: distinguishing "agent X, acting for human Bob" from "agent X, acting for human Alice," when both Bob and Alice are using the same agent deployment through the same client credentials.

### Three realistic approaches

**a) Ride entirely on MCP's own auth surface.** Have the enterprise's identity provider issue a token whose audience is the AgentGate proxy and whose claims include the actual human's identity (a standard OBO/token-exchange pattern, similar to how Azure AD or Okta already support on-behalf-of token flows for regular web apps calling downstream APIs on a user's behalf). AgentGate then just validates and reads claims — no custom SDK needed. **This is the cleanest option and should be the default assumption for v1**, since it means integration is "point your agent host's OAuth config at us," not "rewrite your tool-calling code."

**b) A thin companion SDK.** For agent hosts that can't easily do a full OBO token-exchange flow (custom internal frameworks, older systems), a small SDK wraps the outgoing tool call and attaches the current user's session identity as a signed header or claim, without needing the LLM itself to see or handle that token. This is closer to what the AI-brainstorm draft proposed, and it's a legitimate fallback — but it should be positioned as a fallback integration path, not the primary one, because it adds an SDK dependency and a maintenance burden across every language/framework the customer uses.

**c) Header injection at an existing enterprise gateway.** If the enterprise already terminates employee sessions at an API gateway or reverse proxy, that layer could inject the identity claim before traffic reaches AgentGate. Useful for some enterprise topologies, but it makes AgentGate dependent on a piece of infrastructure it doesn't control, which complicates both security guarantees and support.

**Recommendation:** build for (a) as the primary path, support (b) as an explicit fallback SDK for a small number of languages (Python, TypeScript, Go), and don't build (c) — document it as a pattern customers can wire up themselves if they already have the infrastructure.

### The unsolved edge case

Public/guest access (Scenario B in the whitepaper — no logged-in enterprise identity) doesn't fit this model at all; there's no "on behalf of" human identity to propagate, only a session or device identity. That confirms the whitepaper's recommendation to treat that scenario as a separate roadmap item with its own identity design, not a variant of the v1 flow.

---

## 3. Policy Engine: Cedar vs. OPA

Both AWS Cedar and Open Policy Agent (OPA/Rego) are real, production-grade options for the attribute-based half of the check. Practical tradeoffs for this specific use case:

- **Cedar** was purpose-built for authorization (not general policy), has a schema-validated, relatively readable syntax, and ships with a formally-verified core — a genuine advantage when a compliance auditor asks "how do you know your policy engine behaves as specified." It integrates cleanly as an embedded library (Rust core with bindings) rather than requiring a separate sidecar process.
- **OPA/Rego** is more general-purpose, has a larger existing ecosystem and more examples for "gateway"-style deployments, but Rego is harder to read and reason about for non-engineers, which matters if you want risk/compliance staff — not just developers — to be able to read a policy and understand what it does.

**Recommendation:** Cedar, primarily for auditability and readability, which matters more here than in a typical infrastructure-policy use case, given that the buyer explicitly includes non-engineering risk and compliance stakeholders.

### Illustrative policy shape (not final syntax)

```
// Illustrative only — validate against current Cedar syntax before use
permit(
  principal == Agent::"sales-agent-prod",
  action == Action::"tools/call",
  resource == Tool::"production_database_mutation"
)
when {
  context.on_behalf_of.role == "platform-engineer" &&
  context.transaction_value < 5000 &&
  context.time.is_business_hours
}
unless {
  context.action_type == "DROP_ROW" || context.action_type == "DELETE"
};
```

Destructive action types (`DROP`, `DELETE`, mass mutation) should default to `require-approval` in the shipped default policy set, not to `deny` (which breaks legitimate workflows) or `permit` (which is the failure mode this whole product exists to prevent).

---

## 4. Relationship Graph: What SpiceDB Actually Buys You Here

Your own notes were right to be unsure whether SpiceDB has a real use case in AgentGate versus being carried over out of habit from the credential-sharing design. Here's the honest assessment:

**Where it earns its place:** questions like *"does this specific employee, acting through this agent, have an owner/editor/viewer relationship to this specific resource"* and *"if we revoke this employee's access, which downstream agent-delegated permissions become orphaned"* are genuinely relational, multi-hop questions — exactly what Zanzibar-style ReBAC engines are built for, and exactly what a flat permission table handles poorly.

**Where it's overkill for v1:** if the actual v1 policy need is "this role can call this tool," that's a flat, non-relational check, and running a full graph database for it is unnecessary operational overhead. The recommendation in Section 11 of the whitepaper — scope the graph to the resources where relationships genuinely matter, not everything — is the right call technically as well as strategically. A reasonable v1 approach: use Postgres for straightforward role/permission checks, and reserve SpiceDB specifically for delegation and ownership chains (the "who approved whom" lineage), where it's clearly worth the complexity.

### Illustrative schema shape (not final syntax)

```
definition user {}

definition agent {
  relation acts_for: user
}

definition resource {
  relation owner: user
  relation delegated_approver: user
  permission mutate = owner + delegated_approver
}
```

A tool call resolves to: *does `agent.acts_for` have `mutate` permission on the target `resource`?* — a standard Zanzibar-style check.

---

## 5. The Async Human Approval Flow

This is the part of the system most likely to be built wrong if implemented as a bespoke workaround instead of on top of what MCP now natively supports.

### Why holding the connection open doesn't work

A synchronous "wait for Slack click" design holds a live request open for however long a human takes to respond — seconds in the best case, but realistically minutes, and sometimes longer if the approver is away. That exhausts connection pools, trips load-balancer and gateway timeouts, and ties up client-side execution threads. This was a known failure pattern in early human-approval gateway designs generally, independent of MCP specifically.

### What MCP now offers

Two protocol-level mechanisms are directly relevant, and both are real (verified against current MCP specification sources), though at different levels of maturity as of mid-2026:

- **Elicitation** — a server can pause mid-call and ask the connected client a structured question (approve/deny, provide missing input), with the client rendering a form or prompt. This has been part of the spec since the June 2025 revision and is broadly supported.
- **Tasks extension** — a newer addition (part of the protocol's 2026 roadmap toward a fully stateless core) that lets a server return a durable task handle instead of blocking. The client can disconnect entirely and later poll (`tasks/get`) or receive a push notification when the task resolves. This is explicitly designed for exactly AgentGate's use case: "human-in-the-loop workflows — approval gates that block until a person responds" is called out directly as a target scenario for the Tasks extension.

**Recommended v1 flow:**

1. Agent calls a tool; AgentGate's policy check returns `needs-approval`.
2. AgentGate creates a durable task record (in its own Postgres store, mirroring the semantics of an MCP task) and returns an `input_required` / task-pending result to the client, per the Tasks extension pattern — the agent's execution loop parks cleanly, no open connection required.
3. AgentGate's escalation service posts an interactive message to Slack/Teams with the actor identity, the specific tool call and arguments, and one-click approve/deny.
4. On approval, AgentGate validates the callback signature, marks the task resolved, and either pushes a resumption notification (if the client subscribed) or waits for the client's next poll.
5. On resumption, the original call is forwarded to the real MCP server; the full transaction — request, flag reason, approver, decision, timestamps — is written to the audit log as one linked record.

**Important caveat, stated plainly:** the Tasks extension and the broader stateless-core revision of MCP were only reaching release-candidate status around mid-2026. Building v1's core approval mechanism on a still-stabilizing piece of the spec is a reasonable bet on protocol direction, but it is a real dependency risk — track it explicitly, and have a fallback design (a custom pending-transaction cache with polling, independent of whether the client speaks the official Tasks extension) ready in case the timeline slips or the final spec shape changes before v1 ships.

---

## 5a. The Approval-Gap Race Condition (TOCTOU) and Its Real Limits

This section exists because an earlier design pass missed it, and it deserves to be treated as a first-class requirement, not an appendix.

### The problem, precisely

The approval flow in Section 5 has two distinct moments: the moment AgentGate decides a call needs approval (call this `t_check`, when it evaluates policy and relationship state), and the moment the approved call actually reaches the real tool or API (`t_use`, potentially seconds to hours later, after a human responds). Nothing in the basic design re-examines whether the world is still the same at `t_use` as it was at `t_check`. A resource's state — an account balance, a permission grant, a file's contents — can change in between, including via a concurrent, unrelated action. Approving based on a stale `t_check` snapshot and then executing at `t_use` without re-checking is a standard time-of-check-to-time-of-use defect (CWE-367), and it is a real one here: a plausible scenario is an agent requesting a refund against a positive balance, the check passing, a concurrent withdrawal draining the account during the approval wait, and the refund still executing against the now-depleted balance once a human clicks approve.

### What actually mitigates it — and what doesn't

**Re-verify immediately before forwarding, not just at flag time.** When AgentGate is about to forward an approved call, it should re-run the relevant parts of the policy and relationship check against current state, immediately before forwarding — not trust the `t_check` decision blindly. If the answer has changed, abort and return an error that forces the agent to re-plan, rather than executing against stale assumptions. This is the single highest-value, most implementable mitigation, and it should be a v1 requirement for any call that was paused for approval (routine, unflagged calls don't need this — the check-to-use gap there is milliseconds, not the multi-hour gap that makes this a real risk).

**Where a hash-based "state attestation" helps, and where it can't.** A more elaborate version of the same idea — computing a fingerprint of relevant resource state at `t_check`, embedding it in the pending-approval record, and comparing it against a freshly computed fingerprint at `t_use` before forwarding — is a legitimate, standard technique (it's the same idea as an HTTP `ETag`/`If-Match` precondition, or optimistic locking with a row version column in a database). It is worth building for resources where AgentGate can itself query authoritative state directly (e.g., a balance or row it can read before forwarding). **Be honest about its limit, though: this narrows the race window, it does not close it.** The comparison only proves the state AgentGate can see hasn't changed between AgentGate's own two reads. It cannot guarantee true atomicity of the entire operation unless the downstream tool or API itself supports a conditional, all-or-nothing write (e.g., "only apply this refund if balance == X") — and most tools and APIs a proxy calls generically do not expose that. For a downstream system without conditional-write support, re-verifying immediately before forwarding still leaves a small residual window between AgentGate's final check and the downstream system's actual execution. That residual window is dramatically smaller than the original multi-hour approval-wait window, which is the real, honest value of this mitigation — not "elimination of the race," which no generic proxy can credibly promise for arbitrary downstream systems it doesn't control.

**Where this points for policy authoring:** high-risk action types that are inherently hard to make idempotent or reversible (irreversible deletes, one-way payments to external accounts) should default to a *short* approval SLA in policy (e.g., auto-expire and require re-evaluation if not approved within a few minutes) rather than allowing indefinitely long pending-approval windows, specifically because the race window scales with how long the approval takes.

### What this means for v1 scope

Add to the required v1 feature set: (1) a pre-forward re-check step in the approval orchestrator for any previously-flagged call, and (2) a configurable approval-window expiry per policy rule, defaulting to something short (minutes, not hours) for the highest-risk action types. Treat the hash/fingerprint-based version as a v1.x hardening step once there's a concrete resource type (e.g., a specific payments or database integration) to build it against — building it generically before there's a real target risks over-engineering something that only works for a subset of downstream systems anyway.

---

## 6. Audit Log Design

Every decision AgentGate makes should produce one immutable record with, at minimum:

- `transaction_id`
- `agent_identity` (which agent/harness)
- `on_behalf_of` (resolved human or service identity, or null/guest if unresolved)
- `tool_name`, `arguments` (the exact call — redact/hash sensitive argument values per policy, don't blanket-log secrets)
- `policy_version_hash` (which exact policy snapshot made the decision — critical for the EU AI Act Article 12 "which rules were in effect" question)
- `decision` (allow / deny / approval-required)
- `approval_outcome` (approver identity, timestamp, channel) if applicable
- `resource_target`
- `timestamp`

Store this in an append-only Postgres table (application-level enforcement of immutability is sufficient for v1; true WORM/object-lock replication to something like S3 Object Lock is a reasonable hardening step for later, not a v1 requirement — don't over-engineer this before there's a paying customer asking for it specifically).

Compliance export (EU AI Act Article 12/14 mapping) should be a query + formatting layer over this same table, not a separate logging pipeline — two audit systems that can drift apart is a worse compliance posture than one system with a good export view.

---

## 7. Deployment Model

Given the revised architecture (Section 1) — AgentGate as a governance layer behind an existing open MCP gateway, not a standalone proxy — the deployment question becomes specifically about where AgentGate's own components (policy engine, relationship graph, approval orchestrator, audit store) run relative to the customer's gateway and network. Three realistic options, in order of engineering effort:

1. **Fully self-hosted** — customer runs both the open gateway and AgentGate's governance components inside their own network/VPC, connected to their SSO. Highest customer trust, highest support burden, slowest to iterate on.
2. **Hosted control plane, on-prem enforcement point** — AgentGate hosts the policy authoring UI, relationship graph, and audit store centrally; a lightweight component runs inside the customer's network alongside their gateway to make the actual per-call allow/deny/approval-required calls (so sensitive tool-call payloads and resource state never have to leave the customer's network for the decision itself, even though policy/dashboard/audit data can live centrally). This is the standard pattern for this category of security-adjacent product and is the right target for v1: it keeps live traffic and resource state on the customer's side while letting the team iterate on policy engine and dashboard centrally.
3. **Fully hosted, traffic routed through AgentGate's cloud** — fastest to build and iterate, but a much harder sell to any buyer sophisticated enough to be evaluating agent governance in the first place (they will ask why production tool-call traffic and resource state should transit a third party's cloud for every call).

**Recommendation:** design for (2) from the start. It matches how comparable products in this space are already being sold, and retrofitting a fully-hosted v1 into a hybrid model later is significantly more work than building hybrid from day one. Because the underlying gateway (Section 1) is typically already deployed inside the customer's network in this model, AgentGate's on-prem footprint can be genuinely lightweight — the enforcement callout handler plus a local cache of policy/relationship data — rather than a full standalone proxy stack.

---

## 8. Suggested Engineering Sequencing (Not a Committed Timeline)

Rough sequencing, deliberately more conservative than a fixed "90-day" plan, since real timelines depend on team size and the unresolved questions above:

1. **Gateway integration + identity resolution** — stand up an existing open MCP gateway (e.g. `agentgateway`) in front of one real MCP server, confirm its actual external-authorization/plugin mechanism against current documentation, and get AgentGate's identity/OBO resolution (Section 2) wired into that callout path. Do not build a custom proxy for this step.
2. **Policy engine integration** — embed Cedar, wire the flat role/permission checks first, defer the relationship graph.
3. **Audit log** — get every decision (even trivially "allowed") writing to the immutable log from day one; retrofitting audit onto an existing system is much harder than building it in from the start.
4. **Relationship graph** — add SpiceDB specifically for delegation/ownership chains once there's a concrete customer scenario that needs it, not speculatively.
5. **Async approval flow with state re-verification** — build against the Tasks/elicitation pattern, with the fallback custom polling design as insurance, and include the pre-forward re-check from Section 5a as a required part of this step, not a later addition.
6. **Dashboard** — policy authoring UI, pending-approval queue, lineage browser.

Each stage should be validated against one real, concrete customer scenario rather than built in the abstract — this is a category where the difference between "technically correct" and "actually adoptable" is almost entirely about fitting real integration constraints (transport type, existing SSO setup, existing MCP server inventory) that only show up when working with an actual design partner.

---

## 9. Summary of Corrections to the Earlier AI-Generated Draft

For transparency, since you asked me to sanity-check that draft specifically: most of its architectural instincts (Cedar + SpiceDB dual-engine, async approval instead of blocking, EU AI Act mapping) were directionally reasonable and some specific protocol details in it (the Tasks-like multi-round-trip mechanism, the EU AI Act 2027 deadline) turned out to be accurate once checked against current sources — better than "hallucinated," worse than "verified before writing." The main problems were tone and unverifiable specificity: invented-sounding citations (a specific NSA guidance document, a specific named protocol SEP number stated with unearned confidence), infrastructure specifics presented as decided (Redis Enterprise, WORM S3 replication, Go/Rust binary) when these are implementation choices nobody has made yet, and a polished 90-day roadmap with a false sense of precision. This document tries to keep what was directionally right, verify what could be verified, and flag clearly what's still a genuinely open decision rather than presenting a guess as a spec.

## 10. Summary of Findings From the Second AI Review ("Technical Architecture Audit")

A second AI-generated review raised a TOCTOU vulnerability and a competitive/architecture critique. Independent verification found:

**Confirmed real and incorporated:**
- The TOCTOU race condition between approval flag-time and execution time is genuine and was missing from the earlier version of this document. The underlying citation (arXiv:2603.00476, "Atomicity for Agents," Ohio State, Feb 2026) is a real paper — it studies browser-use agents specifically, not MCP approval flows, so the application to AgentGate is the reviewing model's extrapolation rather than a direct finding, but the extrapolation holds up. Incorporated as Section 5a, including the honest caveat that the fix narrows rather than eliminates the window.
- `agentgateway`'s donation to the Linux Foundation's Agentic AI Foundation, and its backing by Microsoft, AWS, Cisco, IBM, Red Hat, Akamai, T-Mobile, Dell, and CoreWeave, is real and independently confirmed. The broader "agent gateway" category is consolidating quickly (multiple new entrants and one acquisition in the first week of July 2026 alone, per independent reporting). This materially changes the recommended build strategy — incorporated throughout Section 1, 7, and 8, shifting AgentGate from "build a proxy" to "build the governance layer behind an existing proxy."
- The Cedar-vs-Rego/OpenFGA performance claim ("28.7–35.2x faster than OpenFGA, 42.8–80.8x faster than Rego") traces to the actual published Cedar paper (arXiv:2403.04651) and is independently corroborated. Cedar remains the right recommendation for the reasons in Section 3, now on firmer evidentiary footing.

**Not incorporated, or incorporated with a caveat:**
- The specific benchmark table in that review (Cedar p50 0.62ms, SAPL 0.08ms, ~800,000 dps throughput, sourced to "kastra.ai/benchmarks") could not be independently confirmed as presented — the general ranking (SAPL fastest, then Cedar, then Rego/OpenFGA slower) is consistent with real published sources, but the specific decimal-precision figures should not be quoted as verified until checked directly against a primary source.
- The proposed "State-Attested Optimistic Locking" mechanism (SHA-256 state hash embedded in a signed JWT) was presented as eliminating the TOCTOU exploit. It doesn't, for the reasons explained in Section 5a — it's a legitimate technique with a real, narrower benefit than claimed for it.
- Several smaller named competitors in that review (Pipelock, DefenseClaw, Trylon Gateway) were not independently verified one way or the other; the larger, more consequential names (agentgateway, Okta, Aembit, TrueFoundry) were checked and are real.
- The suggestion to integrate specifically via Envoy's "ExtProc" mechanism was not verified — `agentgateway` was built specifically because its authors found Envoy needed substantial rearchitecting to support MCP/A2A, so whether it exposes an Envoy-compatible ExtProc interface is unconfirmed. Section 1 recommends confirming the actual integration mechanism against current gateway documentation rather than assuming ExtProc specifically.
