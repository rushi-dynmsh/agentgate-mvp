# AgentGate — Local Build Plan (v0.1)

Scope: not a throwaway PoC — a working local system, same tech stack as the target v1, minus the parts explicitly deferred by the technical architecture doc (dashboard polish, full compliance export, multi-gateway support). Every phase below should end with something you can actually exercise end-to-end, not just code that compiles.

## Stack decision

| Piece | Choice | Why |
|---|---|---|
| Gateway | `agentgateway` (real, run via Docker) | Not built by us — see prior discussion |
| Governance service | **Go** | Native Cedar (`cedar-go`, official), native SpiceDB (`authzed-go`, official), native gRPC for `ext_authz` |
| Policy engine | Cedar, embedded via `cedar-go` | No sidecar, no network hop for the hot-path decision |
| Relationship graph | SpiceDB (official Docker image) | Added in Phase 6, not Phase 1 — per docs, don't build it speculatively |
| Audit store | PostgreSQL | Append-only table |
| Identity | Real OIDC provider (Keycloak, local) issuing OBO-style tokens | Tests the actual primary identity design (Section 2a), not a shortcut |
| Approval channel | Real Slack app, real workspace | The whole point is testing a real async human-in-the-loop cycle |
| Toy target | 2–3 fake MCP tools you write | One safe (`read_record`), one destructive (`delete_record` or `transfer_funds`) |

## Local environment — what actually runs

```
docker-compose services (sketch, not final):
  agentgateway     — the real proxy, MCP listener in front of the toy server
  toy-mcp-server   — your own minimal MCP server (2-3 tools)
  agentgate        — your Go governance service (ext_authz gRPC server)
  postgres         — audit log + pending-approval table
  spicedb          — relationship graph (Phase 6+)
  keycloak         — local OIDC provider, simulates enterprise SSO
+ ngrok/cloudflared — tunnel so real Slack can reach your local interactive endpoint
+ a real Slack workspace + Slack app (free, external to docker-compose)
```

Two things can't live in docker-compose: Slack itself (it's a real external service — you need an actual free Slack workspace and an app with an interactivity endpoint), and the tunnel that exposes your local machine to it.

## Phased build plan

### Phase 0 — Skeleton
- docker-compose with agentgateway + your toy MCP server, nothing else yet.
- Confirm: an MCP client can call a tool through agentgateway and get a real response, no governance involved. This just proves the gateway plumbing works before you touch policy.

### Phase 1 — ext_authz wiring (allow-everything stub)
- Stand up the Go service as a minimal `ext_authz` gRPC server that always returns "allow," and configure agentgateway to call it on every route.
- Confirm: every tool call now visibly round-trips through your Go service (log it) before reaching the toy server. This is the single most important plumbing milestone — get this exactly right before adding any real logic.

### Phase 2 — Identity / OBO resolution
- Stand up Keycloak locally, configure a client, issue tokens that carry a real user claim.
- Your Go service validates the token (signature, audience, expiry) and extracts `agent_identity` + `on_behalf_of`.
- Confirm: two different Keycloak users produce two different resolved identities in your service's logs for the same agent/tool.

### Phase 3 — Policy engine (Cedar, flat checks only)
- Embed `cedar-go`. Hand-write a small policy set: role-based allow/deny, no relationship graph yet.
- Wire the `ext_authz` decision to actually call Cedar instead of always-allow.
- Default destructive action types (`delete_*`, `transfer_*`) to a `needs-approval` marker in policy — even though the approval flow doesn't exist yet, so the policy shape doesn't need to change later.
- Confirm: a call from an allowed role succeeds, a call from a disallowed role is rejected, both via real Cedar evaluation.

### Phase 4 — Audit log
- Postgres, append-only table, the schema from the architecture doc (Section 6): transaction id, agent identity, on-behalf-of, tool+args, policy version hash, decision, timestamps.
- Every decision from Phase 3 onward writes a row — including the boring "allowed" ones.
- Confirm: you can query Postgres after a test run and reconstruct exactly what happened and why for any given call.

### Phase 5 — Async approval flow (the hard part)
- Build the **custom pending-transaction + polling design** as your primary mechanism, not the MCP Tasks extension — it's still RC-stage as of mid-2026, and depending on it for your core flow is the wrong bet for a solo local build where you want something you can actually finish and trust.
- On `needs-approval`: write a pending-approval row, return a "parked" response to the client instead of blocking the connection.
- Real Slack app posts an interactive message (tool, args, actor) with approve/deny buttons; your service exposes an HTTP endpoint (via the tunnel) that receives Slack's callback.
- On approval: **re-run the Phase 3 policy check against current state before forwarding** — this is the TOCTOU mitigation from Section 5a, and it's cheap to build now versus retrofitting later. Treat it as required, not optional, even at this scale.
- Confirm: trigger a destructive call, watch it park, click approve in real Slack, watch it actually forward and complete — and separately, revoke the underlying permission between flag-time and approval-time and confirm the re-check catches it.

### Phase 6 — Relationship graph (SpiceDB) — stretch goal
- Only build this once Phase 5 works end-to-end. Add SpiceDB, model one real ownership/delegation relationship (Section 4's schema), and route one policy rule through a relationship check instead of a flat role check.
- Confirm: revoking a relationship in SpiceDB changes the authorization outcome for a call that previously succeeded.

### Phase 7 — Minimal dashboard — stretch goal
- A single page: pending approvals + audit log viewer. Not required to prove the architecture works, useful for demoing it.

## Sequencing notes

- Don't skip Phase 1's "always allow" stub — it isolates gateway/transport bugs from policy bugs, which is exactly the kind of thing that's miserable to debug once both are new and both are broken at once.
- Audit logging (Phase 4) comes before the approval flow (Phase 5) deliberately — same reasoning as the original docs: retrofitting audit onto an existing flow is harder than building it in from day one.
- The TOCTOU re-check in Phase 5 is small to add now and a real redesign to add later — don't defer it even though it's tempting to treat as hardening.
