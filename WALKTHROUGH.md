# AgentGate — Project Walkthrough (rebuilding shared understanding)

We're going to rebuild a clear picture of this project, one small piece at a
time. Each section is appended after you've confirmed the previous one makes
sense. Nothing is final until you say so.

---

## 1. The one-sentence problem

AI agents can now call real tools (delete data, send emails, move money,
run code) — but there is no layer that checks, for each individual tool
call, whether that specific action should actually be allowed, and no
record of why it was.

**Nothing about "AI" is required to state the problem** — replace "AI
agent" with "script" or "service account" and the sentence still holds.
The problem is: unattended software is calling powerful tools, and nobody
is checking each call or writing down what happened.

---

*Stop here. Does this match your understanding of what problem we're
solving? Anything to correct before we continue?*

---

## 2. Who actually has this problem

Not "AI companies." Specifically: **any organization that lets an AI agent
call tools that touch real, consequential systems** — a database, a payment
API, an email sender, an internal admin panel, a cloud account.

Concretely, the person who feels this problem is usually one of:

- A **platform/security engineer** who just got asked "we're rolling out an
  AI agent that can access our CRM — how do we make sure it can't delete
  customer records it shouldn't touch?"
- An **engineering lead** building an internal AI agent (support bot, ops
  bot, coding agent) who needs *some* answer to "what stops this from going
  rogue?" before it ships
- A **compliance/audit function** that needs to answer "what did the AI
  agent do, on whose authority, and who approved it?" after the fact

They are not asking for a smarter AI. They are asking for a **control layer**
around the AI they already have.

---

*Stop here. Does this match who you imagine using this? Or did you have a
different/narrower audience in mind?*

---

## 3. What AgentGate actually is, at the highest level

**AgentGate sits between the AI agent and the tools it calls, and forces
every single tool call to pass through it first.**

That's the entire mechanism. Everything else is detail on top of this one
fact:

```
AI agent  --wants to call a tool-->  AgentGate  --if allowed-->  real tool
```

The agent cannot skip AgentGate. It's not a suggestion or a monitor sitting
on the side — it's a **checkpoint in the only path** between the agent and
the tool.

Once you have that checkpoint, you can do three things at it:
1. Decide allow/deny for each call
2. Write down what happened (audit)
3. Change the rules over time based on what you observed

Nothing more than this is required to have a real product. Everything we
discussed before (Cedar, SpiceDB, Keycloak, dashboards) is *how* we
implement these three things — not what the product fundamentally is.

---

*Stop here — is this the right "floor" to build the rest of the explanation
on? Anything about this core idea feel off or incomplete?*

---

## 3a. Clarifying: what does the agent think it's talking to?

> **Question asked:** Does the AI agent actually believe agentgateway
> itself is the MCP tool provider — and does agentgateway, in turn, act as
> the MCP client making the real call to the actual MCP server? I.e. is
> agentgateway really just a forwarding proxy underneath, not a real
> endpoint on either side?

Your understanding is correct. Spelling it out precisely:

- To the **AI agent**, agentgateway *is* the MCP server. The agent connects
  to it, calls `tools/list`, calls `tools/call` — exactly as if it were
  talking to the real tool directly. The agent has no idea a proxy exists.

- To the **real MCP server** (toy-server), agentgateway *is* the MCP client.
  It opens the connection, forwards the call, gets the result.

- **agentgateway itself does not decide anything and does not touch tool
  logic.** It is a pass-through with one extra step bolted on: before
  forwarding, it calls out to AgentGate (`ext_authz`) and asks "should I
  forward this or not?" It forwards only if told yes.

So there are two independent MCP "connections" that agentgateway sits at
the middle of — agent↔gateway, and gateway↔real-server — and it fakes being
the other endpoint in each direction. This is the same pattern as any
reverse proxy (nginx in front of a web server): the client thinks it's
talking to the server; the server thinks it's talking to the client;
neither is directly true.

