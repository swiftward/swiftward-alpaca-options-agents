"""What the account actually did, checked against what this repository says it does.

`make claims` settles the research. This settles the account: it reads the page a
judge already has open - the same read side the demo serves, no credential and no
broker key - and asks whether the trading matches the rules the repository states.

    make account-claims PAGE=https://alpaca.swiftward.dev

Every check below is a sentence somewhere in `docs/`, turned into something a
stranger can run. A check that fails is a claim we have to correct.
"""

import argparse
import json
import sys
import urllib.request

TIMEOUT = 60


def read(page, route, key):
    request = urllib.request.Request(f"{page.rstrip('/')}/api/{route}")
    if key:
        request.add_header("X-Page-Key", key)
    with urllib.request.urlopen(request, timeout=TIMEOUT) as answer:
        return json.load(answer)


class Checks:
    def __init__(self):
        self.rows = []

    def says(self, what, holds, detail=""):
        self.rows.append(("PASS" if holds else "FAIL", what, detail))
        return holds

    def notes(self, what, detail):
        self.rows.append(("----", what, detail))

    def print(self):
        width = max(len(row[1]) for row in self.rows)
        for state, what, detail in self.rows:
            print(f"{state}  {what:<{width}}  {detail}")
        failed = sum(1 for state, _, _ in self.rows if state == "FAIL")
        print(f"\n{sum(1 for s, _, _ in self.rows if s != '----')} checks, {failed} failed")
        return failed


def main():
    parse = argparse.ArgumentParser(description=__doc__)
    parse.add_argument("--page", required=True, help="the page's address, for example https://alpaca.swiftward.dev")
    parse.add_argument("--key", default="", help="X-Page-Key, where the page asks for one")
    where = parse.parse_args()

    money = read(where.page, "money", where.key)
    state = read(where.page, "state", where.key)
    limits = read(where.page, "limits", where.key)
    equity = read(where.page, "equity", where.key)

    checks = Checks()
    account = money["account"]
    orders = money.get("orders") or []
    calls = state.get("calls") or []
    intents = state.get("intents") or []

    checks.notes("account", f"{account['number']}, {account['status']}, "
                            f"options level {account['options_trading_level']}")
    checks.notes("equity", f"{account['equity']:,.2f} now, {account['equity_yesterday']:,.2f} yesterday, "
                           f"{len(equity)} snapshots from {equity[0]['recorded_at'][:10]}")

    # Every order the broker holds is a multi-leg structure. A single-leg order on
    # this account would be a naked option, which every document here says is never
    # opened.
    legged = [o for o in orders if (o.get("legs") or [])]
    singles = [o for o in orders if not (o.get("legs") or [])]
    checks.says("every order is a structure, not a single leg",
                not singles, f"{len(legged)} structures, {len(singles)} single-leg")

    # Closing orders can only close. This is the property the profit watch's path
    # off the gateway rests on, and it is visible in what the broker was sent.
    closes = [leg for o in orders for leg in (o.get("legs") or [])
              if str(leg.get("position_intent", "")).endswith("_to_close")]
    opens = [leg for o in orders for leg in (o.get("legs") or [])
             if str(leg.get("position_intent", "")).endswith("_to_open")]
    checks.says("every leg declares whether it opens or closes",
                all(str(leg.get("position_intent", "")).endswith(("_to_open", "_to_close"))
                    for o in orders for leg in (o.get("legs") or [])),
                f"{len(opens)} opening legs, {len(closes)} closing")

    # The order goes to Alpaca through the gateway, and the record names the server
    # every call was made to. A call to anything else on the order path would show
    # here.
    servers = sorted({c.get("server", "") for c in calls if c.get("tool") == "place_option_order"})
    checks.says("every order was placed through one server",
                len(servers) <= 1, ", ".join(servers) or "no orders in the last calls shown")

    # An intent is recorded before an order. The record carries both, so a fill with
    # nothing behind it is visible rather than deniable.
    checks.says("the intents carry a maximum loss and say whether they close",
                all("max_loss" in i and "is_closing" in i for i in intents),
                f"{len(intents)} intents shown, {sum(1 for i in intents if i.get('is_closing'))} closing")

    # A refusal is not hidden. The page shows the call that was refused and what came
    # back, and a run with no refusals at all would be the surprising one.
    refused = [c for c in calls if c.get("status") != "completed"]
    checks.notes("refusals in the calls shown", f"{len(refused)} of {len(calls)}")

    # What the agent is allowed to do, read from the same service that enforces it.
    if limits.get("governed"):
        rules = ", ".join(sorted(c.get("rule", "") for c in limits.get("constraints") or []))
        checks.says("the limits in force are disclosed to the agent",
                    bool(limits.get("constraints")),
                    f"ruleset {limits.get('ruleset_version')}: {rules}")
        ceiling = next((c for c in limits.get("constraints") or []
                        if c.get("subject") == "position_max_loss"), None)
        if ceiling:
            allowed = account["equity"] * ceiling["value"] / 100
            checks.notes("the position ceiling in force",
                         f"{allowed:,.0f} ({ceiling['value']}% of equity now)")

        # Whether an intent read its limits in the same turn is a fact in the
        # record rather than a sentence in a prompt, and it has three answers, not
        # two. TRUE is a turn that called read_envelope. FALSE is a turn that did
        # not, and the intent was recorded anyway - refusing every intent is worse
        # than keeping an unchecked one, as long as the two never look alike.
        # ABSENT is a deployment with no envelope to read, or a row written before
        # the column existed. So the check is that no intent was written FALSE.
        opening = [i for i in intents if not i.get("is_closing")]
        unchecked = [i for i in opening if i.get("envelope_checked") is False]
        absent = [i for i in opening if i.get("envelope_checked") is None]
        checks.says("no intent was recorded knowing its limits had not been read",
                    not unchecked,
                    f"{len(opening)} opening intents, {len(unchecked)} written unchecked, "
                    f"{len(absent)} where the deployment could not answer")

        # And the loss it names is PROSE the session wrote, not a number this can
        # compare with the ceiling. Saying so is the point: a check that parsed
        # "$9,163 at worst credit $0.23" would be inventing a guarantee. What holds
        # the size is the gateway, and what shows it is the refusal in the calls.
        checks.notes("what an intent's max_loss is",
                     "text the session wrote; the ceiling is enforced at the gateway, not parsed here")
    else:
        checks.notes("limits", "the page reports no governing service")

    sys.exit(1 if checks.print() else 0)


if __name__ == "__main__":
    main()
