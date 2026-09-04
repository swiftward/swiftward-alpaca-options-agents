# The ways an options agent goes wrong, and what this one does instead

Every item below is a failure that is easy to build and hard to see afterwards.
Each says what goes wrong, why it is the natural thing to build, what this system
does instead, and the file where that can be checked. Nothing here is a claim about
anybody else's work: it is a list of the decisions this design is made of, written
as the failures they were chosen to prevent.

## About the numbers a project publishes

**A backtest presented where a result belongs.** A number from a simulation is the
easiest number to produce and the most impressive to read, and once it is on a page
nothing marks it as different from money that moved. Here the three kinds are
listed by name: what recomputes from committed data, what is modelled rather than
traded, and what comes from the account and cannot be recomputed at all
(`research/README.md`). The result on the first screen is total equity on a named
account, and the account's id is published so it can be opened.

**A number with no way to check it.** Publishing a figure and its method is not the
same as publishing a figure anyone can reproduce. `make claims` recomputes
twenty-five of them from data committed to this repository, with no credentials and
no network; it was verified by cloning into an empty directory and running it there.

**A number without the market beside it.** A return means nothing until the reader
knows what the market did in the same window. The first screen carries both, from
the same market data the agent reads (`README.md`).

**A short window read as a measurement.** Four trading days cannot separate skill
from a good draw, whatever the number. This is said in the same table as the result
rather than left for a judge to raise (`README.md`), and the evidence for the
strategy is 646 trading days of committed option prices instead.

**A measurement that chose its own sample.** A filter that selects windows using
information from inside those windows flatters every figure it produces. It happened
here, in the backspread sweep, and it is written up with what the corrected run says
instead (`research/README.md`, `research/sweep_backspread.py`).

## About where the risk actually lives

**Risk limits that live in the prompt.** A ceiling written into an agent's
instructions is enforced by the model's attention. Here the three ceilings that
bound a loss are not in the prompt and not in the agent's file: the session asks for
them while it works and sizes to the answer (`golang/internal/envelope`), and the
service its orders pass through is what refuses one that breaches them - the policy
gateway, reached at `BROKER_MCP_URL` and described in `docs/architecture.md`. Which
of the three the ladder in this repository re-checks afterwards, and which one it
does not, is in `docs/algorithm.md`.

**A limit that cannot be changed without a deploy.** An operator who has to rebuild
an image to tighten a ceiling will not tighten it. Here the next turn reads the new
value, with nothing restarted (`percent_test.go`).

**A guard that is checked after the fill.** A control that can only cancel a resting
order is an observation, not a limit. This system says which of its ceilings are
enforced before an order is forwarded and which are re-checked afterwards, and what
that cost on a day it mattered (`docs/algorithm.md`).

**A guard nobody proved could fail.** A test suite that stays green when a rule is
deleted has measured nothing. Every rule here that can refuse a trade has a test
that was watched to go red with the rule removed, and the one order that leaves
without passing the gateway carries a test that goes red when its intent is flipped
(`TestEveryLegOfAClosingOrderSaysItCloses`).

**A refusal that a program cannot act on.** "Denied" tells a session nothing it can
use. A refusal here names the boundary that stopped it, and the record keeps the
call, its arguments and the answer (`tool_calls`).

## About the trading itself

**Sizing from the premium rather than from the loss.** Sizing on what a structure
collects is how an account discovers that twenty small winners were funded by one
loser it could not carry. Size here comes from the worst case at expiry, computed
from the strikes parsed out of the order's own contracts rather than from anyone's
arithmetic (`execution.WorstCase`).

**Two orders where a structure needs one.** Sending the legs separately opens a
window in which half a spread exists, and half a credit spread is a naked short.
One `place_option_order` with `order_class=mleg` (`marketdata.go`).

**Ranking on prices nobody could trade.** A screen that ranks on raw midpoints ranks
trades that could not have been done. The crossing is subtracted before anything is
compared, measured from this project's own fills, and the research behind the
thresholds charges the full crossing rather than half (`screener/candidate.go`).

**An exit that feels prudent and is not.** Closing a spread the moment the price
touches the strike you sold is the intuitive rule and it is worse than doing
nothing: it pays for crossings that bought nothing. The measurement is published,
and the agent trades the exit that measured better instead
(`research/exit_rules.py`, `make claims`).

**A guard written for the structures of the day.** A rule that counts legs is right
while only verticals are held and wrong from the first backspread - which is what
this watch did until 31 August, and what a backspread cost it. The guard now asks
for two legs with equal quantities sold and bought, which a backspread fails by
construction, and `shapes_test.go` walks every shape a declaration can open so a
fourth cannot be added until each guard says what it does with it
(`golang/internal/structures`, `golang/internal/takeprofit/step.go`).