**AgentGate is not in this MCP conversation at all.** It never speaks MCP.
It only answers the gateway's separate, internal "allow or deny?" question
(a gRPC call, not an MCP call). This is why AgentGate can be swapped,
restarted, or reasoned about independently of the MCP traffic itself.

---

*Stop here — does this fully resolve the agentgateway/AgentGate distinction,
or is there still a fuzzy edge?*

---

## 3b. How agentgateway knows about the real MCP server's tools

> **Question asked:** agentgateway is a proxy and its internal workings
> aren't something we need to worry about — but it must still know what
> tools the real MCP server exposes, and must be able to actually forward
> a tool call to it. Is that allowed/possible out of the box? How does
> agentgateway support this — is it configured through some UI, where
> tools get registered and the gateway's internal logic handles the rest?

Here's what I can say with confidence, based on our actual
`gateway/config.yaml`:

```yaml
backends:
  - mcp:
      targets:
        - name: toy-server
          mcp:
            host: http://toy-mcp-server:8080/mcp
```

We declare the backend's **address** and tell the gateway "this backend
speaks MCP" (the `mcp:` key, as opposed to a generic HTTP backend). We do
**not** manually list `read_record` / `delete_record` anywhere in this
file. agentgateway is MCP-native — because it knows the backend is an MCP
server, it connects to it as an MCP client itself, does the MCP handshake,
and can call `tools/list` on that backend directly. It does not need us to
register tools by hand; the real server is the source of truth for its own
tool list, and agentgateway discovers it.

This is *config-as-code*: the file is version-controlled and hot-reloaded
(no restart needed) — we've relied on this ourselves when changing routes.

**Where my confidence drops — flagging honestly rather than guessing:**
- Whether there's an **admin UI that can edit** this config (vs. only
  *view* it — we know :15000 exists and shows loaded config, but I haven't
  confirmed if it supports live editing)
- Exact behavior when a backend has **multiple MCP targets** under one
  route (does it merge/namespace their tool lists?)
- Whether agentgateway **caches** the discovered tool list, and how/when
  it re-fetches if the real server adds a new tool later

If you want those confirmed rather than left open, here's a prompt you
could run against a web-connected assistant:

> "In agentgateway (the Rust-based MCP-native proxy, agentgateway.dev),
> when a backend is declared with `mcp: targets:` in config.yaml pointing
> at a real MCP server — does agentgateway automatically discover that
> server's tools via `tools/list`, or must tools be manually registered
> somewhere? Is there an admin UI (default port 15000) that supports
> editing config, or is it read-only? If multiple MCP targets are declared
> under one route, how are their tool lists combined for the client?"

---

*Stop here — want me to fold in the answer once you get it, or move on and
treat this as a "confirm later" item?*

---

## 3c. Confirmed answer (from research)

> **Context:** the question from 3b was researched externally (a
> web-connected assistant), since it concerned agentgateway internals we
> didn't have direct documentation for in this project. Answer below.

- **Tool discovery is fully automatic.** agentgateway does the MCP
  handshake with the real server itself and calls `tools/list` on it — we
  never register tools by hand. Confirms 3b.
- **Admin UI (:15000) editability depends on deployment mode.** In our
  setup (standalone Docker, config from a mounted YAML file), the UI is
  *interactive* — you can add MCP servers/backends through it directly. In
  Kubernetes-controller mode it would be read-only (config comes from CRDs
  instead). **Relevant for us:** since we run standalone, the UI is not
  just an inspector — it's a second way to change config, alongside
  editing the YAML file. Worth remembering when we reason about "who can
  change gateway config" for security later.
- **Multiplexing:** if we ever put multiple real MCP servers behind one
  route, agentgateway merges all their tools into one list for the agent,
  auto-prefixing names by target (`toy-server_delete_record`) to avoid
  collisions. Not relevant with our single toy-server today, but relevant
  the moment we front more than one real tool provider.
