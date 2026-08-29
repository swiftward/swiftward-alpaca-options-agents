-- What the chance of surviving was read from: the broker's delta, or the price
-- of volatility where the broker computes none.
--
-- The two are the same quantity from different prices, and the whole expiry-day
-- book arrives by the second route. A session weighing a structure is entitled
-- to know which it is looking at.
ALTER TABLE candidates ADD COLUMN IF NOT EXISTS edge_from TEXT;
