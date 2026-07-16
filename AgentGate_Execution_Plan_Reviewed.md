# AgentGate — Execution Plan (Reviewed & Verified)

Reviewed against live agentgateway docs, the agentgateway GitHub repo, and the official `go-control-plane`/`cedar-go` sources. Original structure and sequencing from the offline-generated plan are preserved — they were sound. Corrections below are all in the "exact mechanics" layer, which is exactly where an AI without internet access can't help but guess.

## Review summary — what changed and why

| # | Task | Original | Issue | Verdict |
|---|---|---|---|---|
| 1 | 0.1/0.4 | Toy MCP server as a generic docker-compose service, "stdio or HTTP, whichever" | stdio backends are spawned **directly by agentgateway itself** (`cmd`/`args` in its config, no separate process boundary) — not automatically a clean networked service. Real, but has a real gotcha: it requires the runtime (e.g. Node for `npx`) inside agentgateway's own container. | **Corrected** — recommend HTTP as the default for the toy server |
| 2 | 1.2 | "Vendor the proto or pull generated bindings" | Vague. There's a real, official, pre-generated Go package for this exact purpose. | **Corrected** — named directly |
| 3 | 1.4 | "extAuthz config... per-route or global" | "Global" isn't quite how agentgateway's attachment model works — policies attach at listener/route/backend with merge semantics, no single global switch. | **Corrected** — precise YAML shown |
| 4 | 2.4/2.5 | "however agentgateway forwards auth context — likely via headers/metadata" | Hedged unnecessarily. This is documented and confirmed: the `Authorization` header is included by default. | **Confirmed, de-hedged** |
| 5 | 5.2 | "Parked" response left undefined | Genuinely underspecified — MCP has no native "pending" status outside the Tasks extension we're deliberately not using. This needed an actual design decision, not a placeholder. | **Filled in** |
| — | *(missing entirely)* | — | agentgateway ships a **built-in Admin UI** (port 15000) and an **`agctl` CLI** with a request tracer (`agctl trace`) — real, verified tools that make Phase 0/1's confirm-gates far easier to validate than log-reading alone. | **Added** |
| — | *(missing entirely)* | — | agentgateway has its own **native JWT validation policy**, which can run *before* `ext_authz` and hand your service pre-validated claims. Re-implementing JWT signature verification in the Go service is redundant work and a second place for that logic to be wrong. | **Added as a design improvement** |
| — | 3.1 | Cedar schema — treated as a settled feature | `cedar-go`'s schema validator is officially marked experimental (`x/exp/schema`). Not a blocker, but the plan shouldn't imply it's as stable as the core authorizer. | **Caveat added** |

Nothing in the original plan was fabricated outright — the offline AI reasoned correctly about *what* needed to happen at each phase, it just couldn't check *how* agentgateway specifically expects it wired, because it had no way to read agentgateway's actual docs. That's exactly the gap this pass closes.

---

## Phase 0 — Skeleton

**Goal:** prove the gateway can front your toy server before governance exists at all.

- [ ] **0.1** Write the toy MCP server as its own small **HTTP-based** service (Streamable HTTP or SSE), run as its own docker-compose container — not stdio. Reasoning: agentgateway's stdio backend spawns the process itself (`cmd: npx`, `args: [...]`), which means the runtime for your toy server has to live *inside agentgateway's own container* — a real, documented gotcha (their own docs warn about `npx not found` for exactly this reason). Keeping the toy server as a separate HTTP service is cleaner, avoids polluting the gateway's container, and matches how a real MCP server would actually be deployed.
- [ ] **0.2** Tool 1: `read_record` (safe, read-only, returns fake data).
- [ ] **0.3** Tool 2: `delete_record` (destructive).
- [ ] **0.4** Write `config.yaml` for agentgateway using the real local-config schema:
  ```yaml
  # yaml-language-server: $schema=https://agentgateway.dev/schema/config
  binds:
    - port: 3000
      listeners:
        - routes:
            - backends:
                - mcp:
                    targets:
                      - name: toy-server
                        mcp:
                          host: http://toy-mcp-server:8080/mcp
  ```
  Run with `agentgateway -f config.yaml`. Config is hot-reloaded on file change — no restart needed as you iterate.
- [ ] **0.5** Wire up an MCP client (Claude Desktop, `mcp-cli`, or a small test script) pointed at agentgateway on port 3000.
- [ ] **0.6** *(new)* Open agentgateway's built-in Admin UI at `http://localhost:15000/ui` — confirm your bind/listener/route/backend show up as expected before touching the client. This catches config mistakes before you even get to the client-call stage.
- [ ] **0.7 — Confirm gate:** client calls both tools through agentgateway and gets real responses back, zero governance involved. Don't proceed to Phase 1 until this is boring and reliable.

---

## Phase 1 — `ext_authz` wiring (allow-everything stub)

**Goal:** isolate transport/plumbing bugs from policy bugs.

