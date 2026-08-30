-- A turn is woken once and then told more things while it runs. Until now the
-- record kept only the first: turns.woken_by, copied onto every intent as
-- intents.session. A steer changed what the turn was doing and changed neither,
-- so an intent recorded because the defence window cut into an entry turn was
-- filed under entry - confidently, and wrongly.
--
-- The causes put in front of a turn are now their own rows, in order. The row id
-- IS the order: two causes can share a timestamp, because the schedule ticks once
-- a minute and windows of ten and fifteen minutes meet on the hour.

CREATE TABLE IF NOT EXISTS turn_causes (
    id       BIGSERIAL PRIMARY KEY,
    turn_ref TEXT NOT NULL REFERENCES turns (turn_ref) ON DELETE CASCADE,
    at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The session name, a person in the chat, or a wake-up - the same vocabulary
    -- turns.woken_by used, which this replaces.
    woken_by TEXT NOT NULL,
    cause    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS turn_causes_turn_idx ON turn_causes (turn_ref, id);

-- Every turn already recorded becomes its own first cause. Nothing is lost and
-- nothing is invented: a turn that was never steered had exactly one cause.
--
-- Guarded on the old column rather than written plainly, because the migrations
-- are applied again on every start and this file drops the column it reads. Left
-- unguarded it succeeds once and then fails every start after it, which is a
-- stack that will not come up.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
         WHERE table_name = 'turns' AND column_name = 'woken_by'
    ) THEN
        INSERT INTO turn_causes (turn_ref, at, woken_by, cause)
        SELECT turn_ref, started_at, woken_by, cause FROM turns;
    END IF;
END $$;

-- Which cause was in force when the intent was written. Resolved when the intent
-- is inserted, never derived afterwards by comparing timestamps: the two would
-- come from different clocks, and they tie.
ALTER TABLE intents ADD COLUMN IF NOT EXISTS cause_id BIGINT REFERENCES turn_causes (id);

-- What the model says it was answering, chosen from the causes actually delivered
-- to its turn. Kept apart from cause_id because their provenance differs: one is
-- what the harness observed, the other is what the model claims. NULL is the
-- ordinary case - the model is not required to say.
ALTER TABLE intents ADD COLUMN IF NOT EXISTS answers BIGINT REFERENCES turn_causes (id);

UPDATE intents i
SET cause_id = c.id
FROM turn_causes c
WHERE c.turn_ref = i.turn_ref AND i.cause_id IS NULL;

CREATE INDEX IF NOT EXISTS intents_cause_idx ON intents (cause_id);

-- Both were copies of a fact that now lives in one place.
ALTER TABLE intents DROP COLUMN IF EXISTS session;
ALTER TABLE turns DROP COLUMN IF EXISTS woken_by;
ALTER TABLE turns DROP COLUMN IF EXISTS cause;
