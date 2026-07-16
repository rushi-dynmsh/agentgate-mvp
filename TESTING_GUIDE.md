# AgentGate — Testing & Playing Guide

A tour of everything the platform can do, from "hello world" to actively
trying to break it. Each scenario says **what to do**, **what you should
see**, and **why it happens** — the *why* is where the learning is.

Setup for all scenarios: stack running (`docker compose up -d`), and ideally
one terminal tailing decisions:

```powershell
docker compose logs -f agentgate
```

Client commands run from the `client/` directory.

---

## Level 1 — Basics: the happy paths

### 1.1 See the tools through the gateway

```powershell
go run . -list -user alice
```

**Expect:** `read_record` and `delete_record` listed.
**Why it matters:** the client is talking to `localhost:3000` (the gateway),
not the toy server. Even *listing tools* went through a full check — see the
`tools/list … allow (protocol message)` line in the agentgate log.

### 1.2 A reader reads

```powershell
go run . -tool read_record -id 1 -user bob
```

**Expect:** the Acme Corp record.
**Log shows:** `on_behalf_of="bob" roles=[reader] → allow (cedar=allow risk=read)`.
**Why:** the Cedar rule `permit (principal in Role::"reader", …) when { resource.risk == "read" }` matched.

### 1.3 An admin reads

```powershell
go run . -tool read_record -id 1 -user alice
```

**Expect:** same record, but the log now says `roles=[admin]` — same agent
software, different human behind it. This is the on-behalf-of (OBO) model in
one picture.

---

## Level 2 — Authorization: watching denials

### 2.1 A reader tries to delete

```powershell
go run . -tool delete_record -id 1 -user bob
```

**Expect:** a clean error: `policy denied delete_record for bob (roles [reader])`.
**Why:** no Cedar rule permits a reader on a destructive tool, and Cedar's
default is deny. Note what the toy server saw: **nothing**. The call died at
the gate. Check: `docker compose logs toy-mcp-server` has no delete line.

### 2.2 No identity at all

```powershell
go run . -tool read_record -id 1
```

**Expect:** `401 … no bearer token found`.
**Why:** this rejection came from **agentgateway itself** (the `jwtAuth`
policy in `gateway/config.yaml`, mode `strict`) — agentgate never even saw
the request. Confirm: no new line in the agentgate log. Two different layers
are defending here.

### 2.3 Wrong password

```powershell
go run . -tool read_record -id 1 -user alice -password nope
```

**Expect:** `keycloak: Invalid user credentials` — rejected a layer even
earlier, by Keycloak, before any tool call was attempted.

### 2.4 A forged token

```powershell
go run . -tool read_record -id 1 -token "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJoYWNrZXIifQ.fake"
```

**Expect:** `401 jwt verification fails`.
**Why:** the gateway validates the token's *cryptographic signature* against
Keycloak's published keys (JWKS). You can't just claim to be someone.

---

## Level 3 — The approval flow (the main event)

### 3.1 Happy path: park → approve → execute

Step 1 — as alice (admin), try something destructive:

```powershell
go run . -tool delete_record -id 2 -user alice
```

**Expect:** NOT a success and NOT a denial, but a third thing:
`delete_record is destructive and requires human approval; transaction <id> is pending`.

**Why:** Cedar said *allow* (alice is admin), but the tool matches a
destructive prefix (`delete_`), so agentgate parked it instead of forwarding.
Log: `→ needs_approval (cedar=allow risk=destructive)` then `parked for approval`.

Step 2 — be the human: open **http://localhost:8090**. Your parked call is
sitting there with who/what/args. Click **Approve**.

**Expect:** the page shows `approved and executed: [... "record 2 deleted"]`.

Step 3 — verify it really happened:

```powershell
go run . -tool read_record -id 2 -user alice
```

**Expect:** `no record with id "2"` — the delete truly executed, but only
*after* a human said yes.

Step 4 — read the story in the audit log:

```powershell
docker exec agentgate-postgres psql -U agentgate -c "SELECT method, decision, reason FROM audit_log ORDER BY id DESC LIMIT 3;"
```

**Expect:** two linked rows — `tools/call → needs_approval`, then
`approval/executed_after_approval` with `approved by local-approver; re-check passed`.

