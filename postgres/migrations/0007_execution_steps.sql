-- Как заявка шла к исполнению: каждый шаг цены с тем, что показывал стакан и
-- какую границу назвала сессия. Без этой записи вопрос «сколько мы сэкономили»
-- отвечается только по логам контейнера, то есть до первого передеплоя.

CREATE TABLE IF NOT EXISTS execution_steps (
    id         BIGSERIAL PRIMARY KEY,
    order_ref  TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL,
    -- Слово о том, что сделала лестница: walked или cancelled.
    action     TEXT NOT NULL,
    was        NUMERIC(14,6) NOT NULL,
    became     NUMERIC(14,6),
    -- Цена, которую показывал стакан в этот момент, и граница сессии.
    showing    NUMERIC(14,6),
    floor      NUMERIC(14,6)
);

CREATE INDEX IF NOT EXISTS execution_steps_order_idx ON execution_steps (order_ref, at);
CREATE INDEX IF NOT EXISTS execution_steps_at_idx ON execution_steps (at DESC);
