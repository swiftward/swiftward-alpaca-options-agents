-- How many contracts a fill was for.
--
-- Without it the record holds the price and not the money: a fill at 0.28 says
-- nothing about whether we collected twenty-eight dollars or fourteen hundred,
-- and "how much did we collect today" - the question the week is judged on -
-- cannot be answered from our own record at all.
ALTER TABLE execution_steps ADD COLUMN IF NOT EXISTS quantity NUMERIC(14,6);
