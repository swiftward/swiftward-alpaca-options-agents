-- What the market charged for an at-the-money option, minute by minute. A
-- history of our own: the broker returns only today's value, and two entries out
-- of three compare today with the past.

CREATE TABLE IF NOT EXISTS volatility_samples (
    id                 BIGSERIAL PRIMARY KEY,
    underlying         TEXT NOT NULL,
    contract           TEXT NOT NULL,
    recorded_at        TIMESTAMPTZ NOT NULL,
    expiration         DATE NOT NULL,
    strike             NUMERIC(14,6) NOT NULL,
    -- The broker's own word: call or put.
    option_type        TEXT NOT NULL,
    implied_volatility NUMERIC(14,6) NOT NULL,
    delta              NUMERIC(14,6),
    bid                NUMERIC(14,6) NOT NULL,
    ask                NUMERIC(14,6) NOT NULL,
    underlying_price   NUMERIC(14,6) NOT NULL,
    UNIQUE (contract, recorded_at)
);

CREATE INDEX IF NOT EXISTS volatility_samples_underlying_idx
    ON volatility_samples (underlying, recorded_at DESC);
