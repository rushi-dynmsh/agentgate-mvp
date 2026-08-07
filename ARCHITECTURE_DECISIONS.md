# AgentGate — Architecture Decisions & Open Problems

A living document capturing design findings, constraints discovered during
development, approaches evaluated, and the team's current direction. Written
so anyone joining the project can understand not just *what* was built, but
*why*, and where the hard problems still live.

---

## 1. What AgentGate is (one paragraph)

AgentGate is a governance layer that sits between AI agents and the tools they
call. Every tool call must pass through AgentGate before reaching the tool.
AgentGate answers one question per call: *"Is this agent, acting on behalf of
this human, allowed to do this — right now?"* If the answer is no, the call
never reaches the tool. Every decision — allow or deny — is permanently logged.

---

## 2. The current architecture (POC)

```
Agent (MCP client)
    │ 1. login → JWT
    ▼
Keycloak (identity provider)
    │
    ▼
agentgateway (Rust MCP proxy, port 3000)
    │ 2. validate JWT signature (jwtAuth, strict)
    │ 3. ext_authz Check(request + claims)
    ▼
agentgate (Go governance service, port 9000/8090)
    ├── identity  → who is this? (from JWT claims)
    ├── policy    → Cedar role check + SpiceDB ownership check
    ├── audit     → write decision to Postgres
    └── approval  → park destructive calls, human UI at :8090
    │
    ▼ (only on allow)
toy-mcp-server (the protected tool, port 8080)
```

**Two independent authorization checks for destructive operations:**
- **Cedar** (role-based): "may admins call delete tools?" — categorical, one
  rule covers every record
- **SpiceDB** (relationship-based): "does alice own record 2?" — per-object,
  roles cannot express this

**Key invariant:** the agent cannot reach any tool without passing through the
gateway. Governance is structurally unavoidable, not advisory.

---

## 3. The binary ext_authz constraint

### What it is

`ext_authz` is a gRPC protocol borrowed from the Envoy proxy ecosystem. The
gateway calls agentgate with the full request and waits for one of two answers:
**allow** or **deny**. There is no third state. There is no "hold" or "wait."

### Why this matters

The POC's human-in-the-loop flow works around this by:
1. Returning **deny** to the gateway immediately
2. Embedding a structured payload in the JSON-RPC error body:
   `{"code": -32003, "data": {"status": "pending_approval", "transaction_id": "..."}}`
3. The agent reads this, understands the pending state, polls
   `check_approval_status`, and waits

**This requires the agent to be purpose-built to understand the custom error.**
A standard MCP client (Claude Desktop, a generic agent framework) would see a
JSON-RPC error and either crash, retry, or report failure. It has no concept of
"a human needs to approve this."

---

## 4. The human-in-the-loop problem — full analysis

### The fundamental constraint

The AI model itself is stateless between turns. It generates a tool call and
stops. The **execution harness** — the framework or application managing the
agent loop — is what actually waits for the tool to return a result. And the
harness is not under our control.

Different harnesses behave differently:
| Harness | Behavior on tool failure/timeout |
|---|---|
| Claude Desktop | Has a timeout, probably won't wait 10+ minutes |
| Claude API (direct) | Developer's code controls the loop — configurable |
| LangGraph | Native `interrupt` mechanism — can pause indefinitely |
| CrewAI / AutoGen | Configurable timeouts, varies by version |
| Custom agent loop | Entirely developer-defined |

### The irreconcilable pair

```
Human approval time  →  unbounded (minutes to hours, cross-timezone)
HTTP connection life →  bounded   (seconds to minutes, outside our control)
```

No gateway design resolves this gap without *something* on the client side
knowing that a pending approval exists and that it should come back.

### Approach A — agentgateway + client-side wrapper

Keep agentgateway as the proxy. Add an SDK/wrapper that intercepts
`pending_approval` errors and handles polling transparently.

**Pros:**
- Leverages mature agentgateway infrastructure (JWT validation, routing)
- Wrapper is reusable across agents

**Cons:**
- Requires the wrapper in every agent, every framework, every language
- Reduces mass adoption — every implementer must integrate the SDK
- SDK must be maintained in Python, TypeScript, Go at minimum
- Framework plugins (LangGraph, CrewAI, etc.) are higher-leverage but even
  more work to maintain
- Enforcement is server-side (good), but ergonomics depend on client code

### Approach B — custom lightweight MCP gateway

Replace agentgateway with a purpose-built Go service that is simultaneously an
MCP server (faces the agent) and an MCP client (faces the tools). It owns the
full request lifecycle and can hold connections during approval.

**How it works:**
```
Agent calls delete_record
    → gateway parks it internally
    → holds the SSE connection open
    → sends periodic MCP progress notifications while waiting
    → human approves
    → gateway forwards to real tool, gets result
    → returns real result to agent
Agent sees a normal (slow) tool response. No errors. No custom codes.
```

**Pros:**
- Completely transparent to agents — zero client-side changes
- Works with any MCP client without modification
- No custom error codes, no custom SDK

