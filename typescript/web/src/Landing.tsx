import { ArrowDown, CircleSlash, Eye, EyeOff, FileCode, Layers } from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { Link } from 'react-router'

import { Boundary } from './Boundary'
import { Card, Chip, Eyebrow, Figure, Figures, inline, LiveLink, Mark, Yaml } from './parts'
import { ACCOUNT, ACTIVITY, BENCHMARK, MEASUREMENTS, OPENED, type Measurement } from './snapshot'

// The landing page: what this is, how it differs, and where to go and look.
//
// Ordered for a reader who arrives and leaves, not for one who is walked through.
// What the session DOES comes first, because a reader who cannot see the model
// working reads everything after it as a backtest with a chat window attached.
// The limits come second and the measurements third.
//
// Every section carries a picture and about forty words. The version before this
// one was six paragraphs of a hundred words each and read as a report: the eye
// had nowhere to stop and nothing to look at. A payoff, three bars and a day on a
// line say at a glance what those paragraphs spent a screen on.
export function Landing() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-14">
      {/* THE FIRST SCREEN CARRIES THE CLAIM AND THE EVIDENCE SIDE BY SIDE.
          Before this the headline promised profit and the first number appeared
          seven screens down, in section 07 - a reader who left early left having
          been told, not shown. The settled figure now sits under the sentence it
          proves, and the four steps of a window sit beside it, so the screen
          argues in two directions at once instead of scrolling in one. */}
      <div className="grid items-start gap-x-12 gap-y-12 lg:grid-cols-12">
        <div className="lg:col-span-7">
          {/* The bar above already carries the name and the live dot. Repeating
              them here said the same thing twice and told a reader nothing; this
              line now says what the thing IS, which is what a reader arriving
              from a link actually needs first. */}
          <Eyebrow>[ Autonomous options agent · Alpaca paper account ]</Eyebrow>

          <h1 className="mt-6 max-w-[17ch] text-[46px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[58px]">
            It trades options for profit, and it is held to what it may lose.
          </h1>

          <p className="mt-6 max-w-[46ch] text-[20px] leading-[1.35] tracking-[-0.01em] text-secondary">
            A harness that puts intent, a model and a risk engine on one line — so that what the agent
            means to do is written down, what it decides is its own, and{' '}
            <Mark>what it may lose is not its to change</Mark>.
          </p>

          <Actions />
        </div>

        <div className="lg:col-span-5">
          <Flow />
        </div>
      </div>

      {/* THE PLAQUE IS THE ACCOUNT. It used to hold four facts about the system -
          windows, names, limits, checks - which are all argued at length further
          down; on the first screen they asked the reader to take an interest
          before being given a reason to. What the account is worth, what it made,
          and how much deciding produced it is the reason.

          Equity and the profit come from the same array section 07 prints, and
          the profit is worked out from the opening balance, so the two places
          cannot drift apart. */}
      <div className="mt-14">
        <Figures spread>
          <Figure name="equity" value={money(SETTLED.equity as number)} />
          <Figure
            name={`P&L since the $${OPENED.toLocaleString('en-US')} start`}
            value={`${change((SETTLED.equity as number) - OPENED)} · ${(
              (((SETTLED.equity as number) - OPENED) / OPENED) *
              100
            ).toFixed(2)}%`}
            tone="gain"
          />
          {/* The market goes NEXT TO the profit, not in a footnote, because the
              two are one claim and reading either alone gets it wrong. */}
          <Figure
            name={`${BENCHMARK.name} over the same window`}
            value={`+${BENCHMARK.percent.toFixed(2)}%`}
          />
          {ACTIVITY.map(([name, value]) => (
            <Figure key={name} name={name} value={value} />
          ))}
        </Figures>

        {/* ONE CAPTION, not two loose blocks. What stood here was a full-width
            line of uppercase mono and a four-line paragraph under it, and neither
            belonged to anything: a reader met them after the numbers had already
            been read and had no reason to start.

            The account's id earns its place - it is what lets a judge open the
            account instead of trusting the figures - and so does the sample-size
            caveat, which is better said by us than raised by a judge. The claim
            about numbers recomputing came out: it is made in full, with the
            command that proves it, on the page for judges. */}
        <p className="mt-4 max-w-[86ch] text-[13px] leading-relaxed text-muted">
          <span className="font-mono text-secondary">{ACCOUNT}</span> · Alpaca paper ·{' '}
          {BENCHMARK.sessions} trading days, {BENCHMARK.window}. Four sessions cannot separate skill
          from a good draw; the evidence for the strategy is the 646 trading days of option prices
          committed to the repository.
        </p>
      </div>

      {/* THE THREE THINGS THIS PROJECT HAS THAT THE FIELD DOES NOT.
          What stood here was longer and softer - three paragraphs a reader skims
          and remembers none of. Each is now one fact with a number behind it,
          because a card is read in about four seconds and what is not read is not
          an argument. The long version of each is in the sections below and in
          the repository, which is where a reader who wants it goes.

          The limits are NOT one of the three, and that is deliberate: the page
          makes that claim four times before this block - in the headline, in the
          sentence under it, on the accented step of the diagram, and for the whole
          of section 02. What the page did not say anywhere was that a session
          reads and decides at all. In this field that is the rare half: the usual
          "agent" is deterministic gates around a single model call. */}
      <div className="mt-16 mb-20 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={FileCode}
          title="A session, not a pipeline"
          says="The screener prices the permitted field every few minutes. A session then reads its four playbooks and chooses: structure, strikes, size — or no trade at all."
        />
        <Claim
          icon={Layers}
          title="The test moves the real book"
          says="No replay, no simulation. Every price comes from the live broker with one number displaced. At zero displacement it matches the market to the cent."
        />
        <Claim
          icon={CircleSlash}
          title="The rule our data killed"
          says="Our defence closed a position when the strike was touched. Over 672 trades it lost to doing nothing. We published that and changed the rule."
        />
      </div>

      {/* SECTIONS 01 TO 04 ARE THE FOUR STEPS OF THE DIAGRAM ABOVE, in the order
          the diagram draws them. The first screen promises a shape; these prove it.
          Before this the sections were about the harness, the limits, the
          instrument and the research, which is a different list from the one the
          picture at the top had just made a reader memorise. */}

      <Block
        label="01 · intent"
        title="It says what it means to do, before it may do it."
        explains="A window wakes a session with a CAUSE and a task, never an answer. Nothing in the file says what to trade. What the session then writes down is filed before the order exists, and the order refers back to it."
      >
        <Card>
          <Day />
          <div className="mt-8 border-t border-line pt-6">
            <Yaml title="alpaca.yaml" source={SCHEDULE} />
          </div>
          {/* WHAT THE FILE BUYS YOU, said under the file itself, and how the thing
              learns - which is the same sentence, because here they are the same
              mechanism.

              The word `self-improving` is deliberately absent. It promises a
              system that rewrites itself, and ours does not: a person edits the
              file. That is the ADVANTAGE, not the shortfall - a system that
              improves itself in a way nobody can point at cannot be taken apart
              after a bad day. Saying `learns` keeps the claim true. */}
          <Pull>Every change to how it trades is a diff. Every number in it can be recomputed.</Pull>
          <p className="mt-4 text-[15px] leading-relaxed text-secondary">
            Everywhere else that is a deploy. Here it is an edit — and that is also how it
            learns: whole rules are added, rewritten and deleted, not only numbers tuned, and the
            file records what each change was made on. The defence rule was deleted because 672
            trades said it lost to doing nothing.
          </p>
        </Card>
      </Block>

      {/* THE LEAD NAMES A BOUNDARY, not a talent.
          Two earlier versions promised more than the section can show. The first
          asked whether an answer could be "wrong in an interesting way" - a good
          test inside a design document, where there is a page to unpack it, and a
          riddle on a landing. The second asked whether a spread was paying more
          because something was about to happen, which set a reader up for a
          demonstration of judgement that is not here and should not be: this
          system takes arithmetic AWAY from the model on purpose, so its record is
          full of thresholds being applied, and dressing that as cleverness argues
          against our own case. What the model gets and what it does not is written
          down, and that is the claim worth making because it can be checked. */}
      <Block
        label="02 · the model"
        title="One session, awake all day, choosing from a short list."
        explains={
          <>
            Pricing six hundred spreads is arithmetic, and arithmetic is code. Which structure,
            which side, whether to sit the window out:{' '}
            <Mark>the choices have no formula, and they are the session's</Mark>. The ceilings on a
            loss are not: those come from the risk engine. The file says which is which, so the
            boundary is one anyone can read.
          </>
        }
      >
        <Card>
          <Funnel />
          {/* ONE CLAIM, no counts. The version before this spent three lines on
              152 wake-ups, ten sessions and a thirty-seven-turn thread - true, and
              measured, but a reader does not arrive wanting our arithmetic. What
              they need is that the session is not restarted for each window.

              And the claim is exactly what `harness.go` says it is - "one thread
              with the agent, held open across turns" - so it is the CONVERSATION
              that persists. Saying the agent "remembers everything" would claim
              more than that. */}
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            The six do not arrive at a fresh prompt. A window wakes a conversation held open across
            turns and hands it the task — so what the session decided this morning is still in
            front of it when it decides again in the afternoon.
          </p>
          <Pull>Give a model everything and it is a script with a random number generator in it.</Pull>
        </Card>

        <div className="mt-4">
          <Card>
            <Playbooks />
            {/* THE PICTURE ALREADY SHOWS THE MECHANISM - each playbook naming what it
                needs from the file - so the words say what it BUYS instead of
                repeating it.

                What is NOT said here any more: that two accounts can run the same
                playbook on different numbers and the difference between them is the
                experiment. True of how this was built, and the wrong sentence to
                put beside a submitted P&L - a judge reading that column can take it
                for several accounts run and the best one entered. The mechanism is
                the same either way and the auditable half of it is the stronger
                claim. */}
            <p className="mt-7 text-[15px] leading-relaxed text-secondary">
              A playbook has to name the numbers it needs and the declaration supplies them, so
              nothing about size or risk hides inside a technique: one file holds every number this
              agent trades on. The window picks the model too — the news pass runs on a smaller one.
            </p>
            <Pull>The mechanics live in the playbook. The choices live in the task.</Pull>
          </Card>
        </div>
      </Block>

      {/* ONE NAME FOR ONE THING. This section was called `policy`, the diagram
          above said `envelope`, and the prose said `the service the order passes
          through` - three names for the same component, and a reader has to work
          out that they are one. The repository settled on `risk engine` because a
          reader who trades already knows what that is; `envelope` is our own word
          and means nothing outside this team.

          The agent's own quotes still say `envelope`: that is the tool it calls,
          and an edited quotation is not a quotation. */}
      <Block
        label="03 · the risk engine"
        title="It knows the wall is there. It does not know where."
        explains="Every rule carries how much of itself it discloses. Tell a session the size cap and it splits one order into four; tell it only that a rule exists, and there is nothing to route around. The risk engine every order passes through is what refuses it, and the refusal names the rule."
      >
        <Card>
          <Envelope />
          {/* THE POINT THE SECTION LEFT UNSAID: that the engine is not something the
              session can reach. Without it the page claims a wall and a reader may
              picture a rule the agent could talk its way past. */}
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            The engine is a service of its own, on the path every order takes. Nothing in the
            session&apos;s reach edits it — and an operator can tighten a rule while the agent is
            running, with the next turn seeing the new one.
          </p>
          <Pull>A prompt is a request. An engine that holds the order is a wall.</Pull>
        </Card>

        {/* ONE REFUSAL, because a section about a wall that never shows it stopping
            anything is asking to be taken on trust. The simplest of the week: the
            engine requires the limits to be read immediately before acting, and
            this intent had not. */}
        <div className="mt-4">
          <Card>
            <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              what the engine returned · 2 September
            </p>
            {/* THE ENGINE'S OWN WORDS, taken from the failed call rather than from
                the agent's account of it. It explains its own rule better than we
                do, and it is the proof of the sentence in the card above: limits
                change while a conversation runs. */}
            <p className="mt-3 border-l-[3px] border-accent pl-5 text-[17px] leading-relaxed text-primary">
              {inline(
                '“call `read_envelope` in this turn before recording an intent: limits change while a conversation runs, and an answer from an earlier turn is not this turn’s answer”',
              )}
            </p>

            <p className="mt-6 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              and what the session did about it
            </p>
            <p className="mt-3 text-[15px] leading-relaxed text-secondary">
              I&apos;m re-reading the envelope now, then I&apos;ll re-record the same stated trade
              only if the limits remain valid.
            </p>

            <p className="mt-6 text-[15px] leading-relaxed text-secondary">
              Nothing was wrong with the trade. The order simply never reached the broker, because
              the limits it was sized against had been read a few turns earlier — and a few turns
              is long enough for them to have changed.
            </p>
          </Card>
        </div>
      </Block>

      <Block
        label="04 · the order"
        title="The price is walked. Every concession is re-priced first."
        explains="The session names the worst price it will accept. From there it is code: the limit walks toward the book, re-priced before every concession, so it cannot concede its way into a trade that no longer clears."
      >
        <Card>
          <Ladder />
          {/* The gap is NAMED here rather than left for a judge to find. A page that
              lists only the guards that worked is describing a different system. */}
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            Two of the three ceilings are checked again here. The third — everything betting the
            same way — is not, and we write that down rather than leave it to be found.
          </p>
          <Pull>
            A guard that can only cancel a resting order is an observation, not a limit.
          </Pull>
        </Card>

        {/* THE WORST CASE WAS FIXED BEFORE THIS STEP, which is why the walking is
            about price and never about risk. This card was in section 02 and did
            not belong: what a spread IS is a fact about the instrument, not about
            how the model decides. Here it answers the question the section raises
            - what is actually being sent, and what can it cost. */}
        <div className="mt-4">
          <Card>
            <Payoff />
            <p className="mt-7 text-[15px] leading-relaxed text-secondary">
              It sold the right to buy SPY at $772 and bought the right to buy at $773. That gap is
              the whole risk: no move in the world costs more than it, less the credit.
            </p>
            <Pull>
              A stop-loss is a hope: it fills where the market lets it. A bought leg is a contract.
            </Pull>
          </Card>
        </div>
      </Block>

      <Block
        label="05 · how it is tested"
        title="Built to attack it, not to agree with it."
        explains="Every rule here that can refuse a trade has a test that FAILS when the rule is removed. A suite that stays green when a gate is deleted has measured nothing."
      >
        <Card>
          <Stand />
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            On top of that, any tool the stand serves can be made to answer with a stated message
            for a stated stretch. A market that misbehaves is only half of what breaks an agent.
          </p>
          <Pull>The other half is a tool that goes quiet, and it leaves no trace: nothing crashes.</Pull>
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            Thirteen trials have run through it. Two defects it caught this week had passed a green
            suite: a cadence measuring 45.002 and 89.999 seconds where 45 was declared, and an order
            that lived nineteen minutes against a patience of eight. No judged order has ever gone
            through the stand — every order on this account went to Alpaca&apos;s own server.
          </p>
        </Card>

        {/* THE RULES ARE ATTACKED THE SAME WAY, and this is what that cost us. The
            section used to be this card alone under the heading `the measurements`,
            which promised several and showed one. */}
        <div className="mt-4">
          <Card>
            <p className="text-[15px] leading-relaxed text-secondary">
              The rules are attacked the same way. Ours closed a spread the moment price touched the
              sold strike, and it felt prudent for four months. Measured across 672 trades,{' '}
              <Mark>it loses to doing nothing</Mark>.
            </p>
            <div className="mt-7">
              <Exits />
            </div>
            <p className="mt-7 text-[15px] leading-relaxed text-secondary">
              Price passed the sold strike in 42.7% of trades and only 26.6% ended breached. The
              rule was paying for 108 crossings that bought nothing, so we deleted it.
            </p>
            <Pull>A measurement that only ever agrees with you is not a measurement.</Pull>
          </Card>
        </div>
      </Block>

      {/* THE THREE IDLE DAYS ARE STATED, not left to be noticed. The account was
          opened on the kickoff day - which is the one rule about accounts this
          hackathon has - and its first order went out three days later, when the
          organiser's own measurement window opens. Both facts were already on the
          page, in different places, and a reader had to put them together; said in
          one breath they answer the question before it is asked, and there is
          nothing in the gap: one funding of 100,000 and no activity of any kind
          until the Monday. */}
      <Block
        label="06 · the result"
        title="Two judges, two clocks, and both are named here."
        explains="One account, opened and funded with $100,000 on the kickoff day and never reset. Its first order went out on the Monday, when the measurement window opens. The result is then taken twice by two clocks that disagree on when the week ends, so both cut-offs are printed with the rule each uses."
      >
        <Card>
          <ul className="m-0 list-none space-y-4 p-0">
            {MEASUREMENTS.map((m) => (
              <Measured key={m.by} of={m} />
            ))}
          </ul>

          {/* THE MARKET, in the section that IS the result. It was on the first
              screen and nowhere else, so a reader who scrolled straight to the
              word `result` met a return with nothing to weigh it against and had
              to remember a number from seven screens up.

              It sits under the list rather than inside a row because it belongs to
              the settled window and not to both: the second measurement closes on
              a different clock. */}
          <p className="mt-6 border-t border-line pt-5 text-[15px] leading-relaxed text-secondary">
            Over the settled window — {BENCHMARK.sessions} sessions, {BENCHMARK.window} — the market
            it trades went{' '}
            <span className="font-medium tabular-nums text-strong">
              +{BENCHMARK.percent.toFixed(2)}%
            </span>
            , read from the same market data the agent reads.
          </p>
        </Card>

        {/* The page ends by handing the reader off. Both figures above are frozen -
            they have to be, a poster cannot depend on a broker answering - and the
            honest next sentence is "the moving one is over here". */}
        <div className="mt-8 flex flex-wrap items-center gap-x-5 gap-y-3">
          <LiveLink />
          <span className="text-[15px] text-secondary">
            Both figures above are frozen. The account itself is not.
          </span>
        </div>
      </Block>
    </main>
  )
}

