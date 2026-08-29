// Package volatility keeps a history of what the market charged for options, so
// a session can ask whether today is expensive or cheap by its own measure.
//
// Two of the three entry rules read implied volatility relative to its own past.
// The broker sells no such history: it answers what a contract is worth now.
// So the history has to be ours, and it has to start before the week it is meant
// to judge - a series begun late answers nothing.
package volatility

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// Sample is one reading: what the option closest to the money cost at a moment.
type Sample struct {
	Underlying        string
	Contract          string
	RecordedAt        time.Time
	Expiration        time.Time
	Strike            float64
	OptionType        string
	ImpliedVolatility float64
	Delta             *float64
	Bid               float64
	Ask               float64
	UnderlyingPrice   float64
}

// Summary answers the only question the strategy asks of the history: where does
// the latest reading sit inside it.
type Summary struct {
	Underlying string    `json:"underlying"`
	Since      time.Time `json:"since"`
	Samples    int       `json:"samples"`
	Latest     float64   `json:"latest"`
	LatestAt   time.Time `json:"latest_at"`
	Lowest     float64   `json:"lowest"`
	Median     float64   `json:"median"`
	Highest    float64   `json:"highest"`
	// Rank is the share of readings the latest one sits above, from 0 to 100. It
	// is a place in the series rather than a place between its ends, so a single
	// outlier moves it by one reading rather than by half the scale.
	Rank float64 `json:"rank"`
}

// Market is what the recorder needs from the broker. The recorder never orders
// anything, so this is deliberately the reading half only.
type Market interface {
	MarketOpen(ctx context.Context) (bool, error)
	LastTrades(ctx context.Context, symbols []string) (map[string]float64, error)
	ContractsAround(ctx context.Context, underlying string, price, span float64, from time.Time, limit int) ([]marketdata.Contract, error)
	Quotes(ctx context.Context, symbols []string) (map[string]marketdata.Quote, error)
}

// Store keeps the samples.
type Store interface {
	Append(ctx context.Context, sample Sample) error
	Summarise(ctx context.Context, underlying string, since time.Time) (Summary, error)
}

// strikeSpan is how far from the price the recorder looks for a contract, in the
// underlying's own currency. Wide enough to find a listed strike on any name,
// narrow enough that the broker answers with a handful of contracts.
const strikeSpan = 3.0

// horizon is how far out the expiration must be for the series to mean anything.
// The option this project trades expires the same day, and its implied
// volatility swings with the hour rather than with the market - a series built
// from it measures the clock. A contract three weeks out moves with the market.
const horizon = 21 * 24 * time.Hour

// contractsRead bounds one listing. Two option types times a few strikes times
// the nearest expirations fit inside it; more would only be paged.
const contractsRead = 40

// Recorder writes one reading per underlying, every Every, while the market is
// open.
type Recorder struct {
	Market      Market
	Store       Store
	Underlyings []string
	Every       time.Duration
	Now         func() time.Time
	Log         *zap.Logger
}

// Run records until ctx ends. It refuses to start misconfigured rather than
// running as a loop that quietly writes nothing.
func (r *Recorder) Run(ctx context.Context) error {
	switch {
	case r.Market == nil || r.Store == nil:
		return fmt.Errorf("the volatility recorder needs a market to read and somewhere to write")
	case len(r.Underlyings) == 0:
		return fmt.Errorf("the volatility recorder was given nothing to watch: set VOLATILITY_UNDERLYINGS")
	case r.Every <= 0:
		return fmt.Errorf("the volatility recorder needs how often to read: set VOLATILITY_EVERY")
	case r.Now == nil:
		return fmt.Errorf("the volatility recorder has no clock")
	}

	ticker := time.NewTicker(r.Every)
	defer ticker.Stop()

	r.readOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.readOnce(ctx)
		}
	}
}

func (r *Recorder) readOnce(ctx context.Context) {
	open, err := r.Market.MarketOpen(ctx)
	if err != nil {
		r.Log.Error("could not read the market clock", zap.Error(err))
		return
	}
	if !open {
		return
	}

	prices, err := r.Market.LastTrades(ctx, r.Underlyings)
	if err != nil {
		r.Log.Error("could not read the underlying prices", zap.Error(err))
		return
	}

	for _, underlying := range r.Underlyings {
		price, known := prices[underlying]
		if !known {
			r.Log.Info("no price for the underlying, nothing to record", zap.String("underlying", underlying))
			continue
		}
		if err := r.record(ctx, underlying, price); err != nil {
			r.Log.Error("could not record the volatility",
				zap.String("underlying", underlying), zap.Error(err))
		}
	}
}

