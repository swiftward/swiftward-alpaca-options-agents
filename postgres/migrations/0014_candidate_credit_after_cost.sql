-- The credit with half the book crossing taken out: what an order sent at the
-- midpoint is worth in expectation rather than what the screen displays.
--
-- Every measure the session ranks on is computed from this. Before it, a
-- structure quoted sixteen cents wide and paying 0.22 measured seven points
-- better than one quoted two cents wide and paying 0.15, and the second is the
-- one that earns.
ALTER TABLE candidates ADD COLUMN IF NOT EXISTS credit_after_cost NUMERIC(14,6);