### 3.2 The human says no

```powershell
go run . -tool delete_record -id 3 -user alice
```

Open http://localhost:8090 and click **Deny**.

**Expect:** UI shows `denied`; the audit log gets `denied_by_human`; and
record 3 still exists (verify with a read). The agent was never told yes.

### 3.3 Approve via the API instead of the UI

Park another call, copy the transaction id from the error message, then:

```powershell
curl.exe -s -X POST http://localhost:8090/decide -H "Accept: application/json" -d "transaction_id=<TX-ID>&decision=approve&decided_by=your-name"
```

**Expect:** JSON with the outcome. `decided_by` lands in the audit trail —
approvals are attributable.

### 3.4 Check a transaction's status any time

```powershell
curl.exe -s http://localhost:8090/status/<TX-ID>
curl.exe -s http://localhost:8090/pending
```

This is how an agent (or a dashboard) would poll whether its parked call
went through.

### 3.5 Double-decision race

Park a call, open http://localhost:8090 in **two browser tabs**, click
Approve in one and Deny in the other.

**Expect:** the first click wins; the second says
`already decided (or unknown transaction)`.
**Why:** the state transition is a single SQL `UPDATE … WHERE state='pending'` —
the database serializes racing humans.

---

## Level 4 — TOCTOU: the flagship security demo

*Time-of-check-to-time-of-use: what if permissions change while a call sits
parked?*

Step 1 — park a destructive call as alice:

```powershell
go run . -tool delete_record -id 1 -user alice
```

Step 2 — **before approving**, revoke alice's destructive power. Edit
`policies/agentgate.cedar` and change the admin rule to:

```cedar
permit (
  principal in AgentGate::Role::"admin",
  action == AgentGate::Action::"call_tool",
  resource
) when { resource.risk == "read" };
```

(No restart needed — the next step re-reads the file.)

Step 3 — now click **Approve** at http://localhost:8090.

**Expect:** `approval voided: re-check denied: cedar=deny risk=destructive roles=[admin]`.

The human said yes, and the system still refused — because at *execution*
time the policy no longer allowed it. In the audit log, compare
`policy_version` on the park row vs the void row: they differ. The log proves
the re-check ran against the newer policy.

Step 4 — undo your policy edit (restore the rule without the `when` clause),
then `docker compose restart agentgate` so normal calls see the restored
policy too.

---

## Level 5 — Policy playground: change the rules

Restart agentgate after each policy edit (`docker compose restart agentgate`).

### 5.1 Take everything away from readers

Comment out the reader `permit` block (Cedar comments are `//`).
**Expect:** bob now gets denied even on `read_record`. Default-deny in action.

### 5.2 Give readers delete rights — but approval still gates it

Change the reader rule's condition to `when { true }`.
**Expect:** bob's `read_record` still allowed, and bob's `delete_record` now
returns **pending approval** instead of a deny.
**Why:** the destructive-tool parking is a separate layer *on top of* Cedar —
policy says "may", AgentGate still insists a human confirms destruction.

### 5.3 Deny a specific person by name

Add a `forbid` rule (forbid always beats permit in Cedar):

```cedar
forbid (
  principal == AgentGate::User::"alice",
  action,
  resource
);
```

**Expect:** alice — the admin — is now denied everything, while bob still works.

### 5.4 Break the policy file on purpose

Delete a semicolon and restart agentgate.
**Expect:** the container exits with `parse cedar policies: …` (check
`docker compose logs agentgate`), and *the gateway then fails closed* — calls
get `403 external authorization failed`, not silent-allow. Fix the file,
restart agentgate, then `docker compose restart agentgateway`.

### 5.5 Run the policy unit tests

```powershell
docker run --rm -v "${PWD}:/repo" -w /repo/agentgate -e GOFLAGS=-buildvcs=false golang:1.26-alpine go test ./internal/policy/ -v
```

These evaluate the real shipped `.cedar` file — a bad policy edit fails tests
before it ever reaches the gateway. (Docker because Windows App Control
blocks locally-built test binaries.)

---

## Level 6 — Identity playground

### 6.1 A user with no roles

