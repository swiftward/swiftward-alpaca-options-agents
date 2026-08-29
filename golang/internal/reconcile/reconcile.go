// Package reconcile answers, after a crash, the one question the record cannot:
// did the order actually reach the broker?
//
// A process that dies between sending an order and reading the answer leaves the
// call marked `unknown`. That is the honest thing to write down and the wrong
// thing to leave written: for an order, "we did not send it" and "we do not know"
// are different facts, and the broker knows which. Every order this project sends
// carries a name of its own, so the question is askable.
package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// Broker is the reading half this needs.
type Broker interface {
	Orders(ctx context.Context, limit int) ([]marketdata.Order, error)
}

// Keeper is the record's side of the question.
type Keeper interface {
	OrdersLeftUnknown(ctx context.Context, since time.Time) ([]record.InFlightOrder, error)
	OrderResolved(ctx context.Context, callRef, answer string) error
}

// Reads bounds how many recent orders are examined at the broker.
const Reads = 100

// Ask resolves what a dead process left unknown, and reports how many it
// settled. An order whose name is found at the broker did reach it; one absent
// from a full recent listing did not, and both are written down as facts rather
// than left as a question.
func Ask(ctx context.Context, broker Broker, kept Keeper, since time.Time, log *zap.Logger) (int, error) {
	unknown, err := kept.OrdersLeftUnknown(ctx, since)
	if err != nil {
		return 0, err
	}
	if len(unknown) == 0 {
		return 0, nil
	}

	orders, err := broker.Orders(ctx, Reads)
	if err != nil {
		return 0, fmt.Errorf("ask the broker what it received: %w", err)
	}

	atBroker := map[string]marketdata.Order{}
	for _, order := range orders {
		if order.ClientID != "" {
			atBroker[order.ClientID] = order
		}
	}

	settled := 0
	for _, one := range unknown {
		if one.Name == "" {
			// Nothing to ask by. Saying so beats matching on time and symbol,
			// which would answer confidently and sometimes wrongly.
			log.Warn("an order left unknown carried no name, so the broker cannot be asked about it",
				zap.String("call", one.CallRef), zap.Time("sent", one.StartedAt))
			continue
		}

		order, found := atBroker[one.Name]
		if !found {
			// A name absent from a full recent listing is a request that never
			// landed. That is an answer, and it belongs in the record.
			if err := kept.OrderResolved(ctx, one.CallRef,
				"reconciled after a restart: the broker has no order under this name, so the request did not reach it"); err != nil {
				return settled, err
			}
			log.Info("an order left unknown never reached the broker",
				zap.String("call", one.CallRef), zap.String("name", one.Name))
			settled++
			continue
		}

		legs := make([]string, 0, len(order.Legs))
		for _, leg := range order.Legs {
			legs = append(legs, leg.Side+" "+leg.Symbol)
		}
		if err := kept.OrderResolved(ctx, one.CallRef, fmt.Sprintf(
			"reconciled after a restart: the broker has it as %s, status %s, filled %.0f of %.0f at %.2f (%s)",
			order.ID, order.Status, order.FilledQuantity, order.Quantity, order.FilledPrice,
			strings.Join(legs, ", "))); err != nil {
			return settled, err
		}
		log.Warn("an order left unknown DID reach the broker",
			zap.String("call", one.CallRef), zap.String("name", one.Name),
			zap.String("order", order.ID), zap.String("status", order.Status))
		settled++
	}

	return settled, nil
}
