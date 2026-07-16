# AgentGate — Execution Plan (Task Breakdown)

Source: `AgentGate_Local_Build_Plan.md` (7 phases) + `AgentGate_Verification_Log.md` (confirmed facts driving design constraints).

How to use this doc: each task is scoped to be completable in one sitting and independently checkable. Work top to bottom within a phase — most tasks depend on the one above it. Don't start a phase until the previous phase's "Confirm" gate passes; that ordering is deliberate (see Sequencing Notes in the build plan) and skipping it is exactly how you end up debugging two unknowns at once.

Checkbox convention: `[ ]` not started · `[~]` in progress · `[x]` done · `[!]` blocked (note why).

---

## Phase 0 — Skeleton

**Goal:** prove the gateway can front your toy server before governance exists at all.

- [ ] **0.1** Scaffold `docker-compose.yml` with just `agentgateway` + a placeholder `toy-mcp-server` service (can be a stub container for now).
- [ ] **0.2** Write the toy MCP server, tool 1: `read_record` (safe, read-only, returns fake data).
- [ ] **0.3** Add tool 2 to the toy server: `delete_record` (destructive — pick this over `transfer_funds`, it's simpler to reason about and equally good for testing the approval path; add `transfer_funds` later only if you want two destructive-action shapes).
- [ ] **0.4** Configure `agentgateway`'s config file to route MCP traffic to `toy-mcp-server` (stdio or HTTP, whichever your toy server speaks).
- [ ] **0.5** Wire up an MCP client (Claude Desktop, `mcp-cli`, or a small test script) pointed at agentgateway.
- [ ] **0.6 — Confirm gate:** client calls both tools through agentgateway and gets real responses back, zero governance involved. Don't proceed to Phase 1 until this is boring and reliable.

---

## Phase 1 — `ext_authz` wiring (allow-everything stub)

**Goal:** isolate transport/plumbing bugs from policy bugs — the single most important milestone per the build plan.

- [ ] **1.1** Scaffold the Go module for the governance service (`go mod init`, directory layout: `cmd/`, `internal/authz/`, etc.).
- [ ] **1.2** Add the Envoy `ext_authz` gRPC proto/service definitions (either vendor the proto or pull the generated Go bindings) — this is the same contract agentgateway, Envoy AI Gateway, Istio, and kgateway all speak, so get the proto right once.
- [ ] **1.3** Implement the gRPC server with a single handler that always returns "allow," nothing else.
- [ ] **1.4** Add a docker-compose service for the Go governance service; wire agentgateway's `extAuthz` config block to point at it (per-route or global — global is fine for now).
- [ ] **1.5** Add structured logging in the Go service: log every incoming check request (tool name, raw request) before returning allow.
- [ ] **1.6 — Confirm gate:** call `read_record` and `delete_record` through the client; watch both visibly hit the Go service's logs before reaching the toy server. No policy logic yet — just confirm the round trip.

---

## Phase 2 — Identity / OBO resolution

**Goal:** exercise the real identity design, not a shortcut.

- [ ] **2.1** Stand up Keycloak in docker-compose (official image), expose the admin console.
- [ ] **2.2** Create a realm, a client for the "agent," and two test users with distinct roles/attributes.
- [ ] **2.3** Configure token issuance so the access token carries a real user claim usable as `on_behalf_of` (custom claim mapper if needed).
- [ ] **2.4** Update the MCP client (or a small helper script) to fetch a token from Keycloak and attach it to tool calls (however agentgateway forwards auth context to `ext_authz` — likely via headers/metadata on the check request).
- [ ] **2.5** In the Go service: validate the JWT — signature against Keycloak's JWKS, audience, expiry.
- [ ] **2.6** Extract `agent_identity` and `on_behalf_of` from the validated token; log both.
- [ ] **2.7 — Confirm gate:** run the same tool call as two different Keycloak users; confirm the Go service's logs show two distinct resolved identities for the same agent/tool pair.

---

## Phase 3 — Policy engine (Cedar, flat checks only)

**Goal:** real authorization decisions, no relationship graph yet.

- [ ] **3.1** Add `cedar-policy/cedar-go` as a dependency; write a minimal schema (entities: `Agent`, `Tool`, roles).
- [ ] **3.2** Hand-write a small policy set: e.g. role `reader` → allow `read_record`; role `admin` → allow `read_record` + `delete_record`; everyone else → deny.
- [ ] **3.3** Add a policy rule/annotation that routes `delete_*` and `transfer_*` tool names to a `needs-approval` marker rather than a flat allow — even though the approval flow doesn't exist yet, so the policy shape is stable going into Phase 5.
- [ ] **3.4** Replace the Phase 1 always-allow stub: the `ext_authz` handler now builds a Cedar request from `agent_identity` + tool + resource, calls Cedar, and returns the real decision (allow / deny / needs-approval-as-deny-for-now).
- [ ] **3.5** Write a couple of unit tests directly against the Cedar policy set (bypassing gRPC) so you can iterate on policy logic fast without round-tripping through the gateway.
- [ ] **3.6 — Confirm gate:** a call from the `reader` user on `read_record` succeeds; the same user on `delete_record` is rejected (or parked, once you get to marker logic); an `admin` user succeeds on both — all via real Cedar evaluation, visible in logs.

---

## Phase 4 — Audit log

**Goal:** every decision, allowed or not, becomes a permanent reconstructible record — built in now, not retrofitted later.

- [ ] **4.1** Design the Postgres schema per the architecture doc's Section 6: transaction id, agent identity, on-behalf-of, tool name + args, policy version hash, decision, timestamps (issued / decided).
- [ ] **4.2** Add Postgres to docker-compose; write the migration (plain SQL file or a lightweight migration tool — `golang-migrate` is a reasonable default for a Go project).
- [ ] **4.3** Add a Postgres client to the Go service (`pgx` is the standard choice).
- [ ] **4.4** Compute a policy version hash (hash of the loaded Cedar policy set at decision time) and include it in every row.
- [ ] **4.5** Wire the audit write into the `ext_authz` handler: every decision from Phase 3 onward — including plain "allowed" ones — writes a row before the response goes back.
- [ ] **4.6 — Confirm gate:** run a short mixed batch of calls (some allowed, some denied), then query Postgres directly and reconstruct exactly what happened and why for each one, matching your logs.

---

## Phase 5 — Async approval flow (the hard part)

**Goal:** the custom pending-transaction + polling design, with the TOCTOU re-check as a required step, not hardening.

- [ ] **5.1** Design the pending-approval table: transaction id (FK to audit log), state (`pending`/`approved`/`denied`/`expired`), original request payload, created-at, decided-at, decided-by.
- [ ] **5.2** Implement the "parked" response: when Cedar returns `needs-approval`, the Go service writes a pending row and returns a non-blocking response to the client (however your client/gateway pairing represents "parked" — e.g. a distinct status the client can poll or a synthetic MCP error the client is expected to retry on).
- [ ] **5.3** Create the real Slack app: register it in a real workspace, enable Interactivity, note you'll need a public HTTPS endpoint for the callback.
- [ ] **5.4** Stand up the tunnel (ngrok or cloudflared) pointing at the Go service's (soon-to-exist) HTTP callback port; register that public URL as the Slack app's interactivity endpoint.
- [ ] **5.5** Implement the Slack notification: on parking a transaction, post an interactive message showing tool, args, and actor, with Approve/Deny buttons encoding the transaction id.
- [ ] **5.6** Implement the HTTP callback endpoint in the Go service that receives Slack's button-click payload, verifies Slack's signing secret, and looks up the pending transaction.
- [ ] **5.7** On approval: **re-run the Phase 3 Cedar check against current state** before forwarding — this is the TOCTOU mitigation from Section 5a. Treat a failed re-check as a fresh deny, logged as such.
- [ ] **5.8** On a passing re-check: forward the original call to the toy MCP server, capture the result, mark the pending row `approved`/completed, write a final audit row.
- [ ] **5.9** On deny (either the human clicking Deny, or the re-check failing): mark the row accordingly, write the audit row, surface the outcome back to the client if your polling design supports that.
- [ ] **5.10 — Confirm gate (happy path):** trigger `delete_record`, watch it park, click Approve in real Slack, watch it actually forward and complete end-to-end.
- [ ] **5.11 — Confirm gate (TOCTOU):** trigger `delete_record`, and *before* clicking Approve, revoke the underlying Cedar permission (edit the policy / demote the user's role) — then click Approve and confirm the re-check catches it and denies.

---

## Phase 6 — Relationship graph (SpiceDB) — stretch goal

**Only start after Phase 5's both confirm gates pass.**

- [ ] **6.1** Add SpiceDB to docker-compose (official image); write a minimal schema modeling one real ownership/delegation relationship (per the architecture doc's Section 4).
- [ ] **6.2** Add `authzed-go` to the Go service; wire a client against the local SpiceDB instance.
- [ ] **6.3** Seed one relationship tuple (e.g. `user:alice` owns `record:123`).
- [ ] **6.4** Route exactly one Cedar policy rule to consult SpiceDB instead of a flat role check (e.g. "you may `delete_record` on a resource you own").
- [ ] **6.5 — Confirm gate:** a call that succeeds under the relationship-based rule stops succeeding once you revoke that relationship tuple in SpiceDB — same call, same user, different authorization outcome.

---

## Phase 7 — Minimal dashboard — stretch goal

- [ ] **7.1** Single page, pending-approvals view: list open rows from the pending-approval table.
- [ ] **7.2** Single page, audit-log viewer: paginated read of the Postgres audit table.
- [ ] **7.3** Wire both to simple read-only HTTP endpoints on the Go service (no new write paths needed — Slack remains the only way to actually approve/deny).

---

## Cross-cutting notes (apply throughout, not phase-specific)

- **Binary `ext_authz` constraint (confirmed in the verification log):** the gateway itself has no "pause" state. Every "parked" behavior in Phase 5 is entirely your service's responsibility — the gateway only ever sees allow/deny from you. Keep this in mind when designing the client-facing "parked" response in 5.2; there's no gateway-native primitive to lean on.
- **Don't let Phase 6/7 creep earlier.** They're explicitly stretch goals — the build plan is deliberate about SpiceDB not being needed for the core loop.
- **Policy version hash (4.4)** only means something if policies are actually versioned somewhere (git commit hash of the policy file is the simplest option) — decide that convention before Phase 4, not during.
