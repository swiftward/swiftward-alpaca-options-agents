-- Что делала сессия и почему. Читается страницей демо и судьёй.
--
-- Три таблицы, и каждая отвечает на свой вопрос: когда и почему агент работал,
-- что он собирался сделать до отправки заявки, и где его остановили.

CREATE TABLE IF NOT EXISTS turns (
    id          BIGSERIAL PRIMARY KEY,
    -- Идентификатор хода у самого агента: по нему ход находится в его журнале.
    turn_ref    TEXT NOT NULL UNIQUE,
    thread_ref  TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    -- Кто разбудил: имя сессии из декларации, человек в чате или пробуждение.
    woken_by    TEXT NOT NULL,
    cause       TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    failure     TEXT
);

CREATE INDEX IF NOT EXISTS turns_started_at_idx ON turns (started_at DESC);

-- Намерение записывается ДО заявки. Судья видит исполнения где угодно; только
-- эта запись говорит, что сессия собиралась сделать и какой убыток принимала.
CREATE TABLE IF NOT EXISTS intents (
    id         BIGSERIAL PRIMARY KEY,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    session    TEXT NOT NULL,
    thesis     TEXT NOT NULL,
    structure  TEXT NOT NULL,
    max_loss   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS intents_recorded_at_idx ON intents (recorded_at DESC);

-- Отказ называет границу, которая остановила заявку. Это то, ради чего
-- существует шлюз, и то, что показывается на странице демо.
CREATE TABLE IF NOT EXISTS refusals (
    id         BIGSERIAL PRIMARY KEY,
    refused_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    boundary   TEXT NOT NULL,
    detail     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS refusals_refused_at_idx ON refusals (refused_at DESC);