- [ ] **1.1** Scaffold the Go module (`go mod init`, `cmd/`, `internal/authz/`, etc.).
- [ ] **1.2** Add the dependency directly — no vendoring or manual proto generation needed:
  ```
  go get github.com/envoyproxy/go-control-plane/envoy/service/auth/v3
  ```
  This is the official, pre-generated Go package for the exact `Authorization`/`Check` gRPC service agentgateway speaks. Import as `auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"`.
- [ ] **1.3** Implement the gRPC server:
  ```go
  func (s *Server) Check(ctx context.Context, req *auth_pb.CheckRequest) (*auth_pb.CheckResponse, error) {
      log.Printf("check: %+v", req.Attributes.Request.Http)
      return &auth_pb.CheckResponse{
          Status: &status.Status{Code: int32(codes.OK)},
      }, nil
  }
  ```
  Register it: `auth_pb.RegisterAuthorizationServer(grpcServer, &Server{})`. That's the entire always-allow stub — worth knowing the shape now since Phase 3/5 just change what gets returned, not the plumbing.
- [ ] **1.4** Add the Go service to docker-compose, and point agentgateway at it with the real attachment syntax — attach at the **route** level (covers every backend under that route without repeating config per-backend):
  ```yaml
  routes:
    - policies:
        extAuthz:
          host: agentgate-service:9000
          protocol:
            grpc: {}
      backends:
        - mcp:
            targets: [...]
  ```
- [ ] **1.5** Structured logging: log every incoming check request (tool name, raw request) before returning allow.
- [ ] **1.6 — Confirm gate:** call `read_record` and `delete_record`; watch both hit the Go service's logs before reaching the toy server. **Also run `agctl trace`** (agentgateway's request tracer, part of its CLI) alongside your own logs — it shows the matched route, applied policies, and chosen backend for the next live request, which is a much faster way to confirm the `ext_authz` hop is actually in the path than log-reading alone.

---

## Phase 2 — Identity / OBO resolution

**Goal:** exercise the real identity design.

- [ ] **2.1** Stand up Keycloak in docker-compose, expose the admin console.
- [ ] **2.2** Create a realm, an "agent" client, two test users with distinct roles/attributes.
- [ ] **2.3** Configure a claim mapper so the access token carries a usable `on_behalf_of` claim.
- [ ] **2.4** Update your test client to fetch a token from Keycloak and send it as a standard `Authorization: Bearer <token>` header on tool calls. **Confirmed, not hedged:** agentgateway includes the `Authorization` header in the `ext_authz` check request by default — no extra `includeRequestHeaders` config needed for this specific header (that setting is only needed for *additional* headers, like cookies).
- [ ] **2.5** *(design improvement over the original plan)* Consider validating the JWT's signature/audience/expiry **at the gateway**, not in your Go service, using agentgateway's own `jwtAuthentication` policy against Keycloak's JWKS endpoint. This runs *before* `ext_authz` in agentgateway's request pipeline, and validated claims can be forwarded to your service via the `extAuthz.protocol.grpc.metadata` field (CEL expression, e.g. `dev.agentgateway.jwt: '{"claims": jwt}'`). This offloads token-integrity checking to the already-hardened Rust proxy instead of re-implementing it — one less place for that logic to have a bug. If you'd rather keep it simple for now, validating the JWT yourself in Go (as originally planned) is still correct, just redundant with a capability the gateway already has.
- [ ] **2.6** Extract `agent_identity` and `on_behalf_of` from the (validated) claims; log both.
- [ ] **2.7 — Confirm gate:** run the same tool call as two different Keycloak users; confirm the Go service's logs show two distinct resolved identities for the same agent/tool pair.

---

## Phase 3 — Policy engine (Cedar, flat checks only)

**Goal:** real authorization decisions, no relationship graph yet.

- [ ] **3.1** Add `cedar-policy/cedar-go`. Write a minimal schema (entities: `Agent`, `Tool`, roles). **Caveat:** the schema *validator* in `cedar-go` is officially experimental (`x/exp/schema`) as of mid-2026 — the core authorizer is stable and corpus-tested against the Rust reference implementation, but don't build hard dependencies on strict schema validation yet; treat it as a nice-to-have, not load-bearing.
- [ ] **3.2** Hand-write a small policy set: role `reader` → allow `read_record`; role `admin` → allow both; everyone else → deny.
- [ ] **3.3** Route `delete_*`/`transfer_*` tool names to a `needs-approval` marker rather than a flat allow, so the policy shape doesn't change going into Phase 5.
- [ ] **3.4** Replace the Phase 1 stub: the `Check` handler builds a Cedar request from `agent_identity` + tool + resource, evaluates it, and returns the real decision. For a deny, populate the `DeniedResponse` variant of `CheckResponse.HttpResponse` (status code + optional body) rather than leaving it empty — an empty deny gives you no visibility into *why* from the client side.
- [ ] **3.5** Unit test the Cedar policy set directly, bypassing gRPC, for fast iteration.
- [ ] **3.6 — Confirm gate:** `reader` succeeds on `read_record`, rejected on `delete_record`; `admin` succeeds on both — all via real Cedar evaluation, visible in logs.

