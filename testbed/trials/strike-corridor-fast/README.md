# strike-corridor-fast — the control for `strike-corridor`

Everything is identical to `strike-corridor` except one line: the defence looks
`every: 3m` instead of `every: 15m`.

It exists because the first run, on its own, proves nothing. If the price crosses
the corridor and the spread is not closed, there are two explanations and they
call for opposite fixes:

- **the schedule** — nobody was asking while the answer would have been yes;
- **everything else** — the rule was read and not applied, the position was not
  recognised as a vertical, the close was refused for a price, the tool failed.

Only a second run separates them. With a check every three minutes a corridor
occupied for seven and a half minutes is caught two or three times over. If the
spread is closed here and was not closed there, the schedule is the cause and the
finding stands. If it is not closed here either, the first run measured something
else, and reporting it as a schedule problem would be a confident answer to a
question that was never asked.

Six of this instrument's own faults on 31 August had exactly that shape: a
measurement that succeeds and answers a different question. Hence the rule the
stand now works to - every trial carries a run that could have come out the other
way.

    arena/run-trial.sh strike-corridor-fast 6
    python3 arena/trials/read-strike-corridor.py
    arena/run-trial.sh stop 6
