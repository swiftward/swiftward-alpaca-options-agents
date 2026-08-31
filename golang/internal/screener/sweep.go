package screener

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// Broker is the reading half this needs. It cannot place an order, and that is
// deliberate: a component that scans hundreds of names must not be able to act
// on what it finds.
type Broker interface {
	MarketOpen(ctx context.Context) (bool, error)
	LastTrades(ctx context.Context, symbols []string) (map[string]float64, error)
	// Chain brings one underlying's contracts and their prices back together, in
	// a single request. Listing then quoting was two, and the pair was the whole
	// cost of a sweep.
	Chain(ctx context.Context, underlying string, low, high float64, until time.Time, most int) ([]marketdata.Contract, map[string]marketdata.Quote, error)
}

// Keeper is where a sweep's findings are left for the session to read and for
// a later run to measure against.
type Keeper interface {
	RecordCandidates(ctx context.Context, at time.Time, found []Candidate) error
	PurgeCandidates(ctx context.Context, before time.Time) (int64, error)
}

// pricesPerCall is how many underlyings one price request carries. Measured
// against the running server, which answered nine symbols in one call.
const pricesPerCall = 20

// Sweep prices every underlying it is given, over and over, and leaves the best
// of them where a session can read them.
//
// It is bounded by the broker's rate limit, not by ambition: the free plan
// allows 200 requests a minute, and one underlying costs two of them - the
// contracts around its price, then the quotes for those contracts. Prices come
// in batches and cost almost nothing. So a few hundred names fit in a few
// minutes, and the limit that matters is which names have options anyone trades.
type Sweep struct {
	Broker   Broker
	Universe []string
	// Thresholds answers what a structure must clear to be offered at all, and is
	// asked once per PASS rather than held as a value. Every number in it is a
	// trading decision - how far the sold strike sits, how likely it is to be
	// reached, what the structure must pay - so it lives in the declaration
	// beside the rest of them, and a declaration is re-read while this process
	// runs. Until 29 August they were seven environment variables, which put the
	// screener's trading choices in a different place from every other one and
	// meant changing any of them was a redeployment.
	Thresholds func() (Wanted, error)
	Every      time.Duration
	Record     Keeper
	// Keep is how long a sweep's findings stay readable after the sweep that
	// replaced them. They are the only record of what the option book offered,
	// because the broker publishes no history of two-sided option quotes.
	Keep time.Duration
	// PerMinute is the broker's limit on requests. A sweep never exceeds it.
	PerMinute int
	// Expirations bounds how far out to look, in days.
	Expirations int
	// Workers is how many underlyings are asked about at once. One is the old
	// behaviour and the reason this field exists: measured against the policy
	// gateway on 27 August, one chain answer takes about two and a third seconds
	// - SPY 3.4, AAPL 2.1, XLF 2.0, and half of that is a megabyte of payload
	// rather than the connection. Asked one at a time, two hundred and eighty
	// four names take eleven minutes and spend twenty six requests a minute out
	// of the hundred and eighty the broker allows. The limiter, not the loop,
	// should be what stops us.
	//
	// Zero means one, so a deployment that never sets it behaves as before.
	Workers int
	Now     func() time.Time
	Log     *zap.Logger

	// spent and since are the limiter's own counters and are touched by every
	// worker, so they are under a lock. Everything else here is read-only once
	// the sweep starts.
	rate  sync.Mutex
	spent int
	since time.Time
}

func (s *Sweep) Run(ctx context.Context) error {
	switch {
	case s.Broker == nil:
		return fmt.Errorf("the screener has no broker")
	case len(s.Universe) == 0:
		return fmt.Errorf("the screener has nothing to look at: set SCREENER_UNDERLYINGS")
	case s.Every <= 0:
		return fmt.Errorf("the screener needs how often to sweep: set SCREENER_EVERY")
	case s.PerMinute <= 0:
		return fmt.Errorf("the screener needs the broker's rate limit: set SCREENER_PER_MINUTE")
	case s.Now == nil:
		return fmt.Errorf("the screener has no clock")
	case s.Thresholds == nil:
		return fmt.Errorf("the screener is not told what a structure must clear")
	case s.Record != nil && s.Keep <= 0:
		return fmt.Errorf("the screener needs how long to keep what it finds: set SCREENER_KEEP")
	}

	ticker := time.NewTicker(s.Every)
	defer ticker.Stop()

	s.once(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.once(ctx)
		}
	}
}

// chainMost is how many priced contracts one underlying may bring back.
//
// The broker's own ceiling is a thousand and it counts contracts, not symbols,
// so a name with many expirations inside the window can reach it. Asking for the
// ceiling costs nothing extra: the limit bounds the answer, not the request.
const chainMost = 1000