- **New concept worth flagging for later:** "Progressive Disclosure" —
  agentgateway can collapse a huge tool list into two meta-tools
  (`get_tool`/`invoke_tool`) to save the agent's context tokens. Not a
  security feature, just noting it exists since it changes what the agent
  actually sees in `tools/list`.

---

*Stop here — anything in this confirmed answer change how you think about
the system? Or shall we move to the next piece?*

---

## 3d. Confirming your restatement

> **Question asked:** to confirm plainly — we don't need to worry about
> manually wiring up tools; agentgateway is capable of making the standard
> MCP calls to any compliant MCP server on its own. Is that correct?

Yes, correct. To be precise about the scope of that "yes":

Any MCP server that follows the standard protocol — `initialize`,
`tools/list`, `tools/call` (and `resources/*`, `prompts/*` if it has them)
— just works behind agentgateway with only an address in `config.yaml`.
agentgateway speaks standard MCP to it and does the rest.

The only thing that stays **our job**, always: telling *AgentGate* which
tools are risky and what "ownership" means for them (the Cedar rules, the
risk classification, the SpiceDB schema). agentgateway will happily
discover and forward a call to `delete_everything` with zero resistance —
protocol compliance is not the same as safety. Discovery is automatic;
governance is not, and never will be, because governance requires
knowing *our* rules, which no protocol can infer on its own.

---

*Stop here — ready for the next piece: what actually differs in AgentGate
per tool/deployment, i.e. why "just point it at any MCP server" isn't the
whole story.*

---

## 3e. Who decides which tools are risky?

> **Question asked:** for AgentGate's policy to know which tools are risky
> versus safe, the policy needs to be told this somehow — so should we
> build a way for a user/operator to define it? Or should AgentGate just
> auto-detect the available tools (via a `tools/list`-style call) and let
> the operator decide what to do with each one from there?

You've landed on it exactly. It's the second one:

1. **AgentGate can auto-detect what tools exist** — same mechanism as
   agentgateway: call `tools/list` on the real server, get the names and
   descriptions back. This part is free, automatic, no human needed.
2. **AgentGate cannot auto-detect which of those tools are dangerous.**
   Nothing in the MCP protocol tells you "this one deletes data" vs "this
   one just reads it." (Our POC currently *guesses* from the tool name —
   `delete_*` — which we already flagged in an earlier doc as fragile and
   wrong for tools like `archive_customer`.)

So the real shape of the feature is: **auto-detect the list, then hand it
to a human to classify**, not auto-detect-and-decide.

Concretely, this becomes an operator-facing screen:

```
Tool discovered       Risk?              Requires ownership check?
─────────────────     ──────             ───────────────────────
read_record            [safe ▾]                  no
delete_record          [destructive ▾]           yes → maps to "record" + "id" arg
send_email             [destructive ▾]           no (no clear owner concept)
archive_customer       [ needs review ]           ?
```

When a **new** tool shows up later (the real server added one), it should
land in an "unclassified — needs review" state and be **denied by default**
until an operator classifies it — not silently allowed. This is exactly
the "tool inventory drift" gap we noted earlier; this screen is the fix.

