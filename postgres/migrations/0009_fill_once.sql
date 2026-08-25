-- A fill is written down once, and the database is what decides that.
--
-- The ladder polls the broker every forty-five seconds, so it meets the same
-- filled order many times, and a restart forgets whatever it held in memory.
-- Announcing a fill twice would put the same trade in the room twice and in the
-- record twice, and the record is what the week is judged on.
CREATE UNIQUE INDEX IF NOT EXISTS execution_steps_one_fill_per_order
    ON execution_steps (order_ref)
    WHERE action = 'filled';
