-- Audit log: one row per authorization decision, written BEFORE the answer
-- goes back to the gateway. This is the "permanent, reconstructible record"
-- of everything agents attempted, whether it was allowed, and why.
--
-- Mounted into Postgres's docker-entrypoint-initdb.d, so it runs
-- automatically the first time the database container starts.

CREATE TABLE IF NOT EXISTS audit_log (
    id             BIGSERIAL PRIMARY KEY,
    -- Stable id for the attempted operation; the approval flow (Phase 5)
    -- references this same id, so an approval can be traced back to the
    -- exact call that triggered it.
    transaction_id UUID        NOT NULL DEFAULT gen_random_uuid(),

    -- Who: the software agent and the human it acted on behalf of.
    agent_id       TEXT        NOT NULL,
    on_behalf_of   TEXT        NOT NULL,
    roles          TEXT[]      NOT NULL DEFAULT '{}',

    -- What: JSON-RPC method, and for tools/call the tool + its arguments.
    method         TEXT        NOT NULL,
    tool           TEXT,
    args           JSONB,

    -- Outcome: allow / deny / needs_approval (+ human-readable reason and
    -- which version of the policy text produced it).
    decision       TEXT        NOT NULL,
    reason         TEXT,
    policy_version TEXT,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_log_transaction_idx ON audit_log (transaction_id);
CREATE INDEX IF NOT EXISTS audit_log_created_idx     ON audit_log (created_at);
