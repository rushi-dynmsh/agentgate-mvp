# AgentGate — Live Demo Script

A precise, sequential walkthrough. Every command and every result below was
run on the actual system and copied verbatim — if you follow it top to bottom,
you will see exactly these outputs.

Read the **"Understand this first"** box, do the **"One-time setup"** once, then
run **Steps 1–10 in order**.

---

## Understand this first (30 seconds)

**What AgentGate is:** a checkpoint that sits between an AI agent and the tools
it wants to use. The agent cannot touch a tool directly — every tool call must
pass through AgentGate, which decides *allow / deny / needs-human-approval*.

**The demo cast:**

| Name | Who | Role | Owns records |
|------|-----|------|--------------|
| **alice** | a human | **admin** (may read + delete) | records **1** and **2** |
| **bob** | a human | **reader** (may only read) | nothing |

There are 3 fake customer records (ids `1`, `2`, `3`). The AI agent always acts
*on behalf of* one of these humans.

**Two independent checks a delete must pass:**
1. **Role** (does your job allow this action?) — decided by a policy file (Cedar).
2. **Ownership** (do you own *this specific* record?) — decided by a graph (SpiceDB).

> ⚠️ **Terminal note — do not panic during the demo.** The demo client is a
> command-line program that *exits with an error code* whenever a call is denied
> or parked (that is correct behaviour). PowerShell then prints a **red block**
> that starts with `At line:1 char:1` and ends with `NativeCommandError`. **That
> red text is just PowerShell's wrapper — ignore it.** The line that matters is
> the one right after `call tool:`. I point this out at each step.

---

## One-time setup (before the audience arrives)

You need **two terminals** and **one browser window**.

### Terminal A — start everything and watch the live decisions

```powershell
cd C:\Users\User\Downloads\2_AccessGraph_Hackathon\agentgate-platform
docker compose up -d
```

Wait ~30 seconds, then confirm all six services are `Up`:

```powershell
docker compose ps
```

You should see `agentgate`, `agentgateway`, `keycloak`, `spicedb`,
`toy-mcp-server`, and `agentgate-postgres` — all `Up` (postgres says
`Up (healthy)`).

Now stream AgentGate's decision log and **leave this terminal open** — during
the demo you can point at it to show each live decision:

```powershell
docker compose logs -f agentgate
```

You'll know it's ready when the last line is:
`agentgate ext_authz service listening on :9000`.

### Terminal B — where you'll type the demo commands

Open a **second** terminal. Every numbered step below is run from here:

```powershell
cd C:\Users\User\Downloads\2_AccessGraph_Hackathon\agentgate-platform\client
```

### Browser — the dashboard

Open **http://localhost:8090/dashboard**. This is a **read-only** view with
three panels that refresh every 3 seconds:
- **Pending approvals** — destructive calls waiting for a human
- **Ownership graph** — who owns which record
- **Audit log** — every decision, colour-coded (green = allowed, red = denied,
  amber = pending)

Keep it on screen throughout — it reacts live to every step.

### Reset to a clean state (run once in Terminal B)

This restores the 3 records and clears any leftovers from testing:

```powershell
docker compose -f ..\docker-compose.yaml restart toy-mcp-server
```

Confirm the starting state is clean:

```powershell
curl.exe -s http://localhost:8090/relations
```
**Expected — alice owns records 1 and 2:**
```json
[{"user":"alice","record_id":"1"},{"user":"alice","record_id":"2"}]
```

You are ready. Run the steps in order.

---

## Step 1 — The agent connects through the gate and lists tools

**Say:** *"An AI agent, acting for the admin alice, connects. It can only reach
tools through AgentGate. Let's see what's available."*

**Run (Terminal B):**
```powershell
go run . -list -user alice
```

**Expected result:**
```
logged in to Keycloak as alice
tools exposed by gateway:
  - delete_record: Delete a customer record by id (DESTRUCTIVE).
  - read_record: Read a customer record by id (safe, read-only).
```

**What this means:** `-user alice` made the agent log in to the identity service
(Keycloak) and receive a signed identity token, then connect to the gateway.
The two tools it can see are the ones behind the gate.

---

## Step 2 — A normal allowed action (reader reads)

**Say:** *"bob is a reader. Reading is within his rights, so this succeeds."*

**Run:**
```powershell
go run . -tool read_record -id 1 -user bob
```

**Expected result (last lines):**
```
calling read_record(id=1)...
isError=false
result:
{
  "content": [
    {
      "type": "text",
      "text": "Customer: Acme Corp — plan: enterprise — status: active"
    }
  ]
}
```

