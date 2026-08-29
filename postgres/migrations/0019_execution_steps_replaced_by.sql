-- A replacement gets a NEW order id from the broker, so the steps of one order
-- and the fill that ends it live under different ids. Without the link the chain
-- breaks at the first price move: 28 August, 19 of 33 fills could not be traced
-- to the call that started them.
--
-- The broker's own word for this is `replaced_by`, and it is kept.
ALTER TABLE execution_steps ADD COLUMN IF NOT EXISTS replaced_by TEXT;