So to directly answer your question: yes, we must provide the operator a
way to do this — it can't be skipped or inferred. The auto-detection just
removes the *typing* work (operator doesn't invent tool names), it doesn't
remove the *judgment* work (operator still decides risk).

---

*Stop here — does this match what you meant, or did you mean something
narrower, like just "do we need a UI for it" (yes)?*

---

## 4. Everything that must be configured per deployment

> **Framing:** we've established agentgateway auto-wires to any MCP
> server for free, but governance is never free. This section lists every
> thing a new customer/deployment must actually configure before AgentGate
> can protect their specific tools — the setup checklist, in effect.

Beyond risk classification (§3e), three more things are deployment-specific
and cannot be auto-detected:

1. **What does "ownership" mean here?**
   For our toy demo it's "a record has one owner." A real deployment might
   have no ownership concept at all (a Slack-posting tool), or a totally
   different shape (a document shared with a *team*, not a person). This
   has to be defined per tool, per deployment — it's business logic, not
   something MCP or AgentGate can infer.

2. **Which argument is "the resource" for a given tool?**
   To check "does alice own record 2," AgentGate must know that
   `delete_record`'s `id` argument *is* the record being acted on. For
   `send_email(to, body)`, there's no ownership check, but `to` is the
   dangerous argument worth looking at. This mapping (tool → which
   argument matters) is manual, per tool.

3. **Where do roles/identity come from?**
   Our POC uses Keycloak with two hardcoded users. A real deployment
   already has its own identity system (Okta, Azure AD, a custom auth
   service) with its own idea of roles. Someone has to connect that source
   to AgentGate — either by pointing at their existing OIDC provider, or by
   AgentGate accepting an already-issued token and reading claims from it.

**The pattern across all of §3e and this section:** AgentGate can discover
*facts* about the tools automatically (names, argument schemas). It can
never discover *intent* automatically (what's risky, what "owning" means,
who's allowed). Every deployment requires a human to state intent once, up
front — after that, enforcement is automatic forever.

---

*Stop here — does this list feel complete for "day-one setup," or is there
a category of configuration you think is missing?*

---

## 5. Where does authentication actually happen, and how does AgentGate get roles?

> **Question asked:** it's unclear how the authentication engine attaches
> to the system. Is the request already authenticated by the time it
> reaches AgentGate (pre-verified, AgentGate just trusts it), or does
> AgentGate itself do the verification? What did "point at their existing
> OIDC provider" actually mean, and how does that solve anything? Where
> and how does AgentGate extract the user's identity and roles from?

**Short answer: the request arrives pre-authenticated. AgentGate never
verifies a signature itself — that already happened one hop earlier, at
agentgateway.** AgentGate only *reads* identity that's already been proven.

Walking the actual request, in order, using our real config:

**Step 1 — the agent gets a token** (before any tool call happens)
The agent logs into whatever identity provider issues tokens for this
deployment (Keycloak in our POC) using OAuth2/OIDC — username+password, or
however that provider allows login. It gets back a **JWT**: a signed piece
of text containing claims like "this user is `alice`, her roles are
`[admin]`." The signature is cryptographic proof the token wasn't forged —
made using a private key only the identity provider holds.

**Step 2 — agentgateway verifies the signature** (`gateway/config.yaml`,
the `jwtAuth` block we already have):
```yaml
jwtAuth:
  mode: strict
  issuer: http://localhost:8081/realms/agentgate
  jwks:
    url: http://keycloak:8080/realms/agentgate/protocol/openid-connect/certs
```
agentgateway fetches Keycloak's **public** keys (the `jwks` URL — safe to
expose, only used to *verify*, not create, signatures) and checks the
token's signature against them. This is where "verification" actually
happens — once, at the gateway, before AgentGate is ever called. If the
signature doesn't check out, or the token is expired, the request is
rejected right here — AgentGate is never even asked.

**Step 3 — the already-validated claims are handed to AgentGate**
agentgateway doesn't just verify and discard the token — it forwards the
decoded claims to AgentGate as part of the `ext_authz` check request (a
field called `metadata_context`). Our `identity` package reads them
directly:
```go
// agentgate/internal/identity/identity.go (as it exists today)
// tries metadata_context first (gateway already validated it)
// falls back to decoding the raw JWT body without re-checking the
// signature — safe, because step 2 already proved it's genuine
```
This is the answer to "pre-authenticated, and AgentGate just accepts it":
**yes, exactly that.** AgentGate deliberately does *not* re-verify the
signature — it would be redundant work, and it trusts the gateway because
the gateway is the only path a request can take to reach it (§3, the
checkpoint idea).

**Step 4 — role extraction**
The claims inside the JWT are just JSON. Keycloak was configured (in our
realm JSON file) to include a `roles` claim and an `on_behalf_of` claim
when it issues a token. AgentGate's identity code simply reads those two
fields out of the JSON. There's no separate "roles lookup" — the roles
travel *inside* the token itself, put there by the identity provider at
login time.

**Now, what "point at an existing OIDC provider" actually meant:**
Keycloak is not special — it's *one* implementation of the OIDC standard.
Any real company already runs an identity provider (Okta, Azure AD,
Google Workspace, Auth0). All of them can issue a JWT and all of them
publish a JWKS URL the same way Keycloak does. So for a real customer,
"attaching authentication" means: **change three lines in
`gateway/config.yaml`** — the `issuer` and `jwks.url` to point at *their*
Okta/Azure AD instead of our Keycloak. Nothing else changes. AgentGate's
code doesn't change at all, because it never talked to Keycloak
directly — it only ever read claims agentgateway already validated.

**The one real catch, worth being honest about:** the *names* of the
claims (`roles`, `on_behalf_of`) are Keycloak-specific, configured by us
in the realm JSON. A different provider will name things differently
(Azure AD might call it `roles` too, or nest it differently). So
"pointing at a new provider" is two changes, not one:
1. Gateway config — new issuer/JWKS URL (trivial)
2. A small mapping in AgentGate's identity code — "in this deployment,
   roles live at this JSON path" (a few lines, but not zero — this is a
   per-deployment thing, same category as §4)