---

## Phase 4 — Audit log

**Goal:** every decision becomes a permanent, reconstructible record.

- [ ] **4.1** Postgres schema: transaction id, agent identity, on-behalf-of, tool name + args, policy version hash, decision, timestamps.
- [ ] **4.2** Add Postgres to docker-compose; migrations via `golang-migrate` or a plain SQL file.
- [ ] **4.3** `pgx` as the Postgres client.
- [ ] **4.4** Policy version hash: use the git commit hash of the policy file — decide this convention now, not mid-Phase-4.
- [ ] **4.5** Wire the audit write into the `Check` handler: every decision, including plain allows, writes a row before the response goes back.
- [ ] **4.6 — Confirm gate:** run a mixed batch of calls, query Postgres, reconstruct exactly what happened and why for each.

---

## Phase 5 — Async approval flow

**Goal:** the custom pending-transaction + polling design, with the TOCTOU re-check as required.

- [ ] **5.1** Pending-approval table: transaction id (FK to audit log), state, original request payload, created-at, decided-at, decided-by.
- [ ] **5.2** *(filled in — the original plan left this undefined)* Design the "parked" response concretely: MCP has no native pending/retry status outside the Tasks extension. The realistic mechanism for a custom design is: return a `DeniedResponse` whose body is a clear, structured message — e.g. `{"status": "pending_approval", "transaction_id": "abc123"}` — surfaced back through the tool call's error/result text. The calling agent (or the human driving it) is the one who re-invokes the tool later, or you optionally expose a `check_approval_status` tool on the toy server that the agent can poll. There's no gateway-native "hold the connection open" primitive to lean on — that's the direct consequence of `ext_authz` being a binary allow/deny protocol, confirmed earlier.
- [ ] **5.3** Real Slack app: register it, enable Interactivity, note the need for a public HTTPS endpoint.
- [ ] **5.4** Tunnel (ngrok/cloudflared) to your Go service's HTTP callback port; register that URL as the Slack app's interactivity endpoint.
- [ ] **5.5** Slack notification: on parking a transaction, post an interactive message (tool, args, actor) with Approve/Deny buttons encoding the transaction id.
- [ ] **5.6** HTTP callback endpoint: receive Slack's payload, verify the signing secret, look up the pending transaction.
- [ ] **5.7** On approval: **re-run the Phase 3 Cedar check against current state** before forwarding. A failed re-check is a fresh deny, logged as such.
- [ ] **5.8** On a passing re-check: forward the original call to the toy MCP server, capture the result, mark the row completed, write a final audit row.
- [ ] **5.9** On deny (human click or failed re-check): mark the row, write the audit row, surface the outcome per the mechanism decided in 5.2.
- [ ] **5.10 — Confirm gate (happy path):** trigger `delete_record`, watch it park, click Approve in real Slack, watch it forward and complete end-to-end.
- [ ] **5.11 — Confirm gate (TOCTOU):** trigger `delete_record`; before clicking Approve, revoke the underlying Cedar permission; click Approve; confirm the re-check catches it and denies.

---

## Phase 6 — Relationship graph (SpiceDB) — stretch goal

**Only start after both Phase 5 confirm gates pass.**

- [ ] **6.1** Add SpiceDB to docker-compose; minimal schema, one ownership/delegation relationship.
- [ ] **6.2** Add `authzed-go` (official client); wire it against local SpiceDB.
- [ ] **6.3** Seed one relationship tuple.
- [ ] **6.4** Route exactly one Cedar policy rule through a SpiceDB check instead of a flat role check.
- [ ] **6.5 — Confirm gate:** revoking the relationship tuple changes the authorization outcome for a previously-succeeding call.

---

## Phase 7 — Minimal dashboard — stretch goal

- [ ] **7.1** Pending-approvals view.
- [ ] **7.2** Audit-log viewer.
- [ ] **7.3** Read-only HTTP endpoints on the Go service — Slack stays the only write path for approve/deny.

---

## Cross-cutting notes

- **Binary `ext_authz` constraint (confirmed):** the gateway has no "pause" state — every "parked" behavior in Phase 5 is entirely your service's responsibility, and there's no gateway-native primitive to lean on for it. This is now a fully worked-out design (5.2), not just a known limitation.
- **Debugging tools that weren't in the original plan:** `agentgateway`'s Admin UI (`:15000/ui`) and the `agctl` CLI (`agctl config`, `agctl trace`) are real, and materially faster than log-reading for confirming Phase 0/1's gates. Use them.
- **Don't let Phase 6/7 creep earlier** — they're deliberately stretch goals.
- **Policy version hash (4.4)** only means something once policies are actually versioned — decide the convention (git commit hash) before Phase 4, not during.
- **The JWT-validation-at-the-gateway option (2.5)** is a genuine architectural choice, not a correction of an error — either approach (gateway-side or Go-service-side validation) works; the gateway-side option is simply less code for you to get right.
