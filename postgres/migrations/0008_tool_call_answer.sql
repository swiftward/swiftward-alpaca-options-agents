-- What the tool answered. A broker refusal travels inside the answer rather than
-- as a protocol error: on 25 August a rejected order was recorded as a successful
-- call, and the record could not tell it from a filled one.

ALTER TABLE tool_calls ADD COLUMN IF NOT EXISTS answer TEXT;
