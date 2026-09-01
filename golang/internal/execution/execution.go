// Package execution gets a decided order filled at the best price it can.
//
// The session decides what to sell, how large, and the worst price it accepts.
// That is judgement, and it belongs to the model. Walking a limit price a cent
// at a time until the book takes it is not judgement: it is arithmetic on a
// clock, it has to happen in seconds, and a turn of the agent costs a minute and
// a half. So the session places the order at the price it wants, and this walks
// it - never past the price the market is actually showing, and never past the
// session's own limit.
//
// Nothing here decides to trade. It cannot open a position, only finish one the
// session already asked for.
package execution

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// Keeper is where each move is written down. Nil means the ladder's work lives
// only in the log, which a redeployment throws away - and the question the log
// answers, what walking the price saved, is asked afterwards.
type Keeper interface {
	AppendExecutionStep(ctx context.Context, step record.ExecutionStep) error
	// NoteFill writes a fill down once and answers whether this call wrote it.
	NoteFill(ctx context.Context, step record.ExecutionStep) (bool, error)
}

// Broker is what the ladder needs: what is in the book, what the legs are worth,
// and the two verbs that move an order.
type Broker interface {
	Orders(ctx context.Context, limit int) ([]marketdata.Order, error)
	Positions(ctx context.Context) ([]marketdata.Position, error)
	Quotes(ctx context.Context, symbols []string) (map[string]marketdata.Quote, error)
	ReplaceOrder(ctx context.Context, id string, limit float64, name string) (string, error)
	CancelOrder(ctx context.Context, id string) error
}

// Ladder walks unfilled multi-leg orders toward the price that fills them.
type Ladder struct {
	Broker Broker
	// Every is how often the ladder looks, and how long an order rests at each
	// price before the next step.
	Every time.Duration
	// Step is one move of the limit price, in dollars per share. A tick on these
	// contracts is a cent.
	Step float64
	// Stride chooses how far one step travels: StrideByTick or StrideToArrive.
	// Empty is StrideToArrive.
	Stride string
	// Patience is how long an order may live unfilled. Past it the order is
	// cancelled: a structure the book will not take at the worst price its session
	// allowed is not a structure worth chasing further.
	Patience time.Duration
	// Record keeps every move. Nil records nothing.
	Record Keeper
	// Wake is how the ladder tells the session that a decision of its own was not
	// carried out. It is called only for orders cancelled unfilled, never for
	// fills: a fill is what the session already planned for, and a turn spent
	// acknowledging it changes nothing. Nil tells nobody.
	Wake func(ctx context.Context, cause string)
	// Say puts one line in front of the person watching. Fills go here and
	// nowhere else: the session already knows what it asked for, and a turn spent
	// acknowledging a fill changes nothing - but a fill is the only thing on the
	// whole screen that actually happened. Nil says nothing.
	Say func(ctx context.Context, line string)
	// Ceiling answers what one position may lose, in dollars, given what the
	// account is worth. It is the SAME limit the session is told to size by, read
	// from the same place - the ladder is where that limit stops being advice.
	//
	// The session works the limit out and can work it out wrong: on 26 August one
	// sized a structure to 906 sets and a maximum loss near 76 000 against a limit
	// of 15 000, after the broker had refused a first attempt at 17 884. Nothing
	// between it and the market disagreed, because the envelope discloses and does
	// not enforce. This is what does.
	//
	// Nil leaves resting orders unchecked, which is the behaviour there was before.
	Ceiling func(ctx context.Context) (float64, error)
	// Book answers what EVERYTHING open may lose together, in dollars. The
	// per-position limit says nothing about how many positions there are: twenty
	// structures each inside their own ceiling can still put the whole account at
	// risk, and on 26 August the portfolio limit was the one number nothing
	// enforced.
	//
	// A resting order is judged against what is already held plus what the orders
	// before it in this pass would add. Nil leaves the book unchecked.
	Book func(ctx context.Context) (float64, error)
	// Fuse answers whether today is over: the account has fallen from yesterday's
	// close by at least the share the declaration names. `said` is the sentence
	// the session is told, because a cancellation it cannot explain is worse than
	// none.
	//
	// It exists because until 29 August the fuse was a line in a playbook and
	// nothing else. The playbook said so itself - "nothing refuses the order if
	// you skip this check, so skipping it is not caught, it is simply a day the
	// account keeps losing" - and on 27 August one account refused an entry at
	// 14:01 and took one at 14:18 on the same two figures.
	//
	// It only ever CANCELS, and only orders that open. A day that has fallen far
	// enough to blow the fuse is a day to stop adding risk, not one to stop
	// giving it back.
	Fuse func(ctx context.Context) (over bool, said string, err error)
	// MinEdgePoints answers the least a structure may pay above what it must
	// survive, in percentage points, from the declaration in force - the same
	// number the session enters on. The ladder holds the WORST PRICE to it: an
	// order whose name states an edge below this one is left where it stands
	// rather than walked, because walking it would buy a structure the session
	// itself had already called unfit.
	//
	// An order that states no edge is walked as before, and said once. The
	// annotation is the session's claim, and a claim nobody made is not a claim
	// this can judge; refusing those would stop an account trading over a missing
	// word. Nil leaves worst prices unchecked.
	MinEdgePoints func() (float64, error)
	// Reads bounds how many recent orders are examined.
	Reads int
	Now   func() time.Time
	Log   *zap.Logger

	// watching is when this ladder started looking. A fill older than that is
	// history: it is written down, because the record is worth having, and it is
	// NOT said, because the room is for what just happened. Without this every
	// redeployment reads the day's fills back into the room as news - which is
	// exactly what one did on 25 August, eighteen lines at once.
	watching time.Time

	// passes counts this ladder's own passes. It is what "has the order rested a
	// tick" is answered with, because the clock cannot answer it: see orderAge.
	passes int64

	// ages remembers, for the order id the broker is showing NOW, when the order
	// was first placed and when it last moved. Both were read from the order's own
	// submitted_at until 31 August, and a replacement is a NEW order with a NEW
	// submission time, so both restarted on every step.
	//
	// What that cost: the freshness check found the order younger than the
	// interval on the tick after each step and skipped it, so an order got a step
	// every OTHER tick. Measured on the live market that day, chaining
	// `execution_steps` through `replaced_by` on both judged accounts, the median
	// interval between one order's steps was 90 seconds against a configured 45 -
	// five offers to the book across eight minutes of patience instead of ten. And
	// patience, read from the same field, measured how long the order had stood
	// STILL rather than how long it had gone unfilled, so an order that kept
	// stepping never aged out at all.
	//
	// It is memory rather than record because the ladder must work with Record
	// nil. What a restart loses is the chain's history, and the fallback is the
	// order's own submitted_at - the behaviour there was before, and it errs
	// toward giving the order more time rather than less.
	ages map[string]orderAge
}

