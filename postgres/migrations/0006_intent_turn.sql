-- Намерение принадлежит ходу. Без этого «какой ход это записал» восстанавливается
-- сравнением времени и свободным текстом, который ввела сама модель, - а судья
-- спрашивает именно про причинность.

ALTER TABLE intents ADD COLUMN IF NOT EXISTS turn_ref TEXT;

CREATE INDEX IF NOT EXISTS intents_turn_ref_idx ON intents (turn_ref);