---

*Stop here — does this resolve where authentication happens and how roles
are read? Any part of steps 1–4 still unclear?*

---

## 5a. What "a small mapping in AgentGate's identity code" actually means

> **Question asked:** clarify that specific phrase — how would this
> mapping actually be implemented, and what does the flow look like from
> the operator's side when they onboard a new identity provider?

Good thing to press on — as I wrote it, it sounded like "edit Go code per
customer," which would be a bad design. Correcting that:

**What exists today (the POC, hardcoded):**
```go
// identity.go, roughly:
roles := claims["roles"]           // hardcoded JSON key name
onBehalfOf := claims["on_behalf_of"] // hardcoded JSON key name
```
This only works because *we* configured Keycloak (in our realm JSON) to
emit claims with exactly these key names. It's not that AgentGate
"understands roles" — it's that we made our test data match what the code
expects. That's backwards for a real product.

**What it should be instead — data, not code:**
A small **config file per deployment**, sitting next to `gateway/config.yaml`,
that just says where things live in *that* customer's tokens:
```yaml
# claims-mapping.yaml — one per deployment
roles_claim: "realm_access.roles"    # Keycloak's actual nested path
on_behalf_of_claim: "preferred_username"
agent_id_claim: "azp"
```
AgentGate's identity code becomes generic: "read whatever field this
config points at" instead of "read the field literally named `roles`."
Onboarding a new identity provider (Okta, say) means writing a *different*
version of this small file — no Go code changes, no rebuild, no redeploy
of AgentGate itself. Same pattern as `policies/agentgate.cedar`: a mounted
file the engine reads, not something baked into the binary.

**The flow at the operator's side, concretely:**
1. Operator already has their company's identity provider running
   (Okta/Azure AD/etc.) — this is not something we ask them to set up, it
   already exists.
2. Operator logs into that provider's admin console once, gets a **test
   token** for any user (every OIDC provider has a way to do this — a
   "try it" button, or a simple curl against the token endpoint).
3. Operator (or us, during onboarding) looks at that token's decoded JSON
   and answers three questions: *where's the role, where's the
   human-identifier, where's the agent/client-identifier?*
4. Those three answers become the three lines in `claims-mapping.yaml`.
5. Point `gateway/config.yaml`'s `issuer`/`jwks.url` at their provider
   (§5, step 2).