// orderAge is the life of one order across the replacements that carry it:
// WHEN it was first placed, and on WHICH pass this ladder last moved it.
//
// The second is a pass number and not a time, and that is the point. "Has it
// rested a full interval" was asked by comparing two clock readings against the
// interval itself, and the two are equal by construction - the ticker fires every
// `Every` and the readings are one tick apart - so the answer was decided by
// however many microseconds the scheduler added to each pass. A teammate's arena
// measured the result: 45.002, 45.000, 89.999, 90.001. Half the passes skipped, at
// random rather than always, which is worse than always because it looks fixed.
//
// A tolerance would have hidden it. Counting passes removes the clock from the
// question: two passes cannot be closer together than the ticker's period, so an
// order this ladder moved on an EARLIER pass has rested its interval by
// construction. Only an order we have never moved has to be timed at all, and
// only against the moment the session placed it.
type orderAge struct {
	placed time.Time
	// movedOn is the pass this ladder last moved the order on. Zero is never.
	movedOn int64
}

// lifeOf is when this order's chain was placed and when it last moved. An order
// this ladder has not seen before is taken at the broker's word: its own
// submission time is both.
func (l *Ladder) lifeOf(order marketdata.Order, now time.Time) orderAge {
	if known, found := l.ages[order.ID]; found {
		return known
	}
	life := orderAge{placed: now}
	if order.SubmittedAt != nil {
		life = orderAge{placed: *order.SubmittedAt}
	}
	if l.ages == nil {
		l.ages = map[string]orderAge{}
	}
	l.ages[order.ID] = life

	return life
}

// carried moves a chain's life onto the id the replacement was given. The old id
// is gone from the broker, so keeping it would only grow the map.
func (l *Ladder) carried(from, to string) {
	life := l.ages[from]
	delete(l.ages, from)
	if to == "" {
		// A broker that answered without an id leaves nothing to carry the life
		// onto; the next pass takes the order at its word again.
		return
	}
	life.movedOn = l.passes
	if l.ages == nil {
		l.ages = map[string]orderAge{}
	}
	l.ages[to] = life
}

