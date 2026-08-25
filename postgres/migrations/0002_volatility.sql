-- Что рынок брал за опцион у денег, минута за минутой. Своя история: брокер
-- отдаёт только сегодняшнее значение, а два входа из трёх сравнивают сегодня с
-- прошлым.

CREATE TABLE IF NOT EXISTS volatility_samples (
    id                 BIGSERIAL PRIMARY KEY,
    underlying         TEXT NOT NULL,
    contract           TEXT NOT NULL,
    recorded_at        TIMESTAMPTZ NOT NULL,
    expiration         DATE NOT NULL,
    strike             NUMERIC(14,6) NOT NULL,
    -- Слово брокера: call или put.
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