**No plan for the moment the result is read.** A position open at the cut-off enters
the result at its mark. The window before it cancels every working order - a fill a
minute before the cut is a result nobody chose - and then closes only what the book
will take at no worse than the mark, because closing everything pays the crossing
out of the number being judged.

## About the shape of the system

**An agent you have to program.** When behaviour lives in code, changing what an
agent does is a code change, a review and a deploy - so it is changed rarely, by the
person who can build it, and never during a market. Here the behaviour is a
declaration: the sessions and their causes, the windows, the playbooks that agent
may load, every number it trades on. An operator edits it with the market open and
the next session reads it (`agent/alpaca-agent-tikhon.yaml`, `declaration/watch.go`).

**Risk logic inside the process that trades.** When the thing that decides and the
thing that constrains are the same program, a bug in one is a bug in the other, and
the constraint is only ever as strong as the code that could route around it. Here
the ceilings come from a service outside the agent, reached over an API, which
refuses the call rather than advising against it (`docs/architecture.md`).

**One agent, one account, and no way to add a second.** A design that shares a
credential or a database between agents has two agents that are one agent to the
rules, and a restart of either closes the other's open turns. Here a chain is the
unit: harness, gateway endpoint, broker server, database, page and credential all
carrying one agent's name, so adding an account adds a chain rather than a setting -
and the stack here runs several side by side, each with its own credential,
database and page (`compose.yaml`).

**Two agents that cannot be told apart afterwards.** If the record does not name
which agent made a call, a week of trading is one undifferentiated stream. Every
call here carries the credential that made it, the record names its account, and one
agent can be stopped without touching the other (`record_account`, `db/account_test.go`).

**One model, wired in.** A session that can only run on one provider cannot be moved
when that provider is slow, expensive or down, and cannot use a cheaper model for
work that does not need the expensive one. Here the model is named per session in
the declaration - the window that only reads the news runs on a cheaper one - and
the calls go through a gateway addressed by configuration
(`agent/alpaca-agent-tikhon.yaml`, `model:`).

**Nothing written down about what did NOT happen.** A system that records its fills
records its successes. The refusals are the more useful half: what the agent tried,
what stopped it, and which rule. They are in the same table as everything else and
on the page beside the call each one stopped (`tool_calls`).

**Learning that cannot be pointed at.** A system that tunes itself between runs is
impossible to audit after a bad day: nobody can say which change made it worse,
because no change was written down. Here a measurement changes a number, the number
lives in the declaration, and the declaration records where it came from - the
script and the date for a measured one, or `PROVISIONAL` where nobody has measured
it yet. Every change to how this agent trades is a diff, and every number in it can
be recomputed (`agent/alpaca-agent-tikhon.yaml`, `provenance_test.go`, `make claims`).

**A stack only its author can run.** A system that needs a particular cloud, a
managed database or a vendor console is a system nobody else can check. This one is
a compose file: agent, broker server, record, page, migrations and egress proxy come
up on a laptop or on the cheapest rented box, from this checkout or from published
images (`compose.yaml`, `compose.prod.yaml`, `docs/deploy.md`).

## About what a reader can do with the repository

**A history that starts at the submission.** One squashed commit says nothing about
how the work was done. This one has its dated commits, under MIT.

**A demo nobody can open.** The read side is public and needs no credential of ours;
a second command checks the trading on that account against these documents
(`make account-claims`).

**Documentation that describes an older version of the system.** A document that
disagrees with the code is worse than no document, because it is believed. The
declaration the submitted account runs is the source of truth here, and the
documents are read against it: three rounds of that on 3 and 4 September found the
fuse, the defence rule, the deadline window, the flatten rule and the schedule
described from another agent's file, and each was corrected to the submitted one.

**A test tier that cannot see the thing it covers.** The stand that questions the
agent is a separate module, deliberately outside the workspace so it can never share
a build with what it questions - and it runs inside the same gate, so it cannot rot
unnoticed (`testbed/Makefile`, `.github/workflows/ci.yml`).

**An instrument that only confirms.** A stand that replays history tells you what
already happened. This one takes the real option book and displaces one number along
a curve, repricing each contract by its own live implied volatility - at zero
displacement it equals the live market to the cent - so the agent can be shown a
fall that did not happen, at prices that are otherwise real. Any tool it serves can
also be taken away mid-session, because a tool that goes quiet while the market
moves leaves no trace (`testbed/proxy/overlay.go`, `testbed/README.md`).