// forgetWhatIsDone drops the chains of orders that are over, so a day of filled
// and cancelled orders does not accumulate here.
//
// It drops on a POSITIVE signal only: an order the broker shows as no longer
// working, or one older than any order of ours can still be. Dropping on absence
// from the list would be wrong, and the reviewer caught it: the broker read is
// bounded by Reads and returns the newest, so a working order can be missing from
// one pass. Its chain would be rebuilt from the replacement's own submission time
// on the next pass - which restarts patience, and an order whose patience keeps
// restarting is never cancelled and holds its underlying out of the entry list
// for as long as it lives.
func (l *Ladder) forgetWhatIsDone(shown []marketdata.Order, now time.Time) {
	alive := make(map[string]bool, len(shown))
	for _, order := range shown {
		if working(order) {
			alive[order.ID] = true
			continue
		}
		delete(l.ages, order.ID)
	}

	// An order the broker is SHOWING as working keeps its chain however old it is.
	//
	// The age sweep alone made an order immortal, and a teammate's arena caught it
	// on the live shape: placed 09:02:28, patience out at 09:10:28, fifteen steps,
	// no cancellation, filled at 09:21:37 - nineteen minutes against eight. The
	// window in which an order can be cancelled is one interval wide, from
	// `Patience` to `Patience + Every`. Miss one pass inside it and the chain was
	// swept; the next pass rebuilt it from the order's own submitted_at, which is
	// FRESH because every step is a replacement, so patience began again. An order
	// that never dies also holds its underlying out of the entry list for as long
	// as it lives.
	//
	// So age decides nothing on its own. It only reaps what we can no longer see -
	// the broker's answer is bounded, and an order outside that bound leaves a
	// chain nothing would otherwise remove.
	stale := now.Add(-(l.Patience + l.Every))
	for id, life := range l.ages {
		if !alive[id] && life.placed.Before(stale) {
			delete(l.ages, id)
		}
	}
}

// Run walks orders until ctx ends.
func (l *Ladder) Run(ctx context.Context) error {
	switch {
	case l.Broker == nil:
		return fmt.Errorf("the execution ladder has no broker")
	case l.Every <= 0:
		return fmt.Errorf("the execution ladder needs how often to step: set EXECUTION_EVERY")
	case l.Step <= 0:
		return fmt.Errorf("the execution ladder needs how far one step moves: set EXECUTION_STEP")
	case l.Patience <= 0:
		return fmt.Errorf("the execution ladder needs how long to wait: set EXECUTION_PATIENCE")
	case l.Now == nil:
		return fmt.Errorf("the execution ladder has no clock")
	}
	if l.Reads <= 0 {
		l.Reads = defaultReads
	}
	l.watching = l.Now()

	ticker := time.NewTicker(l.Every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.step(ctx)
		}
	}
}

// defaultReads is how far back the ladder looks for its own working orders. A
// day of this agent's orders fits well inside it.
const defaultReads = 50