6. Restart AgentGate to load the new mapping file. Done — no code touched.

**This is genuinely a one-time, few-minutes setup step per deployment**,
not an engineering task repeated per customer. It's the same shape as
answering "which argument is the resource" in §4 — a short, human decision
captured once as config, then automatic forever after.

**Not yet built** — today AgentGate really does hardcode the claim names.
This mapping-file mechanism is a near-term to-do, not something that
exists in the codebase right now. Flagging that clearly so it isn't
mistaken for already-shipped behavior.

---

*Stop here — is this concrete enough, or do you want to see what the
actual Go code change to support this would look like?*

---

## 6. Where does the ownership data actually come from?

> **Framing:** identity/roles are settled (§5, §5a). The other half of
> authorization — "does this specific user own this specific record?" —
> has a data-source problem we flagged earlier but never resolved. This is
> arguably the single biggest gap in the whole project, worth walking
> through carefully.

In our POC, ownership is **seeded by us, by hand**, at AgentGate startup:
```go
// relations.go — seeds "alice owns records 1, 2" on connect
```
That's fine for a demo with 3 fake records. It is not fine for a real
deployment. A real company's "who owns what" already lives somewhere —
their CRM knows which sales rep owns which account; their file storage
knows who owns which document; their ticketing system knows who owns which
ticket. **That data already exists. It is not something AgentGate creates —
AgentGate needs to be told it, and kept in sync as it changes.**

This is a genuine open problem. Three realistic ways to solve it, each with
a real tradeoff:

**Option A — one-time import + manual updates**
Operator runs a script once: read ownership from their database, write it
into SpiceDB as edges. Simple to build. Goes stale immediately — if
ownership changes in their real system tomorrow (a record gets
reassigned), AgentGate doesn't know until someone re-runs the import.
Fine only for very static ownership (rarely reassigned).

**Option B — webhook/event sync**
The customer's system sends AgentGate an event whenever ownership changes
("record 47 reassigned from bob to alice"), and AgentGate updates SpiceDB
in real time. Accurate, but requires the customer's system to *support*
firing such an event — real integration work on their side, not something
we can guarantee exists.

**Option C — ask the source system live, don't store ownership at all**
Instead of copying ownership into SpiceDB, AgentGate calls the customer's
own API at decision time: "does alice own record 2?" — asked directly to
their system of record. No sync problem (there's nothing to go stale),
but adds a network call to *every* decision, and requires the customer to
expose such a check as an API — which most systems don't, today.

**Where this leaves us, honestly:** none of these is "solved" yet — this
is a real open design question, not a settled one like §5's claims
mapping. My instinct, not yet validated: start with **Option A** (simplest,
ships something), be explicit to early customers that ownership is a
point-in-time snapshot, and treat **Option B** as the real production
answer once we know which customer systems we're integrating with (the
mechanism differs a lot between "ownership lives in Salesforce" vs.
"ownership lives in our own Postgres table").

This connects directly back to something from `ARCHITECTURE_DECISIONS.md`
(§5 there): we said policy should evolve from what's *observed*. The same
idea might apply here — if AgentGate logs every ownership check it makes
and every time it guessed wrong (denied someone who actually owns the
thing), that log becomes the signal for which sync approach a given
deployment actually needs.

---

*Stop here — does calling this "unsolved, here are the options" match your
own sense of it, or did you already have a preferred direction in mind?*

---

## 6a. A reframe: maybe AgentGate shouldn't track resources at all

> **Idea proposed:** none of the three options in §6 feel reasonable — all
> three assume AgentGate should know and track "who owns what," which
> might be a database-shaped assumption that doesn't generalize. Instead:
> AgentGate only tracks *tool calls* and *who's calling*, never the
> resource itself. Whether the specific data/id sent in a call actually
> belongs to that caller becomes the responsibility of the **MCP tool
> (the backend)**, not AgentGate. AgentGate knows who the call is on
> behalf of (from the token) and forwards that identity — possibly the
> token itself — to the tool. The tool checks the token, identity, role,
> and does its own permission check against the actual record. Does this
> hold up, and does it generalize past database-style tools?