**What this means:** `isError=false` and real data came back. In **Terminal A**
you'll see a matching line ending `on_behalf_of="bob" roles=[reader] → allow`.
AgentGate identified who it was and permitted a read.

---

## Step 3 — Blocked by role (reader tries to delete)

**Say:** *"Now bob — a reader — tries to delete. Deleting is not his job."*

**Run:**
```powershell
go run . -tool delete_record -id 1 -user bob
```

**Expected result — the meaningful line (ignore the red PowerShell wrapper below it):**
```
calling delete_record(id=1)...
call tool: policy denied delete_record for bob (roles [reader])
```

**What this means:** the delete was **rejected at the gate** — it never reached
the tool. The reason is explicit: bob's role is `reader`, and readers may not
delete. In Terminal A the decision line ends `→ deny`.

---

## Step 4 — Blocked with no identity at all

**Say:** *"What if something tries to call a tool without logging in?"*

**Run (note: no `-user`):**
```powershell
go run . -tool read_record -id 1
```

**Expected result — the meaningful line:**
```
initialize: transport error: request failed with status 401: authentication failure: no bearer token found
```

**What this means:** this was rejected even earlier — by the **gateway itself**,
before AgentGate was consulted (notice **nothing** new appears in Terminal A's
log). No valid identity token, no entry. That's a second, outer layer of defence.

---

## Step 5 — The key idea: a destructive action is *parked* for a human

**Say:** *"alice is an admin AND she owns record 2 — she's fully authorised to
delete it. Watch what happens even so."*

**Run:**
```powershell
go run . -tool delete_record -id 2 -user alice
```

**Expected result — the meaningful line (your transaction id will differ):**
```
call tool: delete_record is destructive and requires human approval; transaction 29678989-a87f-413c-9a0d-4b8d6f7b4d59 is pending
```

**What this means:** AgentGate neither denied it nor ran it. Because deleting is
**destructive**, it **parked** the request and is waiting for a human. The agent
is told the request is *pending*.

**Show the audience the dashboard (browser):** the request now appears in the
**Pending approvals** panel. Or show it in Terminal B:

```powershell
curl.exe -s http://localhost:8090/pending
```
**Expected:**
```json
[{"transaction_id":"29678989-...","agent_id":"agent-client","on_behalf_of":"alice","roles":["admin"],"tool":"delete_record","args":{"id":"2"},"state":"pending","created_at":"..."}]
```

---

## Step 6 — A human approves, and only now does it run

**Say:** *"I'm the human on call. I review the request and approve it."*

**Do this in the browser:**
1. Open **http://localhost:8090** (the approval page — this is the queue, not
   the dashboard).
2. You'll see the parked `delete_record` row with who asked and the arguments.
3. Click **Approve**.

**Expected result:** the page reloads with a banner reading:
```
approved and executed: [{"type":"text","text":"record 2 deleted"}]
```

**What this means:** the delete executed **only after** a human said yes. Before
that click, the tool was never touched.

**Prove it actually happened (Terminal B):**
```powershell
go run . -tool read_record -id 2 -user alice
```
**Expected result (last lines) — the record is genuinely gone:**
```
    {
      "type": "text",
      "text": "no record with id \"2\""
    }
  ],
  "isError": true
}
```

---

## Step 7 — Your role isn't enough; you must own the record (SpiceDB)

**Say:** *"alice is an admin — but admin doesn't mean 'delete anything'. She only
owns records 1 and 2. Watch her try to delete record 3, which she does not own."*

**Run:**
```powershell
go run . -tool delete_record -id 3 -user alice
```

**Expected result — the meaningful line:**
```
call tool: policy denied delete_record for alice (roles [admin])
```

**Show WHY (Terminal B) — the recorded reason names both checks:**
```powershell
docker exec agentgate-postgres psql -U agentgate -t -c "SELECT reason FROM audit_log WHERE method='tools/call' ORDER BY id DESC LIMIT 1;"
```
**Expected:**
```
 cedar=allow risk=destructive roles=[admin] | spicedb: alice does not own record 3
```

**What this means:** her role *did* allow the action (`cedar=allow`), but the
ownership graph refused it (`spicedb: alice does not own record 3`). **Both**
must pass. Role alone is not enough.

---

## Step 8 — The security highlight: access revoked *while a request is parked* (TOCTOU)

**Say:** *"The tricky case: a request is parked, then approved a few minutes
later — but the person's access was taken away in between. AgentGate re-checks at
the moment of approval, not just when the request was first made."*

