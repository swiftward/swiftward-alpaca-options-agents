-- Sweeps accumulate instead of replacing one another, so every reader now
-- selects BY sweep: the agent takes the newest, the purge takes the oldest.
CREATE INDEX IF NOT EXISTS candidates_swept_at_idx
    ON candidates (swept_at DESC);