func (l *Ladder) step(ctx context.Context) {
	// ONE moment for the whole pass, and the cadence depends on it.
	//
	// The ticker fires every `Every` and an order may step when it has rested that
	// long. Reading the clock again while the pass runs makes "now" a few
	// milliseconds LATER than the tick that started it, so the interval measured
	// on the next tick is `Every` minus however long this pass took - strictly
	// less, every time, and the order is skipped. That is a step every OTHER tick,
	// which is what the record showed: 90 seconds against a configured 45.
	//
	// Fixed once already by moving where the timestamp came from, which changed
	// nothing, because the flaw was never the source: it was reading the clock
	// twice. A teammate's arena measured the same 90 seconds on the fixed code and
	// said so.
	now := l.Now()
	l.passes++

	orders, err := l.Broker.Orders(ctx, l.Reads)
	if err != nil {
		l.Log.Error("could not read the orders", zap.Error(err))
		return
	}

	var cancelled []marketdata.Order
	// What one position may lose, asked once for the whole pass rather than per
	// order: it is the same answer for all of them, and asking is a request.
	ceiling, hasCeiling := 0.0, false
	if l.Ceiling != nil {
		if limit, err := l.Ceiling(ctx); err != nil {
			l.Log.Error("could not read what one position may lose; resting orders go unchecked",
				zap.Error(err))
		} else {
			ceiling, hasCeiling = limit, true
		}
	}

	// What the whole book may lose, and what it already risks. Both asked once for
	// the pass: the answer is the same for every order in it, and asking costs a
	// request.
	book, atRisk, hasBook := 0.0, 0.0, false
	if l.Book != nil {
		limit, err := l.Book(ctx)
		if err != nil {
			l.Log.Error("could not read what the whole book may lose; resting orders go unchecked against it",
				zap.Error(err))
		} else if positions, err := l.Broker.Positions(ctx); err != nil {
			l.Log.Error("could not read what is already open; resting orders go unchecked against the book",
				zap.Error(err))
		} else {
			book, atRisk, hasBook = limit, marketdata.AtRisk(positions), true
			// A book that already holds a position whose loss has no floor is
			// FULL, whatever the arithmetic says: there is no number to compare a
			// ceiling with. The cage refuses to open one, so this is what the
			// market leaves behind - a long leg assigned early, or expired while
			// the short one lived. Adding to that book is the one move that
			// cannot be right, and closing orders are exempt from this check as
			// from the others.
			if without := marketdata.WithoutAFloor(positions); len(without) > 0 {
				l.Log.Warn("the book holds a position whose loss has no floor; no new exposure is added",
					zap.Strings("series", without))
				atRisk = math.Inf(1)
			}
		}
	}

	// Whether the day is over, asked once for the pass like the two limits above.
	// An unreadable answer cancels nothing: losing the fuse is a reason to speak,
	// never a reason to take an account's working orders away.
	fused, fuseSaid := false, ""
	if l.Fuse != nil {
		over, said, err := l.Fuse(ctx)
		switch {
		case err != nil:
			l.Log.Error("could not tell whether the day's fuse has blown; resting orders go unchecked against it",
				zap.Error(err))
		case over:
			fused, fuseSaid = true, said
		}
	}

	l.forgetWhatIsDone(orders, now)

	for _, order := range orders {
		if order.Status == "filled" || order.FilledQuantity > 0 {
			l.report(ctx, order, now)
		}
		if !working(order) {
			continue
		}
		// A closing order gives exposure back, so neither size check applies to
		// it: both price an order as if it were new, and the one that reduces the
		// book would be cancelled for the size of the position it is undoing.
		closing := OnlyCloses(order)
		if fused && !closing {
			if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
				l.Log.Error("could not cancel an order the day's fuse refuses",
					zap.String("order", order.ID), zap.Error(err))
				continue
			}
			delete(l.ages, order.ID)
			l.Log.Warn("cancelled an opening order: the day's fuse has blown",
				zap.String("order", order.ID), zap.String("said", fuseSaid))
			l.wroteDown(ctx, record.ExecutionStep{
				OrderRef: order.ID, At: now, Action: "cancelled", Was: order.LimitPrice,
			})
			cancelled = append(cancelled, order)
			continue
		}
		if !closing && l.unbounded(ctx, order, now) {
			continue
		}
		if hasCeiling && !closing && l.tooBig(ctx, order, ceiling, now) {
			continue
		}
		if hasBook && !closing {
			if worst, known := WorstCase(order); known {
				// The same allowance the per-position check needs, for the same
				// reason: equity moves with every tick, the session cannot express
				// a position finer than one set, and cancelling for less than that
				// punishes correct arithmetic - then does it again on the retry,
				// spending the entry window and taking no position.
				resting := order.Quantity - order.FilledQuantity
				over := atRisk - worst - book
				// One set is the allowance, and it is only an allowance while
				// there is a set to give up: an order of ONE set that breaches is
				// judged in full, because the session cannot take less of it and
				// the rounding this forgives does not exist there.
				if over > 0 && (resting <= 1 || over >= -worst/resting) {
					l.overBook(ctx, order, atRisk, -worst, book, now)
					continue
				}
				// Orders in one pass add up. Judged against the book as it stands,
				// ten of them would pass as "the last that fits".
				atRisk -= worst
			}
		}
		life := l.lifeOf(order, now)
		// Moved on an earlier pass: the passes are the ticker's own interval apart,
		// so it has rested one by construction. Never moved: it is the session's
		// own order and it rests one interval before this touches it.
		if life.movedOn == 0 && now.Sub(life.placed) < l.Every {
			continue
		}
		if life.movedOn == l.passes {
			continue
		}
		age := now.Sub(life.placed)
		if age > l.Patience {
			if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
				l.Log.Error("could not cancel an order the book would not take",
					zap.String("order", order.ID), zap.Error(err))
				continue
			}
			delete(l.ages, order.ID)
			l.Log.Info("cancelled an order the book would not take",
				zap.String("order", order.ID), zap.Duration("waited", age))
			l.wroteDown(ctx, record.ExecutionStep{
				OrderRef: order.ID, At: now, Action: "cancelled", Was: order.LimitPrice,
			})
			cancelled = append(cancelled, order)
			continue
		}

		if err := l.walk(ctx, order, now); err != nil {
			l.Log.Error("could not walk an order's price",
				zap.String("order", order.ID), zap.Error(err))
		}
	}

	// One pass, one telling. Several orders dying together is one situation for
	// the session to answer, not three.
	if len(cancelled) > 0 && l.Wake != nil {
		l.Wake(ctx, whatDidNotHappen(cancelled))
	}
}

// report says a fill once. What makes it once is the record, not this process:
// the ladder meets the same filled order on every pass and forgets everything it
// held in memory when it restarts.
func (l *Ladder) report(ctx context.Context, order marketdata.Order, now time.Time) {
	if l.Record == nil {
		return
	}

	price, quantity := order.FilledPrice, order.FilledQuantity
	first, err := l.Record.NoteFill(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: now, Action: "filled",
		Was: order.LimitPrice, Became: &price, Quantity: &quantity,
	})
	if err != nil {
		l.Log.Error("could not write a fill down",
			zap.String("order", order.ID), zap.Error(err))
		return
	}
	if !first || l.Say == nil {
		return
	}
	if order.FilledAt == nil || !order.FilledAt.After(l.watching) {
		return
	}

	l.Say(ctx, whatFilled(order))
}