Run these **four commands in order** in Terminal B.

**8a — park a delete alice IS allowed to make (she owns record 1):**
```powershell
go run . -tool delete_record -id 1 -user alice
```
**Expected — copy the transaction id from this line:**
```
call tool: delete_record is destructive and requires human approval; transaction <SOME-ID> is pending
```

**8b — revoke alice's ownership of record 1 while it sits parked:**
```powershell
curl.exe -s -X POST http://localhost:8090/relations/revoke -d "user=alice&record=1"
```
**Expected:**
```json
{"outcome":"revoked: alice owns record 1 = false"}
```

**8c — now approve it. Paste the id from 8a in place of `<SOME-ID>`:**
```powershell
curl.exe -s -X POST http://localhost:8090/decide -H "Accept: application/json" -d "transaction_id=<SOME-ID>&decision=approve&decided_by=demo-operator"
```
**Expected — the approval is VOIDED:**
```json
{"outcome":"approval voided: re-check denied: cedar=allow risk=destructive roles=[admin] | spicedb: alice does not own record 1"}
```

**Say:** *"The human clicked Approve — and the system still refused, because at
the moment of execution alice no longer owned the record. A stale 'yes' cannot
slip through. That is the whole point of AgentGate."*

**8d — restore ownership so the demo is clean, and prove record 1 survived:**
```powershell
curl.exe -s -X POST http://localhost:8090/relations/grant -d "user=alice&record=1"
go run . -tool read_record -id 1 -user alice
```
**Expected:** the grant confirms `= true`, and the read returns
`"Customer: Acme Corp — plan: enterprise — status: active"` — record 1 was never
deleted.

---

## Step 9 — Everything was recorded

**Say:** *"Every action we just took — allows, denials, the parked request, the
voided approval — is permanently logged."*

**Show the dashboard:** point at the **Audit log** panel on
**http://localhost:8090/dashboard**. Green rows are allows, red are denials,
amber are pending.

**Or run the query (Terminal B):**
```powershell
docker exec agentgate-postgres psql -U agentgate -c "SELECT on_behalf_of, tool, decision FROM audit_log WHERE method='tools/call' OR method LIKE 'approval/%' ORDER BY id DESC LIMIT 10;"
```

**Expected (your rows will read bottom-to-top as the demo happened):**
```
 on_behalf_of |     tool      |        decision
--------------+---------------+-------------------------
 alice        | read_record   | allow
 alice        | delete_record | deny
 alice        | delete_record | needs_approval
 alice        | delete_record | deny
 alice        | read_record   | allow
 alice        | delete_record | executed_after_approval
 alice        | delete_record | needs_approval
 bob          | delete_record | deny
 bob          | read_record   | allow
 bob          | read_record   | allow
```

**Close with:** *"So the answer to 'what did the AI agent do, and why was it
allowed?' is never a guess — it's a query."*

---

## Step 10 — Reset (do this after the demo, ready for a re-run)

```powershell
docker compose -f ..\docker-compose.yaml restart toy-mcp-server
```
This restores all 3 records. The ownership graph is already back to seed (alice
owns 1 and 2) because Step 8d restored it.

---

## Quick recovery — if anything misbehaves mid-demo

| What you see | What it means | Fix (run in Terminal B) |
|---|---|---|
| Every call fails `403 ... external authorization failed` | The gateway lost its link to AgentGate | `docker compose -f ..\docker-compose.yaml restart agentgateway` — wait 3s, retry the step |
| `read_record` says `no record with id "N"` | That record was already deleted in a run | `docker compose -f ..\docker-compose.yaml restart toy-mcp-server` |
| A delete you expected to **park** instead **denies** | alice doesn't own that record | grant it: `curl.exe -s -X POST http://localhost:8090/relations/grant -d "user=alice&record=N"` |
| A red PowerShell block after a denied/parked call | Normal — it's just the non-zero exit code | Nothing; read the `call tool:` line above it |
| The dashboard is blank / won't load | agentgate still starting | check Terminal A shows `listening on :9000`; refresh the page |

## One-slide summary (optional closing)

1. Agent logs in → receives a signed identity (**Keycloak**).
2. Agent calls a tool → only reachable **through the gateway**.
3. Gateway asks **AgentGate**: allowed? → checks **role** (Cedar) **and**
   **ownership** (SpiceDB).
4. Every decision is **logged** (Postgres) — fully auditable.
5. Destructive actions are **parked for a human**, and **re-checked at the
   moment of approval** — so revoked access is always respected.