**Cons:**
- Must reimplement JWT validation (not hard, standard Go OIDC libraries)
- The connection-hold timeout problem remains:
  - If human takes > N minutes, connection dies regardless
  - Load balancers, proxies, client libraries all have timeout opinions
  - "No timeout" blocks the agent indefinitely
- For truly async approvals (hours), a reconnect mechanism is still needed,
  which requires some client awareness

### Why neither approach is a clean win

Both fail at the same wall: **approval time is unbounded; connection lifetime
is bounded; and the execution harness that decides "what do I do with a
timed-out tool call?" is not under our control.**

For real-time approvals (operator on-call, approves in < 2 minutes), approach
B works. For typical approvals (Slack ping, someone responds in 20 minutes),
even approach B's held connection has likely dropped.

---

## 5. The chosen direction — Detective Governance + Dynamic Policy

### The reframe

Instead of trying to pause/hold individual requests for per-request human
approval, we shift to a model where:

1. **Policy decides immediately, every time** — allow or deny, instant, binary
2. **Everything is logged** — every decision, with full context
3. **A human reviews the log** and can:
   - See a **denied** request that should have been allowed → update policy to
     permit it next time
   - See an **allowed** request that should have been blocked → update policy
     to restrict it
4. **Policy evolves from observed real behaviour**, not from upfront guessing

This is the same model as:
- WAF (Web Application Firewall) — log traffic, adjust rules
- SELinux permissive mode → enforcing mode
- Network firewall rule evolution from observed traffic

### Why this is the right starting point

- **Zero client-side changes** — any MCP client works, unchanged, forever
- **No held connections** — the ext_authz binary constraint is no longer a
  limitation; it is the design
- **Works with every harness** — model is stateless, harness is irrelevant,
  the gateway just answers immediately
- **Policy becomes empirical** — rules reflect what agents actually do, not
  what we guessed upfront
- **The audit log becomes the core product** — not a side feature

### The hard limit of this model

**For irreversible actions, the damage is done by the time a human reviews.**

`delete_record` was deleted. The email was sent. The wire transfer happened.
Detective governance is not appropriate when:
- The action cannot be undone
- The first occurrence of a wrong decision causes real harm

**Mitigation (not workaround — a different design):**

Irreversible/destructive operations are **prohibited by default** at the policy
layer. An operator must explicitly grant per-agent, per-resource permission
*before* deployment. The human decision happens at setup time, not execution
time. The agent never waits; governance happened before the agent ran.

This is how production database access works: you don't approve each query in
real time. You grant a service account specific permissions at deploy time,
review grants periodically, revoke on wrong behaviour.

### The two-mode model

```
AUDIT mode    →  all calls pass through, everything logged
               →  humans observe what agents do
               →  policy suggestions surface from denied/anomalous calls
               →  use during onboarding and policy discovery

ENFORCE mode  →  policy strictly applied, denials are final
               →  human reviews log, evolves policy for next time
               →  for irreversible operations: blocked by default,
                  requires explicit operator grant before deployment

(No real-time per-request approval queue in either mode)
```

### The dynamic policy update flow

```
Agent calls tool
    → agentgate evaluates current policy
    → ALLOW: logged, forwarded
    → DENY:  logged with full context (agent, user, tool, args, reason, policy version)

Operator reviews dashboard
    → sees denial that was wrong (over-blocking)
    → clicks "update policy" — opens policy editor pre-populated with context
    → modifies Cedar rule, saves (commit to VCS optional)
    → agentgate reloads policy (file mount, no restart needed)
    → next call by that agent succeeds

    → sees allowance that was wrong (under-blocking)
    → tightens rule, saves, reloads
    → future calls denied
```

### Security requirements for policy mutation

Policy update is a **critical attack surface** — it is as powerful as the
policy itself. Requirements:

- [ ] Authentication on who can update policy (not open to anyone on the network)
- [ ] Audit trail of every policy change (who, when, what changed, which
      denial triggered it)
- [ ] Policy change approval — ideally separate reviewer from the person who
      saw the denial
- [ ] Rate limiting on policy evolution (prevent social-engineering operators
      into rapid policy loosening)
- [ ] VCS commit on every policy change (Cedar file is already version-tracked)
- [ ] Policy version in every audit log row (already implemented — SHA-256
      of file content)

---

## 6. Future exploration — connection holding for real-time approval

The team has agreed: **start with the detective governance + dynamic policy
approach**. Once that base exists, explore whether real-time per-request
approval is solvable. This section documents what to investigate.

### The core engineering question

Can we hold an MCP request open — across real-world HTTP timeouts, load
balancers, and diverse client harnesses — long enough for a human to act?

### Directions to investigate

**A. MCP progress notifications**

MCP spec includes `notifications/progress`. A gateway holding a connection
could send periodic progress pings while waiting for approval. This keeps the
connection alive and tells the harness "still working." Viability depends on
whether real harnesses respect these notifications and extend their own
timeouts accordingly. *Research: which harnesses implement
`notifications/progress` and what timeout behaviour do they exhibit?*

**B. Durable execution frameworks**

