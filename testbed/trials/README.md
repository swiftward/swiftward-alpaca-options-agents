# Trials

A trial asks one question of one agent and is built so that the wrong answers
look like work. The scenario is only the market; a trial is four files:

```
arena/trials/<name>/
  scenario.json   the market: prices, clock, staged option book
  sessions.yaml   the task: the windows that ask the question
  trial.json      when each window opens, counted from the proxy's start
  README.md       what separates a right answer from a wrong one
```

Raise one, and put it back:

```
arena/run-trial.sh <name> <participant>
arena/run-trial.sh stop <participant>
```

The runner does the three things that broke, differently each time, when this was
assembled by hand on 30 August: it restarts the proxy so the scenario AND the
roster of tokens are re-read, it templates the declaration from
`participant.yaml.template` rather than from a retired participant, and it names
every window with the minute it was built in - because an `at:` window fires once
a day and the harness restores "already ran today" from the record, so a reused
name is a window that silently never runs.

## Can any harness be put on the same trial and compared?

Yes, and that is the point of the shape: the participant number is the only thing
that changes between runs, and the market, the task and the timing are the same
file. Three things have to be true for the comparison to mean anything, and two
of them were learned the hard way.

**One. The rules text has to be the same.** Participants 1-5 read our arena rules;
participant 6 reads the team's own `agent/AGENTS.md` verbatim. That difference is
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

`arena/trials/measure-interrupt-priority.py` reads the interrupt trial. It takes
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

`run-trial.sh <trial> 3 4 5` raises one trial for several at once: the proxy is
restarted ONCE for the whole group, and the window times are computed from that
single start - otherwise each has a clock of its own and there is nothing to
compare.

One caveat: **the depth shown in the book is one number for everybody.** A trial
whose question turns on scarce depth (`mid-versus-executable`, whose thin leg
shows five contracts) must be run one participant at a time.

Put it back the same way: `run-trial.sh stop 3 4 5` restores the declarations,
folds the book away and raises the proxy with no scenario.
