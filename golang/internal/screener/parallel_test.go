package screener

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// slowBroker answers slowly and remembers how many answers it was giving at
// once. Slowly on purpose: the whole point of workers is that the wait for one
// answer is spent waiting for the next, and a broker that answers instantly
// cannot show that.
type slowBroker struct {
	took     time.Duration
	mu       sync.Mutex
	inFlight int
	most     int
	asked    map[string]int
}

func (b *slowBroker) MarketOpen(context.Context) (bool, error) { return true, nil }

func (b *slowBroker) LastTrades(_ context.Context, symbols []string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, symbol := range symbols {
		out[symbol] = 100
	}

	return out, nil
}

func (b *slowBroker) Chain(_ context.Context, underlying string, _, _ float64,
	_ time.Time, _ int) ([]marketdata.Contract, map[string]marketdata.Quote, error) {

	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.most {
		b.most = b.inFlight
	}
	b.asked[underlying]++
	b.mu.Unlock()

	time.Sleep(b.took)

	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()

	return nil, nil, nil
}

type countingKeeper struct{ kept atomic.Int64 }

func (k *countingKeeper) ReplaceCandidates(context.Context, time.Time, []Candidate) error {
	k.kept.Add(1)
	return nil
}

// The sweep asks about several underlyings at once, and asks about each of them
// exactly once. Measured against the policy gateway on 27 August, one chain
// answer takes about two and a third seconds; asked one at a time, two hundred
// and eighty four names take eleven minutes and spend twenty six requests a
// minute out of the hundred and eighty allowed. What should stop the sweep is
// the broker's limit, not our own loop.
func TestTheSweepAsksAboutSeveralNamesAtOnce(t *testing.T) {
	universe := make([]string, 24)
	for i := range universe {
		universe[i] = fmt.Sprintf("N%02d", i)
	}

	broker := &slowBroker{took: 30 * time.Millisecond, asked: map[string]int{}}
	sweep := &Sweep{
		Broker: broker, Universe: universe, Wanted: anything(),
		Every: time.Minute, Record: &countingKeeper{}, PerMinute: 10_000,
		Expirations: 5, Workers: 6,
		Now: time.Now, Log: zaptest.NewLogger(t),
	}

	sweep.once(context.Background())

	broker.mu.Lock()
	defer broker.mu.Unlock()

	require.Len(t, broker.asked, len(universe), "every name is still asked about")
	for name, times := range broker.asked {
		require.Equal(t, 1, times, "%s must be asked about once, not %d times", name, times)
	}
	require.Greater(t, broker.most, 1,
		"the whole point is more than one answer in flight; got %d at most", broker.most)
	require.LessOrEqual(t, broker.most, 6, "and never more than the workers allowed")
}

// One worker is what a deployment that never sets the field gets, and it must
// behave exactly as the sweep did before workers existed.
func TestWithoutWorkersTheSweepAsksOneAtATime(t *testing.T) {
	broker := &slowBroker{took: 5 * time.Millisecond, asked: map[string]int{}}
	sweep := &Sweep{
		Broker: broker, Universe: []string{"AAA", "BBB", "CCC"}, Wanted: anything(),
		Every: time.Minute, Record: &countingKeeper{}, PerMinute: 10_000,
		Expirations: 5,
		Now: time.Now, Log: zaptest.NewLogger(t),
	}

	sweep.once(context.Background())

	broker.mu.Lock()
	defer broker.mu.Unlock()

	require.Equal(t, 1, broker.most, "unset Workers must mean one at a time")
	require.Len(t, broker.asked, 3)
}
