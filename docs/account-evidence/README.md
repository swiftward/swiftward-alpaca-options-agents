# The account, as the page served it

The five answers behind every number this project publishes about its own trading,
downloaded from the public page at the moment in `taken-at.txt` and committed here.

| File | What is in it |
|---|---|
| `money.json` | the account, every open position, every order with its legs |
| `equity.json` | the equity line, snapshot by snapshot |
| `state.json` | turns, what woke each, the agent's own words, every call with its arguments and answer, the intents, the execution steps |
| `limits.json` | the limits in force, as the agent is told them |
| `sweep.json` | what the screener's last pass found |

**Check them without us:**

```
make account-claims DIR=docs/account-evidence
```

No network, no key, nothing of ours. It asks whether the trading matches what these
documents say: every order a structure rather than a naked leg, every leg declaring
whether it opens or closes, one server behind every order, and no intent recorded
knowing its limits had not been read.

**These are a copy, and the account is the original.** The same five answers are
live at `https://alpaca.swiftward.dev/api/...` for as long as the page is up, and
the account itself is at Alpaca under the id in the root `README.md` - which the
organiser can open and which settles anything this copy is asked to prove. Nothing
here needs a credential of ours, and nothing here can be checked only by us.
