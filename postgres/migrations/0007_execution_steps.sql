-- How an order walked to its fill: every price step with what the book showed and
-- what limit the session named. Without this record the question "how much did we
-- save" can only be answered from container logs, that is, until the first
-- redeploy.

CREATE TABLE IF NOT EXISTS execution_steps (
    id         BIGSERIAL PRIMARY KEY,
    order_ref  TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL,
    -- What the ladder did: walked or cancelled.
    action     TEXT NOT NULL,
    was        NUMERIC(14,6) NOT NULL,
    became     NUMERIC(14,6),
    -- The price the book showed at that moment, and the session's limit.
    showing    NUMERIC(14,6),
    floor      NUMERIC(14,6)
);

CREATE INDEX IF NOT EXISTS execution_steps_order_idx ON execution_steps (order_ref, at);
CREATE INDEX IF NOT EXISTS execution_steps_at_idx ON execution_steps (at DESC);
