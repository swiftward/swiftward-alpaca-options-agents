-- What a session did and why. Read by the demo page and by a judge.
--
-- Three tables, each answering its own question: when and why the agent worked,
-- what it meant to do before sending an order, and where it was stopped.

CREATE TABLE IF NOT EXISTS turns (
    id          BIGSERIAL PRIMARY KEY,
    -- The agent's own turn id: the turn is found by it in the agent's journal.
    turn_ref    TEXT NOT NULL UNIQUE,
    thread_ref  TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    -- Who woke it: a session name from the declaration, a person in the chat, or a wake-up.
    woken_by    TEXT NOT NULL,
    cause       TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    failure     TEXT
);

CREATE INDEX IF NOT EXISTS turns_started_at_idx ON turns (started_at DESC);

-- The intent is written BEFORE the order. A judge can see fills anywhere; only
-- this record says what the session meant to do and what loss it accepted.
CREATE TABLE IF NOT EXISTS intents (
    id         BIGSERIAL PRIMARY KEY,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    session    TEXT NOT NULL,
    thesis     TEXT NOT NULL,
    structure  TEXT NOT NULL,
    max_loss   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS intents_recorded_at_idx ON intents (recorded_at DESC);

-- A refusal names the limit that stopped an order. This is what the gateway
-- exists for, and what is shown on the demo page.
CREATE TABLE IF NOT EXISTS refusals (
    id         BIGSERIAL PRIMARY KEY,
    refused_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    boundary   TEXT NOT NULL,
    detail     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS refusals_refused_at_idx ON refusals (refused_at DESC);