// An AMOUNT and a CHANGE are written differently, and confusing them is how a
// balance ends up wearing a plus sign it did not earn. What the account holds
// takes no sign; what it made since the kickoff takes one.
function money(amount: number) {
  return `$${amount.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function change(amount: number) {
  return `${amount < 0 ? '-' : '+'}${money(Math.abs(amount))}`
}

// ============================================================
// The pictures.
//
// Hand-authored rather than the chart library. ../CLAUDE.md sends charts to
// recharts, and the reason it gives is axes, ticks and hitting the nearest point
// with a mouse - a data chart the reader interrogates. None of these is that:
// they are fixed diagrams of numbers that do not move, with nothing to hover and
// no scale to parse. Each takes its colour from the page's roles, so none of them
// carries an opinion about which theme it is in.
// ============================================================

// The four things that stand between a window opening and an order existing, and
// what each of them answers. Drawn rather than listed because the argument is the
// ORDER of them: the model decides in the middle, with something written down
// before it and something it cannot reach after it. A list of four bullets says
// the same words and loses the sequence, which is the whole claim.
function Flow() {
  // Two of the four rows carry a mark, and they answer the two questions a reader
  // actually arrives with. `decides` is dark because that step is the model and
  // nothing else - the page is read by people asking where the AI is. `holds`
  // carries the accent because it is the step nobody else has: the order can be
  // refused after the model has chosen it, and the session cannot argue.
  const steps: [string, string, string, 'strong' | 'accent' | undefined][] = [
    ['intent', 'what it means to do, and why', 'written first', undefined],
    ['the model', 'reads the book, picks the structure, sizes it', 'decides', 'strong'],
    ['the risk engine', 'refuses what breaks the limits', 'holds', 'accent'],
    ['the order', 'walked to the book, or cancelled on patience', 'sent', undefined],
  ]

  return (
    <figure className="m-0">
      <div className="rounded-xl border border-line bg-surface-raised p-5">
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          one window, from waking to an order
        </p>
        <ul className="m-0 mt-4 list-none p-0">
          {steps.map(([name, does, state, tone], index) => (
            <li key={name} className={index === 0 ? 'py-3' : 'border-t border-line py-3'}>
              <div className="flex items-center justify-between gap-3">
                <span className="flex items-baseline gap-2.5">
                  <span className="font-mono text-[12px] text-muted tabular-nums">
                    0{index + 1}
                  </span>
                  <span className="text-[16px] font-medium text-primary">{name}</span>
                </span>
                <Chip tone={tone}>{state}</Chip>
              </div>
              <p className="m-0 mt-1 pl-[30px] text-[14px] leading-snug text-secondary">{does}</p>
            </li>
          ))}
        </ul>
      </div>
      <div className="flex justify-center py-2.5 text-muted" aria-hidden>
        <ArrowDown className="size-5" />
      </div>
      <div className="rounded-xl border border-line bg-surface-raised px-5 py-4">
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-gain">
          what the account keeps
        </p>
        <p className="mt-1.5 text-[17px] font-medium leading-snug text-primary">
          The credit, less what the loss was allowed to be
        </p>
      </div>
    </figure>
  )
}

// The trading day and what the file puts in it. Three entries open the account;
// what keeps it repeats too often to draw and is said in a line instead.
type Window = {
  hour?: number
  name: string
  closes?: boolean
  rule: string
  said: { at: string; text: string }
  // The intent as the record holds it, and ONLY where one was actually filed.
  // Two of the five windows below have one, and they are exactly the two where an
  // order was sent - which is the section's whole claim, shown rather than said:
  // no intent, no order.
  filed?: { structure: string; worst: string }
}

// One trading day, and what each window is FOR - taken from the declaration's own
// `cause` field, word for word - beside what the session actually said in it,
// taken from the record on the stand, unedited.
//
// The marks are buttons rather than a hover: a tooltip does not exist on a phone,
// and half the readers of this page are on one. Choosing a window is also how a
// keyboard gets at the same thing.
//
// THE LINES ARE CHOSEN FOR WHAT THEY SHOW, NOT FOR FALLING ON ONE DAY. The axis is
// a day as the file declares it; each line carries its own date, and they come
// from three different ones. An earlier set was four refusals in a row, which read
// as an agent that never does anything - and where a line had not been looked for
// at all it said "nothing in the record", which reads as a broken page. Three of
// the five below are the session ACTING: two orders sent and one position bought
// back.
//
// A line is attached only where its timestamp falls inside that window. Putting a
// line under the wrong session would be inventing a quotation.
// The four that sit on the axis: each happens once, at a declared hour.
type Timed = Window & { hour: number }

const WINDOWS: Timed[] = [
  {
    hour: 10.33,
    name: 'entry',
    rule: 'morning entry window, taken only when premium is dear',
    said: {
      at: '1 September, 10:20',
      text: 'Used task thresholds (13% credit-to-risk, +3 fresh edge, one underlying) under envelope `2026-08-31.1`. Submitted 123 QQQ Sep 4 717/718 call spreads at −0.29 credit, worst −0.26.',
    },
    filed: {
      structure:
        'Sell 123 QQQ260904C00717000 (717 calls) and buy 123 QQQ260904C00718000 (718 calls), same-expiry call credit spread; seek 0.29 credit and accept no worse than 0.26 credit.',
      worst:
        '$9,102 at a 0.26 credit (74 points risk x 123 contracts x $100), 8.95% of 101719.3 equity; sized to nine-tenths of the envelope’s 10% position maximum under ruleset `2026-08-31.1`.',
    },
  },
  {
    hour: 12.5,
    name: 'entry',
    rule: 'midday entry window: a position taken here lives half a session',
    said: {
      at: '1 September, 12:30',
      text: 'The screener is fresh. The top three candidates by listed edge — therefore the only three I will re-price this turn — are SPY Sep 8 774/775 calls (+3.07), QQQ Sep 2 714/715 calls (+2.33) and IWM Sep 4 295/296 calls (−0.07).',
    },
  },
  {
    hour: 14.33,
    name: 'entry',
    rule: 'the main entry window of the day',
    said: {
      at: '31 August, 14:20',
      text: 'I took the task’s 0.30 delta cap, +3 fresh-edge rule, borrowed-edge >0 rule, and 2% fuse; the envelope governed. After QQQ and SPY failed fresh tests, I submitted 40 META 570/567.5 put spreads at 0.37 credit, worst 0.29; accepted loss $8,840.',
    },
    filed: {
      structure:
        'Sell 40 META260831P00570000 (570 puts) and buy 40 META260831P00567500 (567.5 puts), same-day vertical credit spread; seek 0.37 credit, accept no worse than 0.29 credit.',
      worst:
        '$8,840 at a 0.29 credit (calculated: $221 x 40 contracts; 2.5-point width less $0.29 credit), 8.79% of 100652.95 equity; 90% sizing ceiling is 9% and envelope position maximum is 10% under ruleset `2026-08-31.1`.',
    },
  },
  {
    hour: 15.67,
    name: 'flatten',
    closes: true,
    rule: 'close what must not be held overnight',
    said: {
      at: '2 September, 15:40',
      text: 'The chain spacing is 2.5 points, so the half-gap threshold is 1.25; SMH `p=549.67` is 5.33 below the 555 short strike. It is not near assignment, so I’m leaving it to expire.',
    },
  },
]

// EVERYTHING THAT IS NOT A TIME OF DAY. The file declares thirteen windows; four
// of them sit on the axis above because they happen once at a fixed hour, and
// these five do not - three run on a cadence all day, two only on their own day of
// the week. They were missing entirely, and with them the picture was claiming a
// trading day is three entries and a close.
//
// A row of pills rather than dashed bands stacked under the axis: five bands would
// be five lines of furniture, and a pill is a control a reader already knows how
// to read.
const OTHERS: Window[] = [
  {
    name: 'news · every half hour',
    rule: 'news check on the underlyings we hold',
    said: {
      at: '1 September, 09:35',
      text: 'Relevant news: oil/U.S.–Iran risk, higher yields, and rising September Fed-hike expectations affected SPY/QQQ; no IWM-specific item. SPY alone is held; at 762.305 it remains 19.305 from the 743 short strike versus the 7.905 trigger threshold. No wake-up or order.',
    },
  },
  {
    name: 'defence · every half hour',
    rule: 'the defence rules',
    said: {
      at: '31 August, 11:09',
      text: 'Closing a structure that has given back its credit: buy-back at 0.10 against 0.36 taken, which is 27.8% of the credit given up against the 35% the numbers allow.',
    },
  },
  {
    name: 'convexity · every two hours',
    rule: 'convexity layer: a cheap bet on a large move, bounded and known',
    said: {
      at: '31 August, 13:51',
      text: 'I took the task’s 1.5/2.5-sigma placement rules, 2% loss caps and 10% width round-trip cap; the envelope governed. Submitted 2 SPY Sep 2 743/734 put-backspread sets at 0.04 debit, worst 0.05; accepted loss $1,810.',
    },
  },
  {
    name: 'earnings · Wednesday',
    rule: 'the earnings window: if a report lands after the close tonight, sell what the unknown has inflated',
    said: {
      at: '2 September, 15:20',
      text: 'The measurement is nearly neutral: fresh AVGO 4 September 370-strike mids imply about 8.09%, against the task’s 8.0% median realized move. The resulting ratio is between the task’s 0.8 buy and 1.3 sell thresholds.',
    },
  },
  {
    name: 'event bet · Thursday',
    rule: 'the employment number lands in the morning: buy the gap tonight',
    said: {
      at: '3 September, 15:30',
      text: 'SPY was 773.435; account equity $102,335.60 and positions were empty. Submitted and accepted: 8 Sep-4 773 straddles at $4.00 debit, max loss $3,200; and 174 Sep-4 762P/785C strangles at $0.11 debit, max loss $1,914. Total worst case $5,114, or 4.999% of equity.',
    },
  },
]


function Day() {
  // The morning entry opens selected because it is the one window in the day with
  // a filled trade behind it, and an empty state here would waste the picture.
  const [chosen, setChosen] = useState<Window>(WINDOWS[0])
  const at = (hour: number) => ((hour - 9.5) / 6.5) * 100

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        a trading day as the file declares it — choose a window
      </figcaption>

      {/* The day scrolls in its own box on a narrow screen rather than pushing the
          card sideways: at 330px the last label sits past the right edge, and a
          page that scrolls horizontally as a whole is worse than a strip that
          does. */}
      <div className="overflow-x-auto pb-1">
        <div className="min-w-[420px]">
          <div className="relative mt-9 h-px bg-line-strong">
            {WINDOWS.map((window) => {
              const on = window === chosen
              return (
                <button
                  key={window.hour}
                  type="button"
                  onClick={() => setChosen(window)}
                  aria-pressed={on}
                  className="absolute top-0 -translate-x-1/2 cursor-pointer"
                  style={{ left: `${at(window.hour)}%` }}
                >
                  <span
                    className={`mx-auto block rounded-full transition-all ${
                      on ? 'size-3.5 ring-4' : 'size-2.5'
                    } ${
                      window.closes
                        ? 'bg-accent-ink ring-accent-ink/15'
                        : 'bg-accent ring-accent/20'
                    }`}
                    style={{ marginTop: on ? '-7px' : '-5px' }}
                  />
                  <span
                    className={`mt-3 block whitespace-nowrap font-mono text-[11px] transition-colors ${
                      on ? 'text-primary' : 'text-muted hover:text-secondary'
                    }`}
                  >
                    {window.name}
                  </span>
                </button>
              )
            })}
          </div>

          <div className="mt-11 flex justify-between font-mono text-[11px] text-muted">
            <span>09:30</span>
            <span>16:00</span>
          </div>

          {/* Pills, not bare text on a line. As plain text the one that used to be
              here was indistinguishable from the axis captions around it and
              nobody would guess it could be chosen. */}
          <div className="mt-8 border-t border-line pt-5">
            <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              and underneath, or on their own day
            </p>
            <div className="mt-3 flex flex-wrap gap-2">
              {OTHERS.map((window) => {
                const on = window === chosen
                return (
                  <button
                    key={window.name}
                    type="button"
                    onClick={() => setChosen(window)}
                    aria-pressed={on}
                    className={`cursor-pointer whitespace-nowrap rounded-full border px-3 py-1.5 font-mono text-[11px] transition-colors ${
                      on
                        ? 'border-accent bg-accent text-on-accent'
                        : 'border-line bg-surface-raised text-muted hover:border-line-strong hover:text-primary'
                    }`}
                  >
                    {window.name}
                  </button>
                )
              })}
            </div>
          </div>

          <p className="mt-5 text-[13px] leading-relaxed text-muted">
            Thirteen windows in one file. The session sets its own wake-ups on top of them — on a
            clock, or on a price it wants to hear about.
          </p>
        </div>
      </div>

      {/* What the chosen window is for, and what it once did. The box keeps a
          floor under its height so choosing another window does not make the page
          jump under the reader's hand. */}
      <div
        className={`mt-7 min-h-[164px] rounded-lg border border-l-[3px] border-line px-5 py-4 ${
          chosen.closes ? 'border-l-accent-ink' : 'border-l-accent'
        }`}
      >
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          what the file asks for
        </p>
        <p className="mt-2 text-[17px] leading-snug text-primary">{chosen.rule}</p>

        <p className="mt-5 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          what it said · {chosen.said.at}
        </p>
        <p className="mt-2 text-[15px] leading-relaxed text-secondary">
          {inline(chosen.said.text)}
        </p>

        {chosen.filed ? (
          <div className="mt-6 border-t border-line pt-5">
            <p className="flex flex-wrap items-center justify-between gap-3 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              and this is what it filed before the order existed
              <Chip tone="accent">limits read</Chip>
            </p>
            <dl className="m-0 mt-4 space-y-3">
              <div className="flex flex-col gap-1 sm:flex-row sm:gap-5">
                <dt className="shrink-0 font-mono text-[12px] text-muted sm:w-[86px]">structure</dt>
                <dd className="m-0 text-[14px] leading-relaxed text-primary">
                  {chosen.filed.structure}
                </dd>
              </div>
              <div className="flex flex-col gap-1 sm:flex-row sm:gap-5">
                <dt className="shrink-0 font-mono text-[12px] text-muted sm:w-[86px]">worst case</dt>
                <dd className="m-0 text-[14px] leading-relaxed text-primary">
                  {inline(chosen.filed.worst)}
                </dd>
              </div>
            </dl>
          </div>
        ) : null}
      </div>

    </figure>
  )
}

// What comes back when the session asks what it may do. The picture exists for
// its last row: a rule that admits it is there and withholds its number.
// The four playbooks: what each one DOES, in its own file's words, and the numbers
// it asks the declaration for.
//
// `read-my-envelope` was in this list and is not any more. It is a skill the agent
// is granted like the others, but it holds no technique - it goes and asks the
// risk engine what this account may lose - and a row for it inside a list of
// playbooks made the count wrong and the caption argue with itself.
//
// Opening one at a time rather than showing all four descriptions at once: the
// point of the picture is the COLUMN of demands, and four paragraphs under it bury
// that. The first is open so the pattern is visible without a click.
const PLAYBOOKS: [string, string, string][] = [
  [
    'premium-harvest',
    'short_leg_delta · min_edge_points · +2 more',
    'Sell a vertical credit spread and let time decay pay for it.',
  ],
  [
    'convexity',
    'convexity_short_leg_distance · convexity_valley_distance · +6 more',
    'Buy a backspread for near nothing, so the day the market moves hard is not a day this account only loses.',
  ],
  [
    'earnings-crush',
    'crush_worst_case_share · crush_implied_over_realized',
    'Sell the premium a company’s report has inflated — and only when the measurement says the market is paying more for the move than the company has historically made.',
  ],
  [
    'event-convexity',
    'event_bet_share · event_exit_by',
    'Buy the gap a scheduled number opens: entered the afternoon before, closed in the first hour after.',
  ],
]

function Playbooks() {
  const [open, setOpen] = useState(0)

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        four playbooks, and what each asks the declaration for
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-1 p-0">
        {PLAYBOOKS.map(([name, wants, does], index) => {
          const on = index === open
          return (
            <li
              key={name}
              className={`rounded-lg border border-l-[3px] px-4 py-3 transition-colors ${
                on ? 'border-line border-l-accent bg-surface-raised' : 'border-line border-l-line bg-surface-raised'
              }`}
            >
              <button
                type="button"
                onClick={() => setOpen(index)}
                aria-expanded={on}
                className="flex w-full cursor-pointer flex-col gap-1 text-left sm:flex-row sm:items-baseline sm:justify-between sm:gap-6"
              >
                <span
                  className={`text-[15px] transition-colors ${
                    on ? 'font-medium text-primary' : 'text-secondary'
                  }`}
                >
                  {name}
                </span>
                <span className="font-mono text-[12px] leading-relaxed text-secondary">{wants}</span>
              </button>
              {on ? (
                <p className="m-0 mt-3 text-[15px] leading-relaxed text-secondary">{does}</p>
              ) : null}
            </li>
          )
        })}
      </ul>
    </figure>
  )
}

function Funnel() {
  const stages: [string, string, number, boolean][] = [
    ['priced by code, ranked by one measure', '~600', 100, false],
    ['put in front of the session', '6', 12, true],
    ['taken', '0 or 1', 3, true],
  ]

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        one pass, every few minutes
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-4 p-0">
        {stages.map(([name, count, width, judged]) => (
          <li key={name}>
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <span className="text-[15px] text-secondary">{name}</span>
              <span className="font-mono text-[15px] tabular-nums text-primary">{count}</span>
            </div>
            <div className="mt-2 h-2 w-full rounded-full bg-surface-sunk">
              <div
                className={`h-2 rounded-full ${judged ? 'bg-accent' : 'bg-accent-ink'}`}
                style={{ width: `${width}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </figure>
  )
}

