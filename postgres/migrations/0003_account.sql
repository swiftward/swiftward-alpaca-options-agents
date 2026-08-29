-- What the account was worth at each moment of the week. The broker returns only
-- "now" while a judge looks at the week's result, so the curve is ours.

CREATE TABLE IF NOT EXISTS account_snapshots (
    id                    BIGSERIAL PRIMARY KEY,
    recorded_at           TIMESTAMPTZ NOT NULL UNIQUE,
    equity                NUMERIC(14,6) NOT NULL,
    -- The account's value at yesterday's close: the day's result is measured from it.
    equity_yesterday      NUMERIC(14,6) NOT NULL,
    cash                  NUMERIC(14,6) NOT NULL,
    buying_power          NUMERIC(14,6) NOT NULL,
    options_buying_power  NUMERIC(14,6) NOT NULL,
    position_market_value NUMERIC(14,6) NOT NULL
);

CREATE INDEX IF NOT EXISTS account_snapshots_recorded_at_idx
    ON account_snapshots (recorded_at DESC);