In the Keycloak admin console (http://localhost:8081, admin/admin): realm
`agentgate` → Users → Add user (`charlie`, set a password under
Credentials, temporary OFF). Assign **no** roles.

```powershell
go run . -tool read_record -id 1 -user charlie -password <what-you-set>
```

**Expect:** denied — `roles=[]` in the log, and no Cedar rule matches a
role-less principal. Authentication succeeded; authorization said no. Those
are different questions and this scenario shows the seam.

### 6.2 Promote charlie live

Users → charlie → Role mapping → assign `reader`. Re-run the same command.
**Expect:** allowed now. No restarts anywhere — the next token simply carries
the new role. (Remember: console changes vanish if you recreate the Keycloak
container; the permanent home is `keycloak/realm-agentgate.json`.)

### 6.3 Look inside a token

```powershell
$t = (curl.exe -s -X POST http://localhost:8081/realms/agentgate/protocol/openid-connect/token -d "grant_type=password&client_id=agent-client&username=alice&password=alice-password" | ConvertFrom-Json).access_token
# decode the payload (middle segment) — add base64 padding then decode:
$p = $t.Split('.')[1].Replace('-','+').Replace('_','/'); $p = $p.PadRight($p.Length + (4 - $p.Length % 4) % 4, '=')
[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($p))
```

**Look for:** `"on_behalf_of": "alice"`, `"roles": ["admin"]`,
`"azp": "agent-client"` — exactly the three claims agentgate resolves into
who/for-whom/what-may-they.

---

## Level 7 — Under the hood

### 7.1 Speak raw MCP with curl (no client CLI)

```powershell
$t = (curl.exe -s -X POST http://localhost:8081/realms/agentgate/protocol/openid-connect/token -d "grant_type=password&client_id=agent-client&username=bob&password=bob-password" | ConvertFrom-Json).access_token
curl.exe -si http://localhost:3000/mcp -H "Authorization: Bearer $t" -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"delete_record\",\"arguments\":{\"id\":\"1\"}}}'
```

**Expect:** a JSON-RPC error object with `"status": "denied"` in its data —
this is exactly what an AI agent's MCP library receives and can reason about.

### 7.2 Prove the gateway is the only door

```powershell
curl.exe -s http://localhost:8080/mcp -H "Content-Type: application/json" -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"delete_record\",\"arguments\":{\"id\":\"1\"}}}'
```

**Expect:** it works — no auth, no policy, record deleted. **This is the
lesson, not a bug:** the toy server's port 8080 is only published to your
localhost for demo purposes. In a real deployment the tool server is
reachable *only* from the gateway's network, and this bypass wouldn't exist.
(Restart `toy-mcp-server` to get the record back.)

### 7.3 Watch the gateway's own view

Open http://localhost:15000/ui → your bind on 3000 → the route shows both
policies (`jwtAuth`, `extAuthz`) and the MCP backend. This is the
configuration actually loaded, not what you *think* the YAML says — useful
whenever config "doesn't seem to apply".

### 7.4 Forensics: reconstruct a whole session

```powershell
docker exec agentgate-postgres psql -U agentgate -c "SELECT to_char(created_at,'HH24:MI:SS') t, on_behalf_of, method, tool, decision FROM audit_log WHERE created_at > now() - interval '15 minutes' ORDER BY id;"
```

Every experiment you just ran is in there — including the denials and the
voided approval. That completeness *is* the audit-log guarantee: the answer
to "what did the agent do while I wasn't looking?" is a query, not a guess.

---

## Scenario checklist

Tick these off and you've exercised the entire platform:

- [ ] 1.1 list tools · 1.2 reader reads · 1.3 admin reads
- [ ] 2.1 reader denied delete · 2.2 no token → 401 · 2.3 bad password · 2.4 forged token
- [ ] 3.1 park→approve→execute · 3.2 human denies · 3.3 API approve · 3.4 status poll · 3.5 double-decision race
- [ ] 4 TOCTOU voided approval (the flagship)
- [ ] 5.1 revoke readers · 5.2 readers + approval · 5.3 forbid one user · 5.4 broken policy fails closed · 5.5 policy tests
- [ ] 6.1 role-less user denied · 6.2 live promotion · 6.3 decode a token
- [ ] 7.1 raw curl MCP · 7.2 the bypass lesson · 7.3 gateway UI · 7.4 forensics query