func (r *Recorder) record(ctx context.Context, underlying string, price float64) error {
	now := r.Now()
	listed, err := r.Market.ContractsAround(ctx, underlying, price, strikeSpan, now.Add(horizon), contractsRead)
	if err != nil {
		return err
	}

	chosen := atTheMoney(listed, price)
	if len(chosen) == 0 {
		return fmt.Errorf("the broker listed no contract within %.0f of %.2f", strikeSpan, price)
	}

	symbols := make([]string, 0, len(chosen))
	for _, contract := range chosen {
		symbols = append(symbols, contract.Symbol)
	}
	quotes, err := r.Market.Quotes(ctx, symbols)
	if err != nil {
		return err
	}

	for _, contract := range chosen {
		quote, answered := quotes[contract.Symbol]
		if !answered || quote.ImpliedVolatility == nil {
			// Absent while a quote is one-sided. Recording a zero here would put a
			// number in the history that the market never charged.
			continue
		}
		sample := Sample{
			Underlying:        underlying,
			Contract:          contract.Symbol,
			RecordedAt:        now,
			Expiration:        contract.Expiration,
			Strike:            contract.Strike,
			OptionType:        contract.Type,
			ImpliedVolatility: *quote.ImpliedVolatility,
			Delta:             quote.Delta,
			Bid:               quote.Bid,
			Ask:               quote.Ask,
			UnderlyingPrice:   price,
		}
		if err := r.Store.Append(ctx, sample); err != nil {
			return err
		}
	}

	return nil
}

// atTheMoney picks one contract: the put closest to the price, from the nearest
// expiration the broker listed. One reading per underlying per run, and always
// the same kind of contract - a series that mixes calls with puts moves when the
// skew moves and says nothing about the level.
func atTheMoney(listed []marketdata.Contract, price float64) []marketdata.Contract {
	var chosen *marketdata.Contract
	for i := range listed {
		contract := listed[i]
		if contract.Type != "put" {
			continue
		}
		switch {
		case chosen == nil:
			chosen = &contract
		case contract.Expiration.Before(chosen.Expiration):
			chosen = &contract
		case contract.Expiration.Equal(chosen.Expiration) &&
			math.Abs(contract.Strike-price) < math.Abs(chosen.Strike-price):
			chosen = &contract
		}
	}
	if chosen == nil {
		return nil
	}

	return []marketdata.Contract{*chosen}
}

// Summarise turns a series into the numbers the entry rules speak in. It lives
// here rather than in the store so that both implementations answer the same
// way.
func Summarise(underlying string, since time.Time, readings []Reading) Summary {
	summary := Summary{Underlying: underlying, Since: since, Samples: len(readings)}
	if len(readings) == 0 {
		return summary
	}

	values := make([]float64, 0, len(readings))
	latest := readings[0]
	for _, reading := range readings {
		values = append(values, reading.ImpliedVolatility)
		if reading.At.After(latest.At) {
			latest = reading
		}
	}
	sort.Float64s(values)

	summary.Latest = latest.ImpliedVolatility
	summary.LatestAt = latest.At
	summary.Lowest = values[0]
	summary.Highest = values[len(values)-1]
	summary.Median = median(values)
	summary.Rank = rank(summary.Latest, values)

	return summary
}

// Reading is one implied volatility with its time: what Summarise works from.
type Reading struct {
	At                time.Time
	ImpliedVolatility float64
}

func median(sorted []float64) float64 {
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}

	return (sorted[middle-1] + sorted[middle]) / 2
}

// rank counts how much of the series the latest reading stands above. A series
// that never moved ranks in the middle: no reading in it is high or low, and
// answering 0 or 100 would read as a signal the market never gave.
func rank(latest float64, sorted []float64) float64 {
	below, equal := 0, 0
	for _, value := range sorted {
		switch {
		case value < latest:
			below++
		case value == latest:
			equal++
		}
	}
	if equal == len(sorted) {
		return 50
	}

	// The reading itself is one of the equals; counting half of them places a
	// repeated value in the middle of its own run rather than at either end.
	return (float64(below) + float64(equal)/2) / float64(len(sorted)) * 100
}