// THE REAL WALK, from `execution_steps`: the QQQ order of 31 August, four
// concessions of a cent each down to the floor the session named, and then a
// cancel because the book still would not take it.
//
// The version before this one had invented middle steps between two real ones,
// which is the worst kind of illustration: it sits beside true numbers and cannot
// be told from them. `showing` is what the book was offering at that moment, and
// it is the column that explains why a concession was made at all.
const WALK: [string, string, boolean][] = [
  ['−0.19 → −0.18', 'book showing −0.08', false],
  ['−0.18 → −0.17', 'book showing −0.17', false],
  ['−0.17 → −0.16', 'book showing −0.11', false],
  ['−0.16 → −0.15', 'the floor the session named', true],
]

function Ladder() {
  return (
    <figure className="m-0">
      <figcaption className="flex flex-wrap items-center justify-between gap-3 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        one order walked to its floor
        <span>31 august · QQQ 722 / 722.5</span>
      </figcaption>
      <ol className="m-0 mt-5 list-none space-y-1 p-0">
        {WALK.map(([step, note, floor]) => (
          <li
            key={step}
            className={`flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1 rounded-lg border px-4 py-3 ${
              floor ? 'border-accent bg-accent text-on-accent' : 'border-line bg-surface-raised'
            }`}
          >
            <span className={`font-mono text-[15px] tabular-nums ${floor ? 'font-medium' : 'text-primary'}`}>
              {step}
            </span>
            <span className={`font-mono text-[13px] ${floor ? '' : 'text-secondary'}`}>{note}</span>
          </li>
        ))}
        <li className="mt-1 rounded-lg border border-dashed border-line px-4 py-3 text-[15px] text-secondary">
          cancelled — the book still would not take it
        </li>
      </ol>
      {/* The diagram's last row already says the order was cancelled, so this line
          does not repeat it: it names the RULE behind that row and gives the one
          thing the picture cannot show - that the same mechanism also fills. */}
      <p className="mt-4 text-[15px] leading-relaxed text-secondary">
        What the book will not take is <Mark>cancelled, not conceded</Mark> — though another order
        the same day conceded a single cent and filled.
      </p>
    </figure>
  )
}

