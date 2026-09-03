# Trials

A trial asks one question of one agent and is built so that the wrong answers
look like work. The scenario is only the market; a trial is four files:

```
testbed/trials/<name>/
  scenario.json   the market: prices, clock, staged option book
  sessions.yaml   the task: the windows that ask the question
  trial.json      when each window opens, counted from the proxy's start
  README.md       what separates a right answer from a wrong one
```

Raise one: build the proxy, start it on the trial's market, and point a harness
at it as its broker.

```
make -C testbed build
ARENA_SCENARIO=testbed/trials/<name>/scenario.json \
ARENA_PARTICIPANTS=<token> \
  testbed/bin/testbed-proxy -listen 127.0.0.1:8100
```

The harness takes `http://127.0.0.1:8100` as `BROKER_MCP_URL` and the trial's
`sessions.yaml` as its declaration; `trial.json` says at what offset from the
proxy's start each window opens.

Three things broke, differently each time, when this was assembled by hand on
30 August, and each is a rule for whoever raises a trial. Restart the proxy so the
scenario AND the roster of tokens are re-read. Build the participant's declaration
from a template rather than from a retired participant. Name every window with the
minute it was built in - an `at:` window fires once a day and the harness restores
"already ran today" from the record, so a reused name is a window that silently
never runs.

## Can any harness be put on the same trial and compared?

Yes, and that is the point of the shape: the participant number is the only thing
that changes between runs, and the market, the task and the timing are the same
file. Three things have to be true for the comparison to mean anything, and two
of them were learned the hard way.

**One. The rules text has to be the same.** Participants 1-5 read our testbed rules;
participant 6 reads this repository's own `agent/AGENTS.md` verbatim. That difference is
deliberate - six exists to reproduce findings on the production path - but it
means a run across 3 and 6 compares rules, harness and model at once. To compare
harnesses, hold the rules file identical across the participants in that run.

**Two. The wake mechanism has to be equalised, or you are measuring the driver.**
On 30 August the same interrupt reached one agent in 0 seconds and another in 59,
and the whole difference was one line in `AGENTS.md` telling it to arm its poller
last instead of first. After the line changed, 80 seconds became 13. If a trial
turns on reacting to something mid-turn, check first that every participant can
receive it mid-turn at all - a `Stop` hook, by construction, cannot.

**Three. One participant per run, unless the trial says otherwise.** The staged
depth is shared. These two trials quote 250 contracts against orders of one, so
several participants cannot reach each other; a thin-book trial can, and says so.

## Reading a run out

`testbed/trials/measure-interrupt-priority.py` reads the interrupt trial. It takes
the stage's anchors from the proxy's own log and the scenario file rather than
from constants - typed in, they are wrong on the second run of the day and turn
every order's timestamp into a plausible lie, which is how the first read-out was
wrong.

It is not yet general. The other two trials are read by hand from the record and
the book until it is.

## What every trial here is built to survive

An agent with web search. None of these rest on a fact the web could check: the
premise is always the broker or our own record, where the agent is required to
read it anyway. There are no intraday option quotes anywhere on the web, the
contracts are real, and the price starts at a real print - so nothing invites the
agent to decide the stand is a fiction and act accordingly.

The rule that follows: **never build a trial that needs the agent to believe an
event in the world.** If it goes looking and finds nothing, it is right to doubt
us, and the trial measures our invention rather than its judgement.

## Several participants in one run

`ARENA_PARTICIPANTS` takes several tokens at once, and the proxy keeps a book per
token. Restart it ONCE for the whole group and compute every window time from that
single start - otherwise each participant has a clock of its own and there is
nothing to compare.

One caveat: **the depth shown in the book is one number for everybody.** A trial
whose question turns on scarce depth (`mid-versus-executable`, whose thin leg
shows five contracts) must be run one participant at a time.

Put it back the same way it went up: restore each declaration, and raise the proxy
again with no `ARENA_SCENARIO`, which is the live market with nothing staged.