func (s *Sweep) once(ctx context.Context) {
	open, err := s.Broker.MarketOpen(ctx)
	if err != nil {
		s.Log.Error("could not ask whether the market is open", zap.Error(err))
		return
	}
	if !open {
		return
	}

	started := s.Now()
	found, refused := s.look(ctx)

	if s.Record != nil {
		if err := s.Record.RecordCandidates(ctx, started, found); err != nil {
			s.Log.Error("could not write down what the sweep found", zap.Error(err))
		}
		// Kept beside the write, because the table grows by a sweep every few
		// minutes and nothing else would ever shrink it.
		if gone, err := s.Record.PurgeCandidates(ctx, started.Add(-s.Keep)); err != nil {
			s.Log.Error("could not drop the sweeps that aged out", zap.Error(err))
		} else if gone > 0 {
			s.Log.Info("dropped the sweeps that aged out", zap.Int64("rows", gone))
		}
	}
	// What each filter threw away is logged beside what survived. A sweep that
	// returns one structure out of hundreds of names is either a quiet market or
	// a threshold set too tight, and only this tells the two apart.
	s.Log.Info("swept the universe",
		zap.Int("underlyings", len(s.Universe)),
		zap.Int("found", len(found)),
		zap.Any("refused", map[string]int(refused)),
		zap.Duration("took", s.Now().Sub(started)))
}

// look prices every underlying and returns what cleared the filters, richest
// first. A name the broker will not price is skipped in silence: most of the
// universe has no options anyone trades, and saying so every few minutes for
// each of them would bury what was found.
func (s *Sweep) look(ctx context.Context) ([]Candidate, Refused) {
	refused := Refused{}
	// Asked once for the pass: it is the same answer for every underlying in it,
	// and an unreadable one offers NOTHING rather than offering everything at a
	// threshold of zero. A screener that silently drops its bounds hands the
	// session the whole book as though it had qualified.
	wanted, err := s.Thresholds()
	if err != nil {
		s.Log.Error("could not read what a structure must clear; this pass offers nothing", zap.Error(err))
		return nil, refused
	}
	prices := map[string]float64{}
	for _, batch := range groups(s.Universe, pricesPerCall) {
		s.wait(ctx)
		got, err := s.Broker.LastTrades(ctx, batch)
		if err != nil {
			s.Log.Error("could not read prices", zap.Strings("underlyings", batch), zap.Error(err))
			continue
		}
		for symbol, price := range got {
			prices[symbol] = price
		}
	}

	workers := s.Workers
	if workers < 1 {
		workers = 1
	}

	// Each worker keeps its OWN tally of what it threw away and they are added up
	// at the end. Sharing one map would be a race, and the tally is the only
	// thing that tells a quiet market apart from a threshold set too tight.
	var (
		guard   sync.Mutex
		found   []Candidate
		tallies = make([]Refused, workers)
	)

	names := make(chan string)
	var crew sync.WaitGroup

	for worker := range workers {
		tallies[worker] = Refused{}
		crew.Add(1)

		go func(mine Refused) {
			defer crew.Done()

			for underlying := range names {
				price := prices[underlying]

				s.wait(ctx)
				// The window is centred on the price and reaches as far as the filters ask
				// on BOTH sides: a put sold below and a call sold above are both wanted.
				// Getting this wrong is silent - the first version reached 4.5 percent
				// below and 1.5 percent above, so calls past a percent and a half were
				// never priced, and the list came back almost entirely puts.
				//
				// ONE call brings the contracts and their prices together. It used to be
				// two, and two was the sweep's whole cost: the broker allows 180 requests
				// a minute, so what the pair bought in reach, the single call buys twice
				// over.
				reach := price * wanted.MaxOutOfTheMoney / 100
				contracts, quotes, err := s.Broker.Chain(ctx, underlying,
					price-reach, price+reach, s.Now().AddDate(0, 0, s.Expirations), chainMost)
				switch {
				case err != nil:
					mine.note(RefusedNoAnswer)
					continue
				case len(contracts) < 2:
					mine.note(RefusedTooFewContracts)
					continue
				}

				best := Best(underlying, price, contracts, quotes, s.Now(), wanted, mine)
				if len(best) == 0 {
					continue
				}

				guard.Lock()
				found = append(found, best...)
				guard.Unlock()
			}
		}(tallies[worker])
	}

	for _, underlying := range s.Universe {
		price, known := prices[underlying]
		if !known || price <= 0 {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		names <- underlying
	}

	close(names)
	crew.Wait()

	for _, mine := range tallies {
		for reason, count := range mine {
			refused[reason] += count
		}
	}

	// Richest first, as the name of this function promises. With one worker the
	// order fell out of the universe's own order; with more it does not fall out
	// of anything, so it is made.
	sort.Slice(found, func(i, j int) bool { return richer(found[i], found[j]) })

	return found, refused
}

// wait keeps the sweep inside the broker's limit. It counts what it spent in the
// current minute and sleeps out the rest of that minute when it runs out - the
// broker's refusal is worse than the delay, because a refusal loses the name
// entirely and a delay only postpones it.
func (s *Sweep) wait(ctx context.Context) {
	s.rate.Lock()
	defer s.rate.Unlock()

	now := s.Now()
	if s.since.IsZero() || now.Sub(s.since) >= time.Minute {
		s.since, s.spent = now, 0
	}
	if s.spent < s.PerMinute {
		s.spent++
		return
	}

	rest := time.Minute - now.Sub(s.since)
	select {
	case <-ctx.Done():
	case <-time.After(rest):
	}
	s.since, s.spent = s.Now(), 1
}

func groups(all []string, size int) [][]string {
	var out [][]string
	for i := 0; i < len(all); i += size {
		out = append(out, all[i:min(i+size, len(all))])
	}

	return out
}