**This is a real architectural correction, and a good one.** It's not a
minor tweak — it removes an entire responsibility from AgentGate and
relocates it to where the information actually already lives.

**Why it's sound:** the tool/backend is *always* the system of record for
its own data. It already knows, natively, who owns what — that's just its
normal data model. Building a second, shadow copy of that knowledge inside
AgentGate (which is what SpiceDB was doing in the POC) means keeping two
sources of truth in sync — which is exactly the unsolved sync problem in
§6. Your reframe deletes the problem by deleting the duplicate copy.

**This also has a name in the identity world:** it's the same shape as
OAuth **"on-behalf-of" token exchange** — a pattern used specifically so
that a downstream service enforces access control using the *original
caller's* identity, rather than trusting a middle layer's say-so. Our
`on_behalf_of` claim already existing in the JWT is a sign this direction
fits naturally with what we'd already built, not something bolted on.

**What AgentGate's job becomes, under this model:**
- Still decides **category-level** access (Cedar: "may a reader call
  destructive tools at all?") — this doesn't change
- Still logs every call, and now also logs **what the tool decided**, if
  the tool reports it back (e.g., "denied: not the owner") — audit gets
  *richer*, not poorer
- **Forwards identity** to the tool (the validated claims, or the token
  itself) instead of forwarding nothing
- **Drops the SpiceDB ownership layer as a required component** — it
  becomes optional, only useful for tools too simple to do their own check

**Does it generalize beyond database-shaped tools?** Mostly yes, with one
real condition: **it only works if the tool is actually capable of doing
its own per-record check.** That splits real-world tools into two kinds:

| Kind of tool | Can it self-check ownership? | What AgentGate does |
|---|---|---|
| A real company system (their CRM, their DB, their document store) — already has row/record-level permissions tied to user identity | **Yes, almost always** — this is what those systems are built to do | Forward identity, trust their check, log the outcome |
| A "dumb" tool with no built-in per-record logic (exactly like our `toy-server` — takes any `id`, does no check) | **No** | Nothing enforces it — a real gap, not a solved one |

**So the honest correction to §6 is:** the ownership-tracking problem
doesn't disappear — it **only relocates for tools sophisticated enough to
carry it themselves.** For a "dumb" tool (and plenty of real internal
tools genuinely are this simple — a thin wrapper someone wrote in an
afternoon), *something* still has to enforce per-record access, and if the
tool won't, AgentGate's own ownership layer (SpiceDB) is the fallback, not
a mistake. Our toy-server is deliberately dumb, which is why the POC
needed SpiceDB to demonstrate ownership at all.

**One security nuance worth flagging, not yet decided:** "forward the
token" needs care. Passing the agent's *raw* token straight through to
every tool means every tool receives a credential broader than the one
action it needs (wrong audience, wrong scope, replayable elsewhere). The
standard fix is **token exchange** (RFC 8693) — AgentGate (or the
gateway) trades the incoming token for a narrower, short-lived token
scoped specifically to *this tool, this call* before forwarding. Not
built, but worth naming now so "forward the token" doesn't quietly become
"forward a token that's too powerful."

**Net effect on the product:** this is a meaningfully better design. It
makes AgentGate lighter (one less subsystem to keep in sync), pushes
authorization to the place that already has perfect information, and
turns "ownership tracking" from a mandatory built-in feature into an
optional fallback for unsophisticated tools only.

---

*Stop here — does this land correctly, or is the "dumb tool fallback"
carve-out too generous / not generous enough?*

---

## 6b. Noted: the "dumb tool" fallback is a smaller concern than it looked

> **Point raised:** a genuinely dumb tool — no per-record logic at all —
> is unlikely to be given access to anything valuable in the first place.
> Whoever builds/deploys a tool that touches real, consequential data
> would naturally build in at least basic ownership checks; the dumb,
> no-checks tools tend to be the low-stakes ones by nature.

Fair, and it weakens the fallback case in §6a further — the SpiceDB-style
"AgentGate tracks ownership itself" layer shrinks from "needed as a
general fallback" to "needed only for the narrow, low-stakes edge case,"
reinforcing that it should be optional, not core to the product.

---

## 7. What AgentGate actually decides now, after the §6a reframe

> **Framing:** with resource-level ownership pushed down to the tool
> itself, it's worth re-stating plainly what's actually left in
> AgentGate's own decision-making, so nothing from earlier sections is
> mistakenly assumed to still be there.

AgentGate's job narrows to exactly two questions, asked on every call:

1. **Category question (Cedar):** "does this role get to call this kind
   of tool at all?" — e.g. readers may call read-only tools; admins may
   call anything. This is unchanged from the very first version of the
   project.
2. **Should this be allowed to happen automatically, or logged and left
   for a human to review later?** — this is the "detective governance"
   direction the team already chose (documented in
   `ARCHITECTURE_DECISIONS.md`, §5 there): rather than holding a
   destructive call and waiting for real-time approval (which we
   established has no clean solution across different agent harnesses),
   the call goes through immediately under current policy, gets logged
   with full detail, and a human tightens or loosens the policy afterward
   based on what they observe.

**What AgentGate does *not* do anymore, after §6a:** it does not maintain
its own copy of "who owns record 2." That question is answered by the tool
itself, using the identity AgentGate forwarded to it.

---

*Stop here — does this "two questions only" framing match your sense of
what's left, or does it feel like something's missing from AgentGate's
job?*

---

## 8. How the "log now, adjust policy later" loop actually works day-to-day

> **Framing:** §7 named this as one of AgentGate's two remaining jobs, but
> only in one sentence. Spelling out the actual mechanics, since this is
> now the core of how the product behaves in practice.

**Every single call, allowed or not, produces one audit row.** Not just
denials — allows too. Each row records: who (agent + human + roles), what
tool, what arguments, what was decided, and *which exact policy* made that
decision (a hash of the policy text at that moment). Nothing is invisible.

**A human — an operator — looks at this log, not in real time per call,
but periodically**, the way someone reviews firewall logs or a spam
filter's decisions:
- "This agent got denied 40 times trying to call `read_schedule` — but
  that's actually harmless, we should allow it." → operator loosens the
  rule.
- "This agent was *allowed* to call `send_email` — but on review, that
  should require a stricter role." → operator tightens the rule.

**The policy itself is just a text file today** (`policies/agentgate.cedar`)
— editing it and restarting AgentGate is how a rule actually changes. This
works, but it's not yet a friendly *operator* experience (a person doing
this shouldn't need to hand-write Cedar syntax) — that's a known gap, not
solved yet.

**One safety idea for this loop, not yet built:** before a policy change
takes effect, replay it against, say, the last 500 real logged calls and
show the operator: *"if this change had been live, these 9 previously-
denied calls would now be allowed, and these 3 previously-allowed calls
would now be denied."* That turns "edit a rule and hope" into "see the
exact effect before it ships" — cheap to build because we already log full
call context on every row, and worth calling out as a standout feature,
not just a nice-to-have.

**What this loop explicitly is *not*:** it is not a per-call approval
queue. Nobody is clicking "approve" on an individual `delete_record(id=2)`
in real time (that's the held-connection idea we already established has
no clean universal solution). This loop operates on the *rule*, not the
*instance* — which is exactly what makes it work without needing the agent
or its harness to wait for anything.

---

*Stop here — is the distinction between "adjusting the rule" vs "approving
one specific call" fully settled now, or worth revisiting once more?*