LangGraph `interrupt`, Temporal `signal`, Inngest — these frameworks have
first-class "pause, persist state, resume on external event" primitives. The
harness itself handles the wait; the gateway only needs to return a stable
transaction ID for the framework to track. *Research: what is the right
integration point? A LangGraph node? A Temporal activity?*

**C. Pre-authorization model (probably the right answer for high-stakes)**

Rather than approving each request in real time, an operator pre-authorizes a
specific agent to perform a specific action on specific resources. The
authorization is written into policy (SpiceDB or Cedar) before the agent runs.
At execution time, the gateway checks the pre-authorization and allows
instantly. The "human decision" happened at deploy time, not run time.
*This eliminates the waiting problem entirely for the highest-risk operations.*

**D. SSE long-hold with configurable operator SLA**

For deployments where the operator controls the full network path
(self-hosted, no external load balancer), configure all layers with a matching
timeout (e.g., 10 minutes). Approval must happen within the SLA window or the
call is denied. Appropriate for tightly-controlled environments with an
on-call operator. Not appropriate for production systems with external
ingress.

### Known constraint that any approach must respect

> The AI model is stateless between turns. "Waiting" is a property of the
> execution harness, not the model. The harness is not under our control.
> Any approach that requires the harness to support specific wait behaviour
> has non-universal adoption.

---

## 7. Component responsibility map (current state)

| Component | Type | Responsibility | Replaceable with |
|---|---|---|---|
| `agentgateway` | Off-the-shelf Rust | MCP proxy, JWT validation, ext_authz caller | Any proxy that supports ext_authz; or custom gateway (Approach B) |
| `Keycloak` | Off-the-shelf Java | JWT issuance, OIDC, custom claims | Okta, Auth0, Azure AD — any OIDC provider |
| `Cedar` | Policy language | Role-based rules, deny-by-default, auditable | Rego (OPA), simpler if/else code — Cedar preferred for human-readability |
| `SpiceDB` | Off-the-shelf Go | Per-object relationship graph | Any Zanzibar-style store; or simpler Postgres lookup for small deployments |
| `Postgres` | Off-the-shelf | Audit log, pending approvals | Any relational DB |
| `agentgate` | **Custom Go** | All decision logic — the actual product | N/A |
| `toy-server` | Custom Go demo | Stand-in for any real MCP tool | Any MCP server |

**The intelligence is concentrated in one service.** Every other component is
either infrastructure or config.

---

## 8. Deployment form (to be decided)

Three options, not mutually exclusive:

| Form | Description | Target customer |
|---|---|---|
| **Self-hosted Docker** | Compose stack, config-driven, mount Cedar file | Enterprises wanting on-premise governance |
| **Embedded Go library** | Import agentgate packages, bring your own infra | Developers integrating governance into existing systems |
| **Managed SaaS** | Hosted gateway + policy UI, multi-tenant | Teams that don't want to operate infrastructure |

Current POC is Form 1. Form 2 requires clean package boundaries (mostly done).
Form 3 requires multi-tenancy design (not started).

---

## 9. What is production-ready today vs what needs work

### Production-quality in the POC
- Cedar policy evaluation with fail-closed behaviour
- SpiceDB ownership checks with fully-consistent reads
- Postgres audit log (every decision, with policy version)
- TOCTOU re-check on approval (reload policy + re-query SpiceDB)
- JWT validation at the gateway layer (agentgateway handles this)
- JSON-RPC error format (denials are parseable, not transport errors)
- Policy version tracking (SHA-256 of file content, not git hash — handles
  uncommitted edits)

### Needs work before production
- **Policy mutation UI** — the "review denial → update policy" flow is the
  core of the chosen direction and does not exist yet
- **Policy change audit trail** — who changed what, when
- **Authentication on the approval/admin UI** — currently open on the network
- **Rate limiting** on agentgate's HTTP endpoints
- **Multi-tenancy** — Cedar and SpiceDB schemas are single-tenant today
- **Slack/Teams notifications** — Slack code exists, Teams does not; both need
  a public HTTPS endpoint for interactive callbacks
- **Tests for the HTTP approval layer** — unit tests exist for policy engine;
  HTTP handler tests do not

---

## 10. Key decisions log

| Decision | Chosen | Alternative | Reason |
|---|---|---|---|
| JWT validation placement | Gateway (agentgateway jwtAuth) | agentgate re-validates | Single validation point; agentgate trusts forwarded claims |
| Policy version tracking | SHA-256 of file content | Git commit hash | Handles uncommitted edits in TOCTOU demo correctly |
| SpiceDB consistency | fully_consistent | minimize_latency | TOCTOU re-check must see revocations that happened mid-flight |
| Denial format | JSON-RPC error (HTTP 200, code -32003) | HTTP 403 | MCP client libraries expect JSON-RPC; raw 403 causes parse failures |
| Human-in-the-loop model | Detective governance + dynamic policy | Per-request approval gate | Client-side requirement of approval gate reduces mass adoption; detective model is zero-friction |
| Irreversible actions | Blocked by default, explicit operator grant | Real-time approval queue | Approval queue requires harness cooperation we cannot guarantee |

