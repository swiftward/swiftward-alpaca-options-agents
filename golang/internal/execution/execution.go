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
}

// Broker is what the ladder needs: what is in the book, what the legs are worth,
// and the two verbs that move an order.
type Broker interface {
	Orders(ctx context.Context, limit int) ([]marketdata.Order, error)
	Quotes(ctx context.Context, symbols []string) (map[string]marketdata.Quote, error)
	ReplaceOrder(ctx context.Context, id string, limit float64) error
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
	// Reads bounds how many recent orders are examined.
	Reads int
	Now   func() time.Time
	Log   *zap.Logger
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

	for _, order := range orders {
		if !working(order) {
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
			continue
		}

		if err := l.walk(ctx, order); err != nil {
			l.Log.Error("could not walk an order's price",
				zap.String("order", order.ID), zap.Error(err))
		}
	}
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
		return nil
	}

	target := showing
	if worseThan(showing, floor) {
		target = floor
	}

	next := Toward(order.LimitPrice, target, l.Step)
	if next == order.LimitPrice {
		return nil
	}

	if err := l.Broker.ReplaceOrder(ctx, order.ID, next); err != nil {
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
