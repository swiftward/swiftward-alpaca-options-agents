-- What the screener found on its last sweep.
--
-- One sweep replaces the whole table: a candidate is a price, and a price from
-- five minutes ago is not a stale row to keep beside a fresh one - it is a wrong
-- answer. The session asks for what is there now.
CREATE TABLE IF NOT EXISTS candidates (
    id                       BIGSERIAL PRIMARY KEY,
    swept_at                 TIMESTAMPTZ NOT NULL,
    underlying               TEXT        NOT NULL,
    kind                     TEXT        NOT NULL,
    expiration               DATE        NOT NULL,
    short_symbol             TEXT        NOT NULL,
    long_symbol              TEXT        NOT NULL,
    short_strike             NUMERIC(14,6) NOT NULL,
    long_strike              NUMERIC(14,6) NOT NULL,
    underlying_price         NUMERIC(14,6) NOT NULL,
    out_of_the_money_percent NUMERIC(14,6) NOT NULL,
    credit                   NUMERIC(14,6) NOT NULL,
    risk                     NUMERIC(14,6) NOT NULL,
    credit_to_risk_percent   NUMERIC(14,6) NOT NULL,
    cost                     NUMERIC(14,6) NOT NULL,
    cost_share_percent       NUMERIC(14,6) NOT NULL
);

CREATE INDEX IF NOT EXISTS candidates_rank_idx
    ON candidates (credit_to_risk_percent DESC);