function Envelope() {
  const rules: [string, string, string][] = [
    ['per-position', '10 percent of equity', 'value'],
    ['one side of the market', '35 percent of equity', 'value'],
    ['whole portfolio', '80 percent of equity', 'value'],
    ['which expirations', '0 to 5 trading days', 'boundary'],
    ['how often it may open', 'a refusal on it is final', 'existence'],
  ]

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what the risk engine answers when asked, in every turn that acts
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-1 p-0">
        {rules.map(([rule, value, level]) => {
          const shown = level !== 'existence'
          return (
            <li
              key={rule}
              className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 rounded-lg border border-line bg-surface-raised px-4 py-3"
            >
              <span className="flex items-center gap-3 font-mono text-[13px] text-secondary">
                {shown ? (
                  <Eye className="size-4 shrink-0 text-muted" aria-hidden />
                ) : (
                  <EyeOff className="size-4 shrink-0 text-muted" aria-hidden />
                )}
                {rule}
              </span>
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span
                  className={`font-mono text-[13px] tabular-nums ${
                    shown ? 'text-primary' : 'text-muted'
                  }`}
                >
                  {value}
                </span>
                <Chip>{level}</Chip>
              </span>
            </li>
          )
        })}
      </ul>
    </figure>
  )
}

// The payoff of the spread the section describes. Level at the credit below the
// sold strike, level at the loss above the bought one, and a slope between. A
// reader with no options background sees in one look that BOTH ends are flat -
// which is what defined risk means and what the prose took five sentences to say.
// The payoff, coloured by what it MEANS rather than drawn in one line.
//
// Two things were wrong with the version before this. The line was a single black
// stroke, so profit and loss looked like one continuous thing when they are the
// two halves the picture exists to separate. And the horizontal dashes were the
// break-even level with nothing to say so - a reader saw a line and had to guess.
//
// Geometry: y=34 is the credit kept, y=172 the worst case, y=60 is zero. The
// payoff crosses zero at x=270, which is SPY 772.20 - the sold strike plus the
// 0.20 that came in. Green above that crossing, red below it, and the two shaded
// areas are the same claim as the line.
function Payoff() {
  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what the position is worth at expiry · 117 spreads
      </figcaption>
      <svg
        viewBox="0 0 640 200"
        className="mt-5 w-full"
        role="img"
        aria-label="The payoff of the spread: it keeps 2,340 dollars while SPY finishes below 772.20, and loses more the higher it finishes, down to 9,360 dollars above 773."
      >
        <g className="text-gain">
          <path d="M0,34 L240,34 L270,60 L0,60 Z" fill="currentColor" opacity="0.12" />
          <polyline
            points="0,34 240,34 270,60"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinejoin="round"
          />
        </g>

        <g className="text-loss">
          <path d="M270,60 L400,172 L640,172 L640,60 Z" fill="currentColor" opacity="0.1" />
          <polyline
            points="270,60 400,172 640,172"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinejoin="round"
          />
        </g>

        <g className="text-line-strong">
          <line x1="240" y1="16" x2="240" y2="184" stroke="currentColor" strokeWidth="1" />
          <line x1="400" y1="16" x2="400" y2="184" stroke="currentColor" strokeWidth="1" />
          <line x1="0" y1="60" x2="640" y2="60" stroke="currentColor" strokeDasharray="3 4" />
        </g>

        {/* The crossing is MARKED here and NAMED below, in HTML. A label inside the
            drawing scales with the drawing: at 640 units across it measured 14px on
            a desktop and 4px on a phone, which is not a label. */}
        <circle cx="270" cy="60" r="3.5" className="text-muted" fill="currentColor" />
      </svg>
      <div className="mt-4 flex flex-wrap justify-between gap-x-6 gap-y-1 font-mono text-[11px] text-muted">
        <span className="text-gain">keeps $2,340</span>
        <span>772 · sold</span>
        <span>773 · bought</span>
        <span className="text-loss">loses $9,360</span>
      </div>
      {/* Only what the drawing cannot say. Green above and red below is what the
          drawing IS, and the dot needs no naming once the dashes have one. */}
      <p className="mt-4 text-[15px] leading-relaxed text-secondary">
        The dashes are break even: SPY at 772.20, the strike it sold plus the 0.20 that came in.
      </p>
    </figure>
  )
}