// whatFilled is a fill in one line, in the words a person reads: the underlying
// and the strikes, not the fifteen characters the industry names a contract by.
func whatFilled(order marketdata.Order) string {
	// An order can arrive with no legs at all - a listing asked without them
	// answers that way - and the loop below would then leave every part of the
	// line empty, printing a fill with no instrument. Measured 28 August: three
	// such lines in one minute, each reading "filled 33, debit 1.26" with nothing
	// to say WHAT was closed.
	if len(order.Legs) == 0 {
		return fmt.Sprintf("✔ filled %.0f at %.2f", order.FilledQuantity, order.FilledPrice)
	}

	kind, strikes := "", make([]string, 0, len(order.Legs))
	underlying := ""
	for _, leg := range order.Legs {
		contract, known := marketdata.ContractFrom(leg.Symbol)
		if !known {
			// Nothing is guessed: a wrong strike in the room is worse than none.
			return fmt.Sprintf("✔ filled %.0f at %.2f", order.FilledQuantity, order.FilledPrice)
		}
		if underlying == "" {
			underlying = leg.Symbol[:len(leg.Symbol)-15]
			kind = contract.Type
		}
		strikes = append(strikes, strconv.FormatFloat(contract.Strike, 'f', -1, 64))
	}

	money := "credit"
	price := order.FilledPrice
	if price > 0 {
		money = "debit"
	}
	if price < 0 {
		price = -price
	}

	return fmt.Sprintf("✔ %s %s %s ×%.0f, %s %.2f",
		underlying, strings.Join(strikes, "/"), kind, order.FilledQuantity, money, price)
}

// whatDidNotHappen is what the session is told when its orders were cancelled
// unfilled. It names the structures rather than summarising them: the session
// has to decide what to do instead, and it cannot decide from a count.
func whatDidNotHappen(orders []marketdata.Order) string {
	lines := make([]string, 0, len(orders)+2)
	lines = append(lines, "The orders you sent were cancelled on patience: the book would not "+
		"take them at the worst price you named. Your decision was not carried out as you made it.")
	for _, order := range orders {
		legs := make([]string, 0, len(order.Legs))
		for _, leg := range order.Legs {
			legs = append(legs, leg.Side+" "+leg.Symbol)
		}
		// Part of an order can fill before the rest is cancelled. Saying "did not
		// fill" there would send the session to re-open a position it already holds.
		got := "did not fill at all"
		if order.FilledQuantity > 0 {
			got = fmt.Sprintf("filled in part: %.0f of %.0f", order.FilledQuantity, order.Quantity)
		}
		lines = append(lines, fmt.Sprintf("- %s at %.2f, %s: %s",
			order.ID, order.LimitPrice, got, strings.Join(legs, ", ")))
	}
	lines = append(lines, "Decide what to do about it: try again at another price, take a "+
		"different structure, or leave it. Read the positions, the orders and the equity "+
		"yourself - only the orders that did not go through are named here.")

	return strings.Join(lines, "\n")
}

// walk moves one order one step toward what the book is showing.
// How far one step of the ladder moves the limit price. Declared per agent so an
// execution tactic with no live evidence behind it runs on ONE account while the
// other keeps the one we have been running - the account submitted is whichever
// stands higher, so the comparison costs nothing and answers by Wednesday.
const (
	// StrideByTick moves one tick a step, whatever the distance. It is what this
	// ladder did from the beginning; the table in docs/execution.md is what it
	// produced.
	StrideByTick = "tick"
	// StrideToArrive divides the distance still to travel by the steps left
	// before patience ends, so the walk arrives while the offer still stands.
	StrideToArrive = "arrive"
)

// stride is how far one step moves the limit: the distance still to travel,
// divided by the steps left before patience ends. Never less than one tick.
//
// A fixed tick per step is a walk that loses ground. Measured on the live market
// on 31 August: at each step the median distance between our price and the book
// was ten cents on one account and six on the other, and following one order
// through its whole life, the distance GREW from 0.18 to 0.32 across nine steps
// while the ladder conceded one cent at each. It conceded nine cents and ended
// further from a fill than it started, then died on patience. The book moves
// several cents in an interval; a cent an interval cannot follow it.
//
// Dividing what is left by the intervals that are left makes the walk arrive by
// the time patience ends, whatever the book does in between, and it needs no
// number of its own - patience and the interval are both already declared. The
// floor is untouched and still bounds everything: `target` never passes the worst
// price the session accepted, so a faster walk gives up no more in the end than a
// slower one, it just gets there while the offer still stands.
func (l *Ladder) stride(order marketdata.Order, target float64, now time.Time) float64 {
	if l.Stride == StrideByTick {
		return l.Step
	}
	distance := math.Abs(target - order.LimitPrice)
	left := l.Patience - now.Sub(l.lifeOf(order, now).placed)
	steps := int(left / l.Every)
	if steps < 1 {
		steps = 1
	}
	if stride := distance / float64(steps); stride > l.Step {
		return stride
	}

	return l.Step
}

