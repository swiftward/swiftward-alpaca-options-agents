-- Sixty readings of QQQ implied volatility, so that today's is high in its own
-- history and the rank the session tool answers is 88.
--
-- Why seeded rather than measured. The rank is computed from THIS agent's own
-- history (`volatility_samples`), which is written by the recorder role - a role
-- the arena does not run, and which in any case would need weeks to build a
-- history worth ranking against. The rule under test forbids selling premium
-- when the rank is above 80, and that rule has never once been exercised here:
-- with an empty table the tool answers nothing and the gate cannot refuse.
--
-- The numbers are a shape, not a market: fifty-nine readings walking between
-- 0.132 and 0.198, and today's at 0.196. Fifty-two of the fifty-nine sit below
-- it, so the rank lands at 87. Nothing here claims to be what QQQ actually did.
DELETE FROM volatility_samples WHERE underlying = 'QQQ';

INSERT INTO volatility_samples
  (underlying, contract, recorded_at, expiration, strike, option_type,
   implied_volatility, delta, bid, ask, underlying_price)
SELECT
  'QQQ',
  'QQQ260904P00710000',
  now() - (make_interval(days => g)),
  DATE '2026-09-04',
  710,
  'put',
  v.iv,
  -0.50,
  v.iv * 10,
  v.iv * 10 + 0.10,
  716.00
FROM generate_series(59, 1, -1) AS g
CROSS JOIN LATERAL (
  -- A walk that stays in a band, with a handful of quiet days near the bottom
  -- and no single spike: a rank has to be earned by where the reading sits in
  -- the series, not by one outlier dragging the ends apart.
  SELECT round((0.132 + 0.066 * (0.5 + 0.5 * sin(g::numeric / 4.7)))::numeric, 4) AS iv
) AS v;

-- Today's reading, the one the rank is about.
INSERT INTO volatility_samples
  (underlying, contract, recorded_at, expiration, strike, option_type,
   implied_volatility, delta, bid, ask, underlying_price)
VALUES
  ('QQQ', 'QQQ260904P00710000', now() - interval '2 minutes', DATE '2026-09-04',
   710, 'put', 0.1960, -0.50, 1.96, 2.06, 716.00);
