-- Pending approvals: destructive tool calls are "parked" here instead of
-- being forwarded. A human decides (Slack button or local approval UI);
-- on approval the policy check is RE-RUN (TOCTOU safety) and only then is
-- the original call executed against the backend tool.

CREATE TABLE IF NOT EXISTS pending_approvals (
    id             BIGSERIAL PRIMARY KEY,
    -- Ties back to the audit_log row of the original attempt.
    transaction_id UUID        NOT NULL UNIQUE,

    -- Everything needed to re-check and re-play the call later.
    agent_id       TEXT        NOT NULL,
    on_behalf_of   TEXT        NOT NULL,
    roles          TEXT[]      NOT NULL DEFAULT '{}',
    tool           TEXT        NOT NULL,
    args           JSONB,

    -- pending → approved / denied → executed / failed
    state          TEXT        NOT NULL DEFAULT 'pending',
    decided_by     TEXT,
    decided_at     TIMESTAMPTZ,
    -- Result of executing the call after approval (or the reason the
    -- TOCTOU re-check refused it).
    result         TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS pending_approvals_state_idx ON pending_approvals (state);
