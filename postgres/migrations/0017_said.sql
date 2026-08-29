-- What the agent SAID, not what it did.
--
-- Turns and tool calls have been recorded for a long time, and they show WHAT
-- happened. They do not show why: the reasoning lives in codex transcripts, in
-- files on disk, in its own format and with no link to our turns.
--
-- That is not enough for the page. A judge opens the address and must see not
-- only the equity curve but the decision in words: what the session based an
-- entry on, why it declined, what it found in the chain. Parsing someone else's
-- JSONL on every request would mean a second route to the data and a dependency
-- on a format that changes with codex. The harness already holds the line in its
-- hands - it is what sends it to the chat.
--
-- The transcripts remain the full raw record for our own analysis. What is shown
-- is here.
create table if not exists said (
    id         bigserial primary key,
    turn_ref   text        not null,
    at         timestamptz not null default now(),
    text       text        not null
);

-- This is always read the same way: newest first, and everything said inside a turn.
create index if not exists said_at_idx on said (at desc);
create index if not exists said_turn_idx on said (turn_ref, at);
