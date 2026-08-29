package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

type brokerDouble struct {
	orders []marketdata.Order
	asked  int
}

func (b *brokerDouble) Orders(context.Context, int) ([]marketdata.Order, error) {
	b.asked++
	return b.orders, nil
}

type keeperDouble struct {
	unknown  []record.InFlightOrder
	resolved map[string]string
}

func (k *keeperDouble) OrdersLeftUnknown(context.Context, time.Time) ([]record.InFlightOrder, error) {
	return k.unknown, nil
}

func (k *keeperDouble) OrderResolved(_ context.Context, callRef, answer string) error {
	if k.resolved == nil {
		k.resolved = map[string]string{}
	}
	k.resolved[callRef] = answer
	return nil
}

var since = time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

// The dangerous case: the process died after the broker took the order. Left
// unknown, the next session cannot tell whether it holds the position, and a
// judge reading the record cannot either.
func TestAnOrderThatDidReachTheBrokerIsSaidSo(t *testing.T) {
	kept := &keeperDouble{unknown: []record.InFlightOrder{
		{CallRef: "call-1", Name: "worst=-0.46;MU955-960-183143"},
	}}
	broker := &brokerDouble{orders: []marketdata.Order{{
		ID: "order-9", ClientID: "worst=-0.46;MU955-960-183143", Status: "filled",
		Quantity: 5, FilledQuantity: 5, FilledPrice: -0.46,
		Legs: []marketdata.Order{{Side: "sell", Symbol: "MU260826C00955000"}},
	}}}

	settled, err := Ask(context.Background(), broker, kept, since, zaptest.NewLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 1, settled)
	assert.Contains(t, kept.resolved["call-1"], "order-9")
	assert.Contains(t, kept.resolved["call-1"], "filled")
}

// Absent from a full recent listing is an answer too, and leaving it as a
// question invites the next session to re-send what was never sent.
func TestAnOrderThatNeverLandedIsSaidSo(t *testing.T) {
	kept := &keeperDouble{unknown: []record.InFlightOrder{
		{CallRef: "call-2", Name: "worst=-0.11;QQQ703-702"},
	}}

	settled, err := Ask(context.Background(), &brokerDouble{}, kept, since, zaptest.NewLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 1, settled)
	assert.Contains(t, kept.resolved["call-2"], "did not reach")
}

// Without a name there is nothing to ask by, and matching on time and symbol
// would answer confidently and sometimes wrongly. It stays unknown.
func TestAnOrderWithoutANameStaysUnknown(t *testing.T) {
	kept := &keeperDouble{unknown: []record.InFlightOrder{{CallRef: "call-3"}}}

	settled, err := Ask(context.Background(), &brokerDouble{}, kept, since, zaptest.NewLogger(t))
	require.NoError(t, err)
	assert.Zero(t, settled)
	assert.Empty(t, kept.resolved, "unknown is the honest answer when nothing can be asked")
}

// Nothing unknown means the broker is not asked at all: a restart in the quiet
// should cost no request.
func TestNothingUnknownAsksTheBrokerNothing(t *testing.T) {
	broker := &brokerDouble{}

	settled, err := Ask(context.Background(), broker, &keeperDouble{}, since, zaptest.NewLogger(t))
	require.NoError(t, err)
	assert.Zero(t, settled)
	assert.Zero(t, broker.asked)
}
