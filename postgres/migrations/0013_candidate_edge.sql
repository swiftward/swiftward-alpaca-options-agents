-- What a structure pays above what it must survive, in percentage points.
--
-- Neither half decides alone: a delta ceiling keeps what is far and throws away
-- what pays, a credit threshold keeps what pays and ignores how often it loses.
-- On 26 August a delta ceiling of 0.25 rejected three structures that were each
-- paying more than the same market said their risk was worth.
ALTER TABLE candidates ADD COLUMN IF NOT EXISTS edge_points NUMERIC(14,6);