func (l *Ladder) walk(ctx context.Context, order marketdata.Order, now time.Time) error {
	symbols := make([]string, 0, len(order.Legs))
	for _, leg := range order.Legs {
		symbols = append(symbols, leg.Symbol)
	}

	quotes, err := l.Broker.Quotes(ctx, symbols)
	if err != nil {
		return err
	}

	showing, known := Showing(order, quotes)
	if !known {
		// A leg without a two-sided quote has no price to walk toward. Leaving the
		// order where it stands is the honest answer; patience will end it.
		l.Log.Info("left an order alone: a leg has no two-sided quote",
			zap.String("order", order.ID), zap.Float64("limit", order.LimitPrice))
		return nil
	}

	// The ladder only ever concedes. A book already standing better than our own
	// limit is either about to fill or resting on a quote nobody will trade, and
	// asking for MORE credit answers neither - it walks a marketable order away
	// from the fill it was about to get.
	if !worseThan(showing, order.LimitPrice) {
		l.Log.Info("left an order alone: the book already stands better than our price",
			zap.String("order", order.ID), zap.Float64("limit", order.LimitPrice),
			zap.Float64("showing", showing))
		return nil
	}

	// Two bounds, and only one of them is about safety. The floor is the session's
	// decision and is never crossed. The book is only about not paying more than
	// the moment requires: if it stands closer than the floor, stop at the book
	// and keep the difference.
	floor, named := Reservation(order)
	if !named {
		// Nobody said how much of this credit may be given up, and this is not the
		// place to decide it. The order keeps the price it was placed at, and
		// patience ends it if the book never comes.
		l.Log.Info("left an order alone: its name carries no worst price",
			zap.String("order", order.ID), zap.String("name", order.ClientID))
		return nil
	}

	if l.refusesTheFloor(order, floor, quotes) {
		return nil
	}

	target := showing
	if worseThan(showing, floor) {
		target = floor
	}

	next := Toward(order.LimitPrice, target, l.stride(order, target, now))
	if next == order.LimitPrice {
		l.Log.Info("left an order alone: it already stands at the price it walks toward",
			zap.String("order", order.ID), zap.Float64("limit", order.LimitPrice),
			zap.Float64("target", target))
		return nil
	}

	// The floor travels with the order: a replacement the broker names itself would
	// drop it, and the next step would find nothing to obey. The name is rebuilt
	// rather than copied, because the broker refuses a name it has already seen.
	replacement, err := l.Broker.ReplaceOrder(ctx, order.ID, next, NameCarrying(order, now))
	if err != nil {
		return err
	}
	l.carried(order.ID, replacement)
	l.Log.Info("walked an order toward the book",
		zap.String("order", order.ID),
		zap.String("became", replacement),
		zap.Float64("was", order.LimitPrice),
		zap.Float64("now", next),
		zap.Float64("showing", showing),
		zap.Float64("floor", floor))
	step := record.ExecutionStep{
		OrderRef: order.ID, At: now, Action: "walked",
		Was: order.LimitPrice, Became: &next, Showing: &showing, Floor: &floor,
	}
	// Absent rather than empty: a broker that answered without an id leaves the
	// chain broken, and an empty string would read as a link to nothing.
	if replacement != "" {
		step.ReplacedBy = &replacement
	}
	l.wroteDown(ctx, step)

	return nil
}

// wroteDown keeps a move. A record that cannot be written is said out loud and
// does not stop the walk: the order matters more than the note about it.
func (l *Ladder) wroteDown(ctx context.Context, step record.ExecutionStep) {
	if l.Record == nil {
		return
	}
	if err := l.Record.AppendExecutionStep(ctx, step); err != nil {
		l.Log.Error("could not record an execution step",
			zap.String("order", step.OrderRef), zap.Error(err))
	}
}

// working reports whether this is one of our structures still waiting in the
// book. A single-leg order is not ours: every structure this project opens is
// sent as one multi-leg order.
func working(order marketdata.Order) bool {
	if order.Class != "mleg" || len(order.Legs) == 0 || order.SubmittedAt == nil {
		return false
	}
	return order.Active()
}

// Showing is the price the book is actually offering for this structure right
// now: every leg sold at its bid, every leg bought at its ask. A credit is
// negative, the way the broker states it.
func Showing(order marketdata.Order, quotes map[string]marketdata.Quote) (float64, bool) {
	total := 0.0
	for _, leg := range order.Legs {
		quote, answered := quotes[leg.Symbol]
		if !answered || quote.Bid <= 0 || quote.Ask <= 0 {
			return 0, false
		}
		// The RATIO, not the quantity. They differ by the number of sets, and the
		// order's limit price is named per set: a backspread of two sets carries
		// qty 2 and 4 on its legs while its ratio is 1 and 2. Pricing it by
		// quantity doubles the answer, and a doubled credit reads as a book
		// standing better than our own price - so the ladder concedes nothing and
		// waits out its whole patience. Measured on the fill of 27 August: by
		// ratio -1.93 + 2*0.62 = -0.69, exactly the limit that filled; by
		// quantity -1.38.
		//
		// Where the broker names no ratio the leg is the set, and its quantity is
		// the ratio.
		ratio := leg.Ratio
		if ratio <= 0 {
			ratio = leg.Quantity
		}
		if ratio <= 0 {
			ratio = 1
		}
		if leg.Side == "sell" {
			total -= quote.Bid * ratio
		} else {
			total += quote.Ask * ratio
		}
	}

	return round(total), true
}

