-- Which account this record is of.
--
-- One record holds one account: the two agents keep their work in two databases,
-- `agents` and `agents_near`, and nothing but an environment variable said so.
-- On 31 August two read-only pages were pointed at the first agent's database and
-- served its equity line and its intents beside a money panel reading a different
-- account, with no sign that the two disagreed.
--
-- The row is written by the process that keeps the record, and every process that
-- opens the record compares its own name against it.
CREATE TABLE IF NOT EXISTS record_account (
    one_row BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (one_row),
    account TEXT NOT NULL CHECK (account <> ''),
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
