-- How likely the sold strike is to finish in the money, as the broker read it.
--
-- The screener already computes this and filters on it, and then dropped it on
-- the floor: the column did not exist, so the session was handed a shortlist with
-- no delta and had to ask the broker again, contract by contract, for a number
-- already known. Nullable because the broker computes none on expiry day.
ALTER TABLE candidates ADD COLUMN IF NOT EXISTS short_delta NUMERIC(14,6);
