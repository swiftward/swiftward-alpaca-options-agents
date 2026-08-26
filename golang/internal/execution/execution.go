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
	Quotes(ctx context.Context, symbols []string) (map[string]marketdata.Quote, error)
	ReplaceOrder(ctx context.Context, id string, limit float64, name string) error
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

	for _, order := range orders {
		if order.Status == "filled" || order.FilledQuantity > 0 {
			l.report(ctx, order)
		}
		if !working(order) {
			continue
		}
		if hasCeiling && l.tooBig(ctx, order, ceiling) {
			continue
		}
		age := l.Now().Sub(*order.SubmittedAt)
		if age < l.Every {
			continue
		}
		if age > l.Patience {
			if err := l.Broker.CancelOrder(ctx, order.ID); err != nil {
				l.Log.Error("could not cancel an order the book would not take",
					zap.String("order", order.ID), zap.Error(err))
				continue
			}
			l.Log.Info("cancelled an order the book would not take",
				zap.String("order", order.ID), zap.Duration("waited", age))
			l.wroteDown(ctx, record.ExecutionStep{
				OrderRef: order.ID, At: l.Now(), Action: "cancelled", Was: order.LimitPrice,
			})
			cancelled = append(cancelled, order)
			continue
		}

		if err := l.walk(ctx, order); err != nil {
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
func (l *Ladder) report(ctx context.Context, order marketdata.Order) {
	if l.Record == nil {
		return
	}

	price, quantity := order.FilledPrice, order.FilledQuantity
	first, err := l.Record.NoteFill(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: l.Now(), Action: "filled",
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
	kind, strikes := "", make([]string, 0, len(order.Legs))
	underlying := ""
	for _, leg := range order.Legs {
		contract, known := marketdata.ContractFrom(leg.Symbol)
		if !known {
			// Nothing is guessed: a wrong strike in the room is worse than none.
			return fmt.Sprintf("✔ исполнено %.0f по %.2f", order.FilledQuantity, order.FilledPrice)
		}
		if underlying == "" {
			underlying = leg.Symbol[:len(leg.Symbol)-15]
			kind = contract.Type
		}
		strikes = append(strikes, strconv.FormatFloat(contract.Strike, 'f', -1, 64))
	}

	money := "кредит"
	price := order.FilledPrice
	if price > 0 {
		money = "дебет"
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
	lines = append(lines, "Заявки, которые ты отправил, сняты по терпению: стакан не взял их "+
		"по худшей цене, которую ты назвал. Твоё решение выполнено не так, как ты его принял.")
	for _, order := range orders {
		legs := make([]string, 0, len(order.Legs))
		for _, leg := range order.Legs {
			legs = append(legs, leg.Side+" "+leg.Symbol)
		}
		// Part of an order can fill before the rest is cancelled. Saying "did not
		// fill" there would send the session to re-open a position it already holds.
		got := "не исполнилась вовсе"
		if order.FilledQuantity > 0 {
			got = fmt.Sprintf("исполнилась частично: %.0f из %.0f", order.FilledQuantity, order.Quantity)
		}
		lines = append(lines, fmt.Sprintf("- %s по цене %.2f, %s: %s",
			order.ID, order.LimitPrice, got, strings.Join(legs, ", ")))
	}
	lines = append(lines, "Реши, что с этим делать: повторить по другой цене, взять другую "+
		"конструкцию или оставить как есть. Позиции, заявки и капитал прочитай сам - "+
		"здесь названы только те заявки, что не прошли.")

	return strings.Join(lines, "\n")
}

// walk moves one order one step toward what the book is showing.
func (l *Ladder) walk(ctx context.Context, order marketdata.Order) error {
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

	target := showing
	if worseThan(showing, floor) {
		target = floor
	}

	next := Toward(order.LimitPrice, target, l.Step)
	if next == order.LimitPrice {
		l.Log.Info("left an order alone: it already stands at the price it walks toward",
			zap.String("order", order.ID), zap.Float64("limit", order.LimitPrice),
			zap.Float64("target", target))
		return nil
	}

	// The floor travels with the order: a replacement the broker names itself would
	// drop it, and the next step would find nothing to obey. The name is rebuilt
	// rather than copied, because the broker refuses a name it has already seen.
	if err := l.Broker.ReplaceOrder(ctx, order.ID, next, NameCarrying(floor, l.Now())); err != nil {
		return err
	}
	l.Log.Info("walked an order toward the book",
		zap.String("order", order.ID),
		zap.Float64("was", order.LimitPrice),
		zap.Float64("now", next),
		zap.Float64("showing", showing),
		zap.Float64("floor", floor))
	l.wroteDown(ctx, record.ExecutionStep{
		OrderRef: order.ID, At: l.Now(), Action: "walked",
		Was: order.LimitPrice, Became: &next, Showing: &showing, Floor: &floor,
	})

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
	switch order.Status {
	case "new", "accepted", "pending_new", "partially_filled":
		return true
	default:
		return false
	}
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
		ratio := leg.Quantity
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

// tooBig cancels a resting order that would lose more than one position may, and
// says so both in the log and to the session that sent it.
//
// It reports whether the order was taken out of the ladder's hands, so a
// cancelled order is not then walked toward a book it must never reach.
//
// An order it cannot read is LEFT ALONE rather than cancelled: unknown is not
// the same as too large, and cancelling on a symbol this code failed to parse
// would take out a sound structure for a reason that is ours.
func (l *Ladder) tooBig(ctx context.Context, order marketdata.Order, ceiling float64) bool {
	worst, known := WorstCase(order)
	if !known || -worst <= ceiling {
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
		OrderRef: order.ID, At: l.Now(), Action: "cancelled", Was: order.LimitPrice,
	})

	if l.Wake != nil {
		l.Wake(ctx, fmt.Sprintf(
			"заявка %s снята: её худший исход %.0f при разрешённых %.0f на позицию. "+
				"Пересчитай размер от предела конверта, а не от покупательной способности, "+
				"и скажи одной строкой, сколько насчитал.",
			order.ID, -worst, ceiling))
	}

	return true
}
