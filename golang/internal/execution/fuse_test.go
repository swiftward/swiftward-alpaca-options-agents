package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// A closing order sent as one mleg: both legs give a position back.
func closingSpread(id string, limit float64, submitted time.Time) marketdata.Order {
	return marketdata.Order{
		ID: id, Class: "mleg", Status: "new", LimitPrice: limit, SubmittedAt: &submitted,
		ClientID: NameFor(0.5),
		Legs: []marketdata.Order{
			{Symbol: "QQQ260826P00701000", Side: "buy", Quantity: 1, PositionIntent: "buy_to_close"},
			{Symbol: "QQQ260826P00700000", Side: "sell", Quantity: 1, PositionIntent: "sell_to_close"},
		},
	}
}

// The day's fuse is the session's own rule, and until 29 August nothing enforced
// it: the playbook said in its own words that skipping the check is not caught.
// Here it is a backstop in the last place our code holds an order.
func TestABlownFuseCancelsWhatWouldOpenAndSparesWhatCloses(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{
			spread("opens", -0.12, "new", at.Add(-2*time.Minute)),
			closingSpread("closes", 0.40, at.Add(-2*time.Minute)),
		},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Fuse = func(context.Context) (bool, string, error) {
		return true, "down 3.4% from yesterday's close of 100000, and the fuse is 3%", nil
	}
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"opens"}, cancelled,
		"only the opening order is cancelled: a day bad enough to blow the fuse is a "+
			"day to stop adding risk, not one to stop giving it back")
}

// An unreadable fuse cancels nothing. Losing a limit is a reason to speak, never
// a reason to take an account's working orders away - the same rule the ceiling
// and the book checks follow.
func TestAFuseThatCannotBeReadCancelsNothing(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Fuse = func(context.Context) (bool, string, error) {
		return false, "", errors.New("the broker gives no equity for yesterday's close")
	}
	l.step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, cancelled)
	assert.Contains(t, replaced, "o-1", "the order goes on walking, as it would with no fuse at all")
}

// A day that has not fallen far enough changes nothing at all.
func TestAnIntactFuseLeavesTheWalkAlone(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Fuse = func(context.Context) (bool, string, error) { return false, "", nil }
	l.step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, cancelled)
	assert.InDelta(t, -0.11, replaced["o-1"], 1e-9)
}
