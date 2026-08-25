-- Что сессия делала руками: каждый вызов инструмента с аргументами и исходом.
-- Намерение говорит, что она собиралась сделать; это говорит, что она сделала.

CREATE TABLE IF NOT EXISTS tool_calls (
    id          BIGSERIAL PRIMARY KEY,
    -- Идентификатор вызова у самого агента: по нему начало сходится с концом.
    call_ref    TEXT NOT NULL UNIQUE,
    turn_ref    TEXT NOT NULL,
    -- Сервер, которому принадлежит инструмент: session, broker, gateway, shell.
    server      TEXT NOT NULL,
    tool        TEXT NOT NULL,
    arguments   JSONB,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    -- Слово агента: completed, failed, а для оборванного хода - unknown.
    status      TEXT NOT NULL,
    failure     TEXT
);

CREATE INDEX IF NOT EXISTS tool_calls_turn_ref_idx ON tool_calls (turn_ref, started_at);
CREATE INDEX IF NOT EXISTS tool_calls_started_at_idx ON tool_calls (started_at DESC);
