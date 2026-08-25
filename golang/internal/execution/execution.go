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
)

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
	// cancelled: a structure the book will not take at its own showing price is
	// not a structure worth chasing further.
	Patience time.Duration
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

	next := Toward(order.LimitPrice, showing, l.Step)
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
		zap.Float64("showing", showing))

	return nil
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

// Toward moves a limit one step in the direction of showing, and stops there
// rather than passing it: the book's own price is the worst this ever asks for.
func Toward(limit, showing, step float64) float64 {
	if math.Abs(showing-limit) < step/2 {
		return round(showing)
	}
	if showing > limit {
		return round(math.Min(limit+step, showing))
	}

	return round(math.Max(limit-step, showing))
}

// round keeps prices at the cent the broker quotes in; without it a walk
// accumulates a fraction the broker will refuse.
func round(price float64) float64 {
	return math.Round(price*100) / 100
}