// Toward moves a limit one step in the direction of target, and stops there
// rather than passing it.
func Toward(limit, target, step float64) float64 {
	if math.Abs(target-limit) < step/2 {
		return round(target)
	}
	if target > limit {
		return round(math.Min(limit+step, target))
	}

	return round(math.Max(limit-step, target))
}

// worseThan reports whether one price gives up more than another. A credit is
// negative and a debit positive, so "worse" is the same direction for both: the
// larger number.
func worseThan(price, than float64) bool { return price > than }

// round keeps prices at the cent the broker quotes in; without it a walk
// accumulates a fraction the broker will refuse.
func round(price float64) float64 {
	return math.Round(price*100) / 100
}

// overBook cancels a resting order that would take the whole account past what it
// may have at risk at once, and tells the session why in the words it needs to
// act on: the book is full, not this order too large.
func (l *Ladder) overBook(ctx context.Context, order marketdata.Order, held, adds, allowed float64, now time.Time) {
	l.Log.Warn("cancelling a resting order that would take the book past its limit",
		zap.String("order", order.ID),
		zap.Float64("already_at_risk", held),
		zap.Float64("this_order_adds", adds),
		zap.Float64("allowed", allowed))

	if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
		l.Log.Error("could not cancel an order that takes the book past its limit",
			zap.String("order", order.ID), zap.Error(err))
		return
	}
	l.wroteDown(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: now, Action: "cancelled", Was: order.LimitPrice,
	})

	if l.Wake != nil {
		l.Wake(ctx, fmt.Sprintf(
			"order %s cancelled: what is open already risks %.0f, this would add %.0f, "+
				"and the whole account is allowed %.0f. There is no room left - close "+
				"something or take less, and say in one line how much is left.",
			order.ID, held, adds, allowed))
	}
}

// unbounded cancels a resting order whose loss has no floor, and says so.
//
// No ceiling is needed to judge this one: a structure left net short calls loses
// more the higher the underlying goes, so there is no number it fits under. It is
// checked before the ceiling because the ceiling would otherwise compare against
// a sampled figure that means nothing here.
func (l *Ladder) unbounded(ctx context.Context, order marketdata.Order, now time.Time) bool {
	if !Unbounded(order) {
		return false
	}

	l.Log.Warn("cancelling a resting order whose loss has no floor",
		zap.String("order", order.ID),
		zap.String("why", "the structure is left net short calls, so the loss grows with the price"))

	if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
		l.Log.Error("could not cancel an order with unbounded risk",
			zap.String("order", order.ID), zap.Error(err))

		return false
	}
	l.wroteDown(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: now, Action: "cancelled", Was: order.LimitPrice,
	})

	if l.Wake != nil {
		l.Wake(ctx, fmt.Sprintf(
			"order %s cancelled: it leaves more calls sold than bought, so its loss has no "+
				"limit at any price. Buy at least as many calls as you sell, and say in one "+
				"line what you changed.", order.ID))
	}

	return true
}

