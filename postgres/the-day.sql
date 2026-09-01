-- The three numbers a trading day is judged by here, for ONE account's record.
--
-- Written on 1 September so that the evening read is a command and not an
-- improvisation: the decisions it feeds - whether the arriving stride earned its
-- place, whether the width bound starved the list, whether a withheld underlying
-- is still emptying windows - are taken at the end of a day, which is exactly when
-- nobody wants to be writing SQL.
--
-- Runs against one database. `make day` runs it against every record.
-- The day is New York's, because the market's is.

\set ON_ERROR_STOP on

\echo '-- orders: did they fill, or die on patience'
SELECT
    count(*) FILTER (WHERE action = 'filled')                            AS filled,
    count(*) FILTER (WHERE action = 'cancelled')                         AS cancelled,
    round(100.0 * count(*) FILTER (WHERE action = 'filled')
          / nullif(count(*) FILTER (WHERE action IN ('filled','cancelled')), 0)) AS fill_rate_percent
FROM execution_steps
WHERE (at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date;

\echo '-- what each fill paid or cost: this is the price a faster walk is supposed to charge'
--
-- A credit spread is SOLD at a negative limit, so `-became` is money in. A
-- buy-back is bought at a positive one and is money out. Adding their absolute
-- values together - which a first version of this did - counts a buy-back as
-- income and reported 15,703 collected on a day the two accounts gained 4,218.
SELECT
    CASE WHEN became < 0 THEN 'opened (credit in)' ELSE 'closed (debit out)' END AS side,
    count(*)                                        AS fills,
    round(avg(abs(became) * 100 * quantity))        AS per_fill,
    round(sum(abs(became) * 100 * quantity))        AS total
FROM execution_steps
WHERE action = 'filled' AND became IS NOT NULL AND quantity IS NOT NULL
  AND (at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date
GROUP BY 1 ORDER BY 1;

\echo '-- entry windows: how many ended with an intent, and how many named a withheld underlying'
WITH windows AS (
    SELECT DISTINCT turn_ref FROM turn_causes
    WHERE woken_by LIKE 'entry%'
      AND (at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date
), withheld AS (
    SELECT DISTINCT turn_ref FROM said
    WHERE (text ILIKE '%withheld%' OR text ILIKE '%working order%')
      AND (at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date
)
SELECT
    (SELECT count(*) FROM windows)                                          AS windows,
    (SELECT count(*) FROM windows w WHERE EXISTS
        (SELECT 1 FROM intents i WHERE i.turn_ref = w.turn_ref))            AS with_an_intent,
    (SELECT count(*) FROM windows w JOIN withheld h ON h.turn_ref = w.turn_ref
      WHERE NOT EXISTS
        (SELECT 1 FROM intents i WHERE i.turn_ref = w.turn_ref))            AS withheld_and_empty;

\echo '-- the ladder: how far apart one order and the book were at each step'
WITH chained AS (
    SELECT extract(epoch FROM b.at - a.at) AS gap
    FROM execution_steps a
    JOIN execution_steps b ON b.order_ref = a.replaced_by AND b.at >= a.at
    WHERE a.replaced_by IS NOT NULL
      AND (a.at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date
)
SELECT
    (SELECT count(*) FROM chained)                                                      AS steps,
    (SELECT round(percentile_cont(0.5) WITHIN GROUP (ORDER BY gap)::numeric, 1)
       FROM chained)                                                                    AS seconds_between_steps,
    (SELECT round(percentile_cont(0.5) WITHIN GROUP (ORDER BY abs(showing - became))::numeric, 3)
       FROM execution_steps
      WHERE action = 'walked' AND showing IS NOT NULL AND became IS NOT NULL
        AND (at AT TIME ZONE 'America/New_York')::date = (now() AT TIME ZONE 'America/New_York')::date) AS median_distance_to_book;
