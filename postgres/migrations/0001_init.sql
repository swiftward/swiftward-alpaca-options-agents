-- Каждая сессия агента, что она решила и что из этого исполнилось.

CREATE TABLE IF NOT EXISTS sessions (
    id           BIGSERIAL PRIMARY KEY,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,
    cause        TEXT NOT NULL,
    playbook     TEXT NOT NULL,
    ruleset      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS decisions (
    id           BIGSERIAL PRIMARY KEY,
    session_id   BIGINT NOT NULL REFERENCES sessions(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    intent       JSONB NOT NULL,
    outcome      TEXT NOT NULL,
    refusal      JSONB
);

CREATE TABLE IF NOT EXISTS fills (
    id           BIGSERIAL PRIMARY KEY,
    decision_id  BIGINT NOT NULL REFERENCES decisions(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    broker_order TEXT NOT NULL,
    payload      JSONB NOT NULL
);
