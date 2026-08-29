-- An intent belongs to a turn. Without this, "which turn wrote it" is recovered
-- by comparing timestamps and free text the model itself typed - and causality is
-- exactly what a judge asks about.

ALTER TABLE intents ADD COLUMN IF NOT EXISTS turn_ref TEXT;

CREATE INDEX IF NOT EXISTS intents_turn_ref_idx ON intents (turn_ref);