// tooBig cancels a resting order that would lose more than one position may, and
// says so both in the log and to the session that sent it.
//
// It reports whether the order was taken out of the ladder's hands, so a
// cancelled order is not then walked toward a book it must never reach.
//
// An order it cannot read is LEFT ALONE rather than cancelled: unknown is not
// the same as too large, and cancelling on a symbol this code failed to parse
// would take out a sound structure for a reason that is ours.
func (l *Ladder) tooBig(ctx context.Context, order marketdata.Order, ceiling float64, now time.Time) bool {
	worst, known := WorstCase(order)
	if !known || -worst <= ceiling {
		return false
	}

	// A breach smaller than ONE SET is not a sizing error.
	//
	// The limit is a share of equity, and equity moves with every tick of the
	// open book - including this order's own mark once it fills. So the number
	// the session sized against and the number read here are never quite the
	// same. Meanwhile the session cannot express a position finer than one set:
	// having taken the largest whole number that fits, it has already sized as
	// accurately as the instrument allows.
	//
	// Cancelling for less than that punishes correct arithmetic and does it
	// again on every retry - a loop that spends every entry window and takes no
	// position. Seen on 26 August: 518 sets refused for 12 dollars and 34 cents
	// on a limit of 15 009, where one set was worth 29.
	// And only while a set is what could be given up. At one set the allowance
	// below equals the whole worst case, so every single-set order passed
	// whatever it risked: 20,000 dollars against a ceiling of 8,000 read as a
	// rounding error. One set that breaches cannot be made smaller, so it is
	// judged in full.
	// STRICTLY smaller than one set. At exactly one set the breach is a whole set
	// the session could have left out, which is a sizing error and not the
	// rounding this forgives: two sets risking the ceiling each passed a ceiling
	// they doubled.
	resting := order.Quantity - order.FilledQuantity
	if resting > 1 && -worst-ceiling < -worst/resting {
		return false
	}

	l.Log.Warn("cancelling a resting order that may lose more than one position is allowed to",
		zap.String("order", order.ID),
		zap.Float64("worst_case", -worst),
		zap.Float64("allowed", ceiling),
		zap.Float64("quantity", order.Quantity))

	if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
		l.Log.Error("could not cancel an order that is too large", zap.String("order", order.ID), zap.Error(err))
		return false
	}
	l.wroteDown(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: now, Action: "cancelled", Was: order.LimitPrice,
	})

	if l.Wake != nil {
		l.Wake(ctx, fmt.Sprintf(
			"order %s cancelled: its worst case is %.0f against the %.0f allowed on one "+
				"position. Work the size out from the envelope's ceiling, not from buying "+
				"power, and say in one line what you worked out.",
			order.ID, -worst, ceiling))
	}

	return true
}

// refusesTheFloor answers whether a fill at the worst price this order names
// would pay less above its risk than the declaration demands.
//
// The ladder walks toward the floor and stops there, so the floor is the price
// this order can actually be filled at. On 1 September a session entered on
// "edge at least +3" and named a floor whose edge was +2.53; the ladder walked to
// it in forty-five seconds and was saved only by a book eight cents away.
//
// The number is COMPUTED here, from the quotes this pass has already read, and
// not taken from anything the session wrote. A number written at placement is
// what the market said then; by the time the ladder would concede to it, the
// delta has moved. This is also why nothing new has to be written into an order's
// name: the ladder holds the whole structure already - the legs carry the strikes
// and their quotes carry the delta.
//
// It never cancels. The price the order was PLACED at cleared the rule, so the
// order is still worth leaving in the book; only the concession is refused.
func (l *Ladder) refusesTheFloor(order marketdata.Order, floor float64, quotes map[string]marketdata.Quote) bool {
	if l.MinEdgePoints == nil {
		return false
	}
	// Only an order that plainly OPENS is held to an entry rule. An exit has no
	// edge to measure, and an unlabelled leg falls the same way: a rule that can
	// cost a fill judges nothing it is unsure of.
	if !OnlyOpens(order) {
		return false
	}
	edge, measurable := EdgeAt(order, floor, quotes)
	if !measurable {
		return false
	}
	least, err := l.MinEdgePoints()
	if err != nil {
		// Losing the number is a reason to speak, never a reason to stop walking an
		// order the session placed within the rules it could read at the time.
		l.Log.Error("could not read the least edge a structure may pay; worst prices go unchecked",
			zap.Error(err))

		return false
	}
	if edge >= least {
		return false
	}
	l.Log.Warn("left an order alone: at its worst price it pays less than the entry rule demands",
		zap.String("order", order.ID),
		zap.Float64("floor", floor),
		zap.Float64("edge_at_floor", edge),
		zap.Float64("min_edge_points", least))

	return true
}

// EdgeAt is what a fill at the given price would pay above what the structure
// must survive, in percentage points of the width - the same measure the screener
// ranks by, so a session and the ladder cannot mean different things by it.
//
// Only a two-legged structure at one set per leg is measured. A backspread is a
// different shape whose risk is not the width between two strikes, and the answer
// for it is that there is no answer: false, and the caller judges nothing.
func EdgeAt(order marketdata.Order, price float64, quotes map[string]marketdata.Quote) (float64, bool) {
	if len(order.Legs) != 2 || price >= 0 {
		return 0, false
	}
	var short, long marketdata.Order
	for _, leg := range order.Legs {
		ratio := leg.Ratio
		if ratio <= 0 {
			ratio = leg.Quantity
		}
		if ratio != 1 {
			return 0, false
		}
		if leg.Side == "sell" {
			short = leg
		} else {
			long = leg
		}
	}
	if short.Symbol == "" || long.Symbol == "" {
		return 0, false
	}

	sold, soldKnown := marketdata.ContractFrom(short.Symbol)
	bought, boughtKnown := marketdata.ContractFrom(long.Symbol)
	if !soldKnown || !boughtKnown {
		return 0, false
	}
	width := math.Abs(sold.Strike - bought.Strike)
	if width <= 0 {
		return 0, false
	}

	quote, answered := quotes[short.Symbol]
	if !answered || quote.Delta == nil {
		return 0, false
	}

	return 100*math.Abs(price)/width - 100*math.Abs(*quote.Delta), true
}
