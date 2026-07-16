# AgentGate — Product Brief

The AccessGraph core (ReBAC + approvals + lineage)

The authorization, human-approval, and audit layer for AI agents and the tools / MCP servers they call — the control that lets an enterprise move an agent from **pilot to production** without the risk team saying no.

## The problem (the gate)

Enterprises can build capable agents; they can't ship them. The blocker isn't capability — it's that security and risk teams have no way to answer, **and prove**, four questions: *What is this agent allowed to do? On whose behalf? Was a high-risk action approved? Where is the record?* Observability dashboards don't clear that gate. An enforceable authorization + audit boundary does. That boundary is the product.

## What it is

At the moment an agent invokes a tool / MCP server / API:

- **Identity & on-behalf-of** — which agent is acting, as which user or service.
- **Authorization** — scoped, relationship-based (ReBAC) allow / deny, per action.
- **Human-in-the-loop** — high-risk actions route to a human approver in Slack / Teams, resolved in seconds.
- **Immutable audit** — *"this agent, on behalf of this user, took this action against this resource, under this policy, at this time."*


## v1 scope

1. **MCP / tool-call proxy or lightweight SDK** that attaches identity + on-behalf-of context to each agent tool call.
2. **Per-action policy check** (Cedar / OPA): allow / deny / require-approval.
3. **Human approval flow** in Slack / Teams, resolving in seconds.
4. **Immutable action audit log + ReBAC lineage graph** — reuse the AccessGraph core.
5. **One compliance export** mapped to EU AI Act logging / human-oversight clauses.

Everything beyond this — multi-framework breadth, full observability, decision intelligence — is roadmap, not v1.

