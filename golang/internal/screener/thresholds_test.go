package screener

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Thresholds that cannot be read offer NOTHING.
//
// The dangerous reading is the other one, and it is the reason this is a test
// rather than a comment: a threshold quietly defaulting to zero does not narrow
// the list, it opens it. Every structure clears a floor of zero, so the session
// would be handed the whole book as though it had qualified - and the log would
// call the pass successful.
func TestAPassWhoseThresholdsCannotBeReadOffersNothing(t *testing.T) {
	broker := &brokerDouble{prices: map[string]float64{"QQQ": 700}}
	sweep := &Sweep{
		Broker: broker, Universe: []string{"QQQ"},
		Thresholds: func() (Wanted, error) { return Wanted{}, errors.New("the declaration names no screener_nearest") },
		Every:      time.Minute, PerMinute: 1000, Now: time.Now, Log: zap.NewNop(),
	}

	found, _ := sweep.look(context.Background())

	assert.Empty(t, found, "a pass that does not know what it is looking for finds nothing")
	assert.Zero(t, broker.calls, "and does not spend the broker's rate limit finding it")
}

// The thresholds are read on EVERY pass, not captured when the sweep starts:
// that is the whole reason they moved into the declaration, where an edit
// reaches a running process.
func TestTheThresholdsAreReadOnEveryPass(t *testing.T) {
	broker := &brokerDouble{prices: map[string]float64{"QQQ": 700}}
	asked := 0
	sweep := &Sweep{
		Broker: broker, Universe: []string{"QQQ"},
		Thresholds: func() (Wanted, error) { asked++; return anything(), nil },
		Every:      time.Minute, PerMinute: 1000, Now: time.Now, Log: zap.NewNop(),
	}

	sweep.look(context.Background())
	sweep.look(context.Background())

	require.Equal(t, 2, asked, "one reading per pass, not one for the life of the process")
}