// The three exits on the 672 trades the history holds. Bars rather than a table:
// the whole point is that the middle one is SHORTER than doing nothing, and a
// column of dollar signs makes the reader work that out for himself.
// The stand, and why it is not a backtest. Every row is from `docs/write-up.md`.
//
// The last row is the property the whole instrument rests on and is marked as
// such: if the overlay did not equal the live market at zero displacement, nothing
// it showed at any other displacement would mean anything.
const STAND: [string, string, boolean][] = [
  ['every quote', 'from the live broker, now', false],
  ['one number', 'displaced along a curve', false],
  ['every contract', 'repriced from that move, by its own live volatility', false],
  ['at zero displacement', 'equal to the live market, to the cent', true],
]

function Stand() {
  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        not a backtest · what the agent is shown
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-1 p-0">
        {STAND.map(([name, what, rests]) => (
          <li
            key={name}
            className={`flex flex-col gap-1 rounded-lg border border-l-[3px] px-4 py-3 sm:flex-row sm:items-baseline sm:justify-between sm:gap-6 ${
              rests ? 'border-line border-l-accent bg-surface-raised' : 'border-line border-l-line bg-surface-raised'
            }`}
          >
            <span className="font-mono text-[13px] text-muted">{name}</span>
            <span className={`text-[15px] ${rests ? 'font-medium text-primary' : 'text-secondary'}`}>
              {what}
            </span>
          </li>
        ))}
      </ul>
    </figure>
  )
}

