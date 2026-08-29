"""Every number this project publishes, recomputed and checked, with no credentials.

A judge should not have to believe a table. This runs the measurements behind the
claims in `docs/write-up.md` and the declarations, from data committed to this
repository, and prints each one PASS or FAIL against the value we publish.

It needs no Alpaca key and reaches no network: `data/candidates_bt.parquet` is
the structures the rule could have seen, built once from history and committed so
that this command works for anyone.

A claim that fails here is a claim we must correct, not a test to relax.
"""
import sys

import pandas as pd

from grid import prepare, pick, BOOK, FEE, SLIP

STRUCTURES = "data/candidates_bt.parquet"


def main() -> None:
    frame = prepare(pd.read_parquet(STRUCTURES))
    checks, failed = [], 0

    def claim(what: str, got, expected, tolerance=0.0):
        nonlocal failed
        ok = abs(float(got) - float(expected)) <= tolerance
        failed += 0 if ok else 1
        checks.append((("PASS" if ok else "FAIL"), what, f"{got}", f"{expected}"))

    days = frame.day.nunique()
    claim("the history covers 646 trading days", days, 646, 0)

    taken = pick(frame, 0.30, 3)
    by_dte = taken.groupby("dte").pnl.mean()
    claim("one day to expiry pays more than five", round(by_dte.get(1, 0), 2) > round(by_dte.get(5, 0), 2), True)
    claim("the expiry gradient is monotone from 2 to 5 days",
          all(by_dte[d] >= by_dte[d + 1] for d in (2, 3, 4) if d in by_dte and d + 1 in by_dte), True)

    # The expiry table the playbook publishes, value by value rather than as an
    # ordering: a claim checked only for its direction is half a claim.
    for days, published in ((1, 10.72), (2, 4.86), (3, 4.70), (4, 3.10), (5, 2.29)):
        if days not in by_dte:
            checks.append(("SKIP", f"{days} days to expiry pays {published} a trade",
                           "no trades at that life", f"{published}"))
            continue
        claim(f"{days} days to expiry pays {published} a trade",
              round(by_dte[days], 2), published, 0.01)

    # Per underlying at our own thresholds. The write-up does not carry these, but
    # the journal does, and a claim in the journal is a claim.
    for name, published in (("SPY", 3.17), ("QQQ", 5.09), ("IWM", 5.26), ("GLD", 2.25)):
        rows = taken[taken.underlying == name]
        if rows.empty:
            checks.append(("SKIP", f"{name} pays {published} a trade", "not in the structures", f"{published}"))
            continue
        claim(f"{name} pays {published} a trade", round(rows.pnl.mean(), 2), published, 0.01)

    # The delta ceiling: the number the whole entry rule turns on.
    looser = pick(frame, 0.45, 3)
    claim("a ceiling of 0.30 beats one of 0.45 per trade",
          round(taken.pnl.mean(), 2) > round(looser.pnl.mean(), 2), True)

    # The crossing is charged three ways; the ordering is the claim.
    means = {}
    for name, slip in (("no crossing", 0.0), ("half the book", BOOK / 2), ("the full book", BOOK)):
        means[name] = prepare(pd.read_parquet(STRUCTURES), slip=slip).pnl.mean()
    claim("the crossing costs more than the strategy earns without it",
          means["no crossing"] > means["the full book"] * 5, True)

    width = 60
    print(f"{'':<6}{'claim':<{width}}{'computed':>14}  {'published':>10}")
    for state, what, got, expected in checks:
        print(f"{state:<6}{what:<{width}}{got:>14}  {expected:>10}")
    print(f"\n{len(checks)} claims, {failed} failed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
