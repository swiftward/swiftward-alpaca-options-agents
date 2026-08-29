-- What the session did with its hands: every tool call with its arguments and
-- outcome. The intent says what it meant to do; this says what it did.

CREATE TABLE IF NOT EXISTS tool_calls (
    id          BIGSERIAL PRIMARY KEY,
    -- The agent's own call id: the start is matched to the end by it.
    call_ref    TEXT NOT NULL UNIQUE,
    turn_ref    TEXT NOT NULL,
    -- The server the tool belongs to: session, broker, gateway, shell.
    server      TEXT NOT NULL,
    tool        TEXT NOT NULL,
    arguments   JSONB,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    -- The agent's own word: completed, failed, or unknown for a turn that was cut short.
    status      TEXT NOT NULL,
    failure     TEXT
);

CREATE INDEX IF NOT EXISTS tool_calls_turn_ref_idx ON tool_calls (turn_ref, started_at);
CREATE INDEX IF NOT EXISTS tool_calls_started_at_idx ON tool_calls (started_at DESC);