function Exits() {
  const most = 3.46

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what a trade paid · 672 trades, january 2024 to august 2026
      </figcaption>
      <ul className="m-0 mt-6 list-none space-y-5 p-0">
        {EXITS.map(([rule, value, removed]) => (
          <li key={rule}>
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <span className={`text-[15px] ${removed ? 'text-primary' : 'text-secondary'}`}>
                {rule}
                {removed ? (
                  <span className="ml-3 font-mono text-[13px] text-loss">← removed</span>
                ) : null}
              </span>
              <span className="font-mono text-[15px] tabular-nums text-primary">
                ${value.toFixed(2)}
              </span>
            </div>
            <div className="mt-2 h-2 w-full rounded-full bg-surface-sunk">
              <div
                className={`h-2 rounded-full ${removed ? 'bg-loss' : 'bg-accent-ink'}`}
                style={{ width: `${(value / most) * 100}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </figure>
  )
}

const EXITS: [string, number, boolean][] = [
  ['hold to expiry', 2.94, false],
  ['close when the sold strike is touched', 2.32, true],
  ['close a full width past the sold strike', 3.46, false],
]


// A faithful excerpt of the submitted declaration, not a paraphrase of it. The
// sample here once quoted the DEFENCE window - a rule about counting legs, which
// is housekeeping - and it said `every: 15m` where the file says 30m, with a cause
// of its own invention. This is an ENTRY window instead, because it carries the
// section's claim in the file's own words: even the condition for taking a trade
// is a judgement to be made, not a number to be met. The `...` marks where the
// task is cut; nothing inside the quoted lines is altered.
const SCHEDULE = `- name: entry-morning
  cause: "morning entry window, taken only when premium is dear"
  at: "10:20"
  within: 45m
  days: [mon, tue, wed, thu]
  task: |
    ...
    A morning entry is not taken every day: a whole day of
    movement lies ahead, so the premium has to be dear by a
    measure visible in the chain itself.`


// ============================================================
// The page's own parts.
// ============================================================

// The landing's section, which is not the live page's section.
//
// `Section` in parts.tsx titles with an Eyebrow - monospace, 12px, muted - and
// that is right where a title labels a panel of numbers the reader is already
// looking at. On a page somebody is scanning, a 12px caption is not a heading:
// the eye finds nothing to stop at and the page reads as one grey column.
//
// This is deliberately NOT a variant of that part. The two are different things
// wearing the same word: a data panel takes a caption, an argument takes a
// heading. Giving one component both jobs is how it ends up with a property for
// each.
function Block({
  label,
  title,
  explains,
  children,
}: {
  label: string
  title: string
  explains?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="mb-20">
      <Eyebrow>{label}</Eyebrow>
      <h2 className="mt-3 max-w-[24ch] text-balance text-[32px] font-medium leading-[1.1] tracking-[-0.02em] text-primary sm:text-[38px]">
        {title}
      </h2>
      {explains ? (
        <p className="mt-4 max-w-[68ch] text-[17px] leading-[1.55] text-secondary">{explains}</p>
      ) : null}
      <div className="mt-7">
        <Boundary says="this section failed">{children}</Boundary>
      </div>
    </section>
  )
}

// One measurement: who takes it, when, by what rule, and the number if that
// moment has passed. A cut-off still ahead shows a dash rather than a figure -
// the alternative is a number the reader cannot tell from a settled one.
// The measurement that is IN. One place picks it, so the plaque on the first
// screen and the section at the foot cannot disagree, and when the second
// measurement lands nothing here needs editing.
const SETTLED = MEASUREMENTS.filter((m) => m.equity !== null).slice(-1)[0] ?? MEASUREMENTS[0]

// TWO buttons, not four. The bar above already carries GitHub, and a hero that
// repeats a link the reader can see six inches higher is spending its most
// valuable row on nothing. The deck is not written yet, and a button for a page
// that does not exist is worse than no button.
//
// The first is filled because it is the one thing a judge cannot get from any
// other page: the account moving while they watch. The second is outlined
// because it orients rather than persuades.
// Used at the top of the page and again at the foot of it, so it is written once.
function Actions() {
  return (
    <div className="mt-9 flex flex-wrap items-center gap-3">
      <LiveLink />
      <Link
        to="/submission"
        className="inline-flex items-center rounded-lg border border-line-strong px-5 py-2.5 text-[15px] font-medium text-primary transition-colors hover:bg-surface-sunk"
      >
        For judges
      </Link>
    </div>
  )
}

function Measured({ of }: { of: Measurement }) {
  const settled = of.equity !== null
  const profit = settled ? (of.equity as number) - OPENED : 0

  return (
    <li className="rounded-lg border border-line bg-surface-raised px-5 py-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="text-[17px] font-medium text-primary">{of.by}</span>
          <span className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
            {of.when}
          </span>
        </span>
        <Chip tone={settled ? 'gain' : undefined}>{settled ? 'settled' : 'not yet taken'}</Chip>
      </div>

      {settled ? (
        <p className="mt-3 flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <span className="text-[32px] font-medium leading-none tracking-[-0.02em] text-primary tabular-nums">
            {money(of.equity as number)}
          </span>
          <span className="text-[17px] font-medium tabular-nums text-gain">
            {change(profit)} · {((profit / OPENED) * 100).toFixed(2)}%
          </span>
        </p>
      ) : (
        <p className="mt-3 text-[32px] font-medium leading-none text-muted">&mdash;</p>
      )}

      <p className="mt-3 text-[15px] leading-relaxed text-secondary">{of.rule}</p>
    </li>
  )
}

// The line a section is remembered by, at the end of the card that earned it.
function Pull({ children }: { children: ReactNode }) {
  return (
    <p className="mt-7 border-l-[3px] border-mark pl-4 text-[19px] font-medium leading-[1.4] tracking-[-0.01em] text-primary">
      {children}
    </p>
  )
}

function Claim({ icon: Icon, title, says }: { icon: typeof Eye; title: string; says: string }) {
  return (
    <Card>
      <Icon className="size-5 text-accent" aria-hidden />
      <h2 className="mt-4 text-[22px] font-medium leading-tight tracking-[-0.01em] text-primary">
        {title}
      </h2>
      <p className="mt-2.5 text-[15px] leading-relaxed text-secondary">{says}</p>
    </Card>
  )
}
