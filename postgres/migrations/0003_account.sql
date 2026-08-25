-- Сколько стоил счёт в каждый момент недели. Брокер отдаёт только «сейчас», а
-- судья смотрит на результат за неделю, поэтому кривая наша.

CREATE TABLE IF NOT EXISTS account_snapshots (
    id                    BIGSERIAL PRIMARY KEY,
    recorded_at           TIMESTAMPTZ NOT NULL UNIQUE,
    equity                NUMERIC(14,6) NOT NULL,
    -- Стоимость счёта на вчерашнем закрытии: от неё считается результат дня.
    equity_yesterday      NUMERIC(14,6) NOT NULL,
    cash                  NUMERIC(14,6) NOT NULL,
    buying_power          NUMERIC(14,6) NOT NULL,
    options_buying_power  NUMERIC(14,6) NOT NULL,
    position_market_value NUMERIC(14,6) NOT NULL
);

CREATE INDEX IF NOT EXISTS account_snapshots_recorded_at_idx
    ON account_snapshots (recorded_at DESC);
