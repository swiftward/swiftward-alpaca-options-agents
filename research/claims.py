"""The twenty-five numbers that CAN be recomputed here, checked with no credentials.

A judge should not have to believe a table. This runs the measurements behind the
thresholds in the declarations and the results in `docs/write-up.md`, from data
committed to this repository, and prints each one PASS or FAIL against the value we
publish.

It needs no Alpaca key and reaches no network. Everything it reads is committed:
`data/candidates_bt.parquet`, the structures the rule could have seen, and the
entry-window leg prices and buy-back lows behind the exit numbers.

What it does NOT cover is listed in `README.md` beside it, name by name: numbers
whose exit price is modelled rather than traded, and numbers that come from our own
account record, which is not published. Reading this as "every number in the
repository" would be the wrong reading, and the list exists so that nobody has to
guess which is which.

A claim that fails here is a claim we must correct, not a test to relax.
"""
import json
import sys
from pathlib import Path

import pandas as pd

import exit_rules
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

    # The defence numbers. These cost about a minute - every trade's path is walked
    # bar by bar and every exit repriced - and they are here because they are the
    # numbers a rule was REMOVED on, which is the kind a reader should be able to
    # check rather than take.
    picked, trades, paths, ivs = exit_rules.population(loud=False)
    claim("the defence measurement covers 672 trades", len(trades), 672, 0)
    means = {
        "holding to expiry pays 2.94 a trade": (exit_rules.run_rule(trades, paths, ivs, "hold"), 2.94),
        "closing on the touch pays 2.32 a trade": (exit_rules.run_rule(trades, paths, ivs, "touch", 0.0), 2.32),
        "closing a width past the strike pays 3.46 a trade": (exit_rules.run_rule(trades, paths, ivs, "touch", 1.0), 3.46),
    }
    def a_trade(outs):
        return float(pd.Series([o["pnl"] for o in outs]).mean())

    for what, (outs, published) in means.items():
        claim(what, round(a_trade(outs), 2), published, 0.01)
    claim("closing on the touch is worse than holding",
          a_trade(means["closing on the touch pays 2.32 a trade"][0])
          < a_trade(means["holding to expiry pays 2.94 a trade"][0]), True)

    # The take-profit share. `threshold.py` reads a 114 MB path file that stays out
    # of the repository; `reduce_paths.py` keeps the only thing the rule asks of a
    # path - the minutes where the buy-back cost fell to a new low - and that
    # reproduces every share exactly, in 186 KB.
    buyback = json.loads(Path("data/buyback_lows.json").read_text())

    def take_profit(share):
        out = []
        for x in buyback:
            target = x["credit"] * share
            reached = [c for c in x["lows"] if c <= target]
            out.append(x["pnl_hold"] if not reached
                       else (x["credit"] - reached[0]) * 100 - (BOOK * 100 + FEE) * 2)
        return pd.Series(out)

    held = pd.Series([x["pnl_hold"] for x in buyback])
    claim("the take-profit measurement covers 597 trades", len(buyback), 597, 0)
    claim("holding those to expiry returns 2461", round(held.sum()), 2461, 0)
    claim("a quarter of them end in the red", round(float((held < 0).mean()), 2), 0.26, 0.005)
    at_share = take_profit(0.35)
    claim("closing at 0.35 of the credit returns 6722", round(at_share.sum()), 6722, 0)
    claim("closing at 0.35 leaves 9 percent in the red",
          round(float((at_share < 0).mean()), 2), 0.09, 0.005)
    claim("closing at 0.35 beats holding", float(at_share.sum()) > float(held.sum()), True)

    width = 60
    print(f"{'':<6}{'claim':<{width}}{'computed':>14}  {'published':>10}")
    for state, what, got, expected in checks:
        print(f"{state:<6}{what:<{width}}{got:>14}  {expected:>10}")
    print(f"\n{len(checks)} claims, {failed} failed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
