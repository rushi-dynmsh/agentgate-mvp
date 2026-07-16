# AgentGate — Verification Log

Purpose: this is a living document. Every time an AI-generated claim in the AgentGate docs needs checking, it gets its own entry here — one claim, one verdict, sourced. The goal is to separate "directionally reasonable, independently confirmed" from "plausible-sounding but unverified" before any of it drives engineering decisions.

Verdict key: ✅ Confirmed · ⚠️ Confirmed with caveat/nuance · ❓ Could not verify · ❌ Contradicted by sources

---

## Cluster: `agentgateway` (Linux Foundation MCP/A2A proxy)

### 1. Does agentgateway exist as a real, functioning open-source MCP/A2A proxy?
**✅ Confirmed.**
Agentgateway is a real, actively developed open-source project created by Solo.io. It's described as a proxy built on AI-native protocols (MCP and A2A) providing security, observability, and governance for agent-to-LLM, agent-to-tool, and agent-to-agent traffic, with drop-in MCP support (tool federation, stdio/HTTP/SSE/Streamable HTTP transports, OAuth). It has a GitHub repo, a docs site, active releases (2.2, 2.3 as of mid-2026), and an enterprise-hardened version sold by Solo.io on top of the open-source core.

Correction to note: it is **not** built in Go/Node.js (which is what your *old* draft, `AgentGate_Project_Explore.md`, proposed for a custom proxy). Solo.io built agentgateway from the ground up in **Rust**, specifically because they concluded Envoy would need substantial rearchitecting to support MCP/A2A cleanly.

Sources: agentgateway.dev, github.com/solo-io/agentgateway-new-ui, docs.solo.io/agentgateway

---

### 2. Is agentgateway governed by the Linux Foundation's "Agentic AI Foundation," the same body that governs MCP?
**⚠️ Confirmed, with a timing nuance.**
Two distinct events:
- **Aug 25, 2025:** Solo.io donated agentgateway to the **Linux Foundation directly**, at Open Source Summit Europe — before the AAIF existed.
- **Dec 9, 2025:** Anthropic donated **MCP** specifically to the newly-formed **Agentic AI Foundation (AAIF)** — a directed fund under the Linux Foundation, co-founded by Anthropic, Block, and OpenAI, with Google, Microsoft, AWS, Cloudflare, and Bloomberg as platinum members.

agentgateway is now published and treated as an AAIF project — its own usage write-ups are hosted directly on aaif.io, and independent trackers of the foundation explicitly list agentgateway alongside MCP, goose, and AGENTS.md as one of AAIF's hosted projects. So "same governing body as MCP" is accurate as of mid-2026, it just joined AAIF slightly after MCP did, and via a different initial path (general LF → AAIF, vs. donated straight to AAIF).

Sources: linuxfoundation.org press release (Dec 9, 2025), anthropic.com/news, aaif.io/blog, agentgateway.dev/blog

---

### 3. Are the specific named corporate backers accurate — Microsoft, AWS, Cisco, IBM, Red Hat, Akamai, T-Mobile, Dell, CoreWeave?
**✅ Confirmed, 9 for 9.**
Checked against the primary Linux Foundation contribution announcement directly (not a secondary summary):
- AWS, Microsoft, Red Hat, IBM, Cisco — named among organizations contributing to agentgateway's community meetings.
- Dell, CoreWeave, Akamai, T-Mobile — each has a named executive quoted directly in the announcement (John Roese/Dell, Chen Goldberg/CoreWeave, Jon Alexander/Akamai, Rob Hansen/T-Mobile).

Bonus finding: the real backer list is even longer than the doc claimed — Zayo, Shell, Huawei, and UBS also show up as contributors/adopters. This is one of the more precisely-sourced claims checked so far.

Source: agentgateway.dev/blog/2025-08-25-solo-contributes-agentgateway-to-lf (primary announcement)

---

### 4. Does agentgateway actually support handing allow/deny/hold decisions to an external service (the pattern AgentGate's architecture depends on)?
**✅ Confirmed — and more concretely than the doc claimed.**
Agentgateway has a documented `extAuthz` configuration that is explicitly **API-compatible with Envoy's standard External Authorization gRPC service** — the same pattern used by OPA and similar policy engines across the industry. It supports both HTTP and gRPC modes, per-backend or per-route attachment, TLS/auth to the authz service, and response caching. A real third-party integration (Cerbos, via their "Synapse" product) uses exactly this mechanism to make per-MCP-tool-call authorization decisions against agentgateway today, in production-style demos — not just generic HTTP.

**Open gap this surfaces, not fully addressed by either doc:** `extAuthz` is a **binary allow/deny** contract. There's no native "pause and wait for a human" third state in the protocol. AgentGate's "needs-approval" branch would have to be implemented as your governance service returning a deny (or a custom signal) through that channel, then separately managing the pause/Slack-notify/resume/re-forward cycle out-of-band — the gateway itself has no concept of this. Worth treating as a confirmed hard constraint on the integration design, not just an assumption.

**Related, not directly confirmed:** the earlier draft's uncertainty about integrating via Envoy's **ExtProc** mechanism specifically was appropriately hedged as unconfirmed — and it's a moot point anyway, because `extAuthz` (a different, more directly-relevant Envoy extension point) is the one that's actually documented and used for authorization decisions.

Sources: agentgateway.dev/docs (External authorization), cerbos.dev blog (Cerbos + agentgateway integration), kgateway.dev docs (BYO ext auth)

---

## Not yet checked (candidates for the next pass, one at a time as requested)

- MCP's native **Tasks extension** and **elicitation** mechanisms — real, but how mature/stable as of mid-2026?
- EU AI Act **Digital Omnibus** deadline claims (Annex III → Dec 2, 2027; Annex I → Aug 2028; Article 50 → Dec 2, 2026)
- Cedar vs. OPA/Rego performance benchmark numbers cited in the tech doc
- SpiceDB / Zanzibar claims
- The *old* draft's invented-sounding specifics (HTTP 428 retry flow, Redis TTL numbers, the SHA-256 audit hash formula, a specific NSA guidance doc, a specific protocol SEP number) — these read as fabricated implementation detail rather than checkable facts, but worth confirming that assessment explicitly rather than assuming it.

