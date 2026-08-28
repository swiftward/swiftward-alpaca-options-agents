// Package placement answers where to put the legs of a structure whose payoff
// has a VALLEY.
//
// The screener already answers a different question, and the two must not be
// confused. It goes WIDE: 284 underlyings, some thirty thousand two-leg
// verticals, sixty-seven seconds, and it hands back the two dozen that pay best
// for what they must survive. Its measure, edge_points, works because a
// vertical's payoff is monotone - the further the strike, the safer, and
// intuition does not lie.
//
// A backspread is not monotone. Its worst case sits in the middle, at the bought
// strike, at expiration - the valley. Put that valley where the market usually
// arrives and the structure loses by construction, before the market does
// anything at all. Measured over 466 windows on 28 August 2026: EVERY placement
// with the valley inside two sigma has a negative expectation, and the one the
// agent chose by eye that day - sold at 0.57 sigma, valley at 1.25 - was worth
// about minus seventy dollars a trade the moment it was sent.
//
// So this package goes DEEP instead: one underlying, one expiration, every
// placement the caller's limits allow, each one priced from the book and then
// replayed against the underlying's own history. It returns numbers and ranks
// them. It chooses nothing, sends nothing, and knows nothing about what the
// declaration permits - the caller passes its limits in, and they are recorded
// with the call.
package placement

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// Market is what scoring needs and no more: the price now, the book for one
// expiration, and the underlying's own past.
type Market interface {
	LastTrades(ctx context.Context, symbols []string) (map[string]float64, error)
	ChainOn(ctx context.Context, underlying string, low, high float64, on time.Time, kind string, most int) ([]marketdata.Contract, map[string]marketdata.Quote, error)
	DailyCloses(ctx context.Context, symbol string, days int) ([]float64, error)
}

// Ask is one question about one underlying.
type Ask struct {
	Underlying string
	// Expiration is the day the structure expires. The chain is read up to it and
	// only contracts expiring exactly then are used: two legs of different
	// expirations are a different animal with a different worst case.
	Expiration time.Time
	// Kind is "call" or "put".
	Kind string
	// Bought is how many are bought against the one sold. Two is the backspread.
	Bought int
	// ShortLeastSigma and ValleyLeastSigma are the caller's limits, in sigmas of
	// the move expected by expiry. They are NOT defaults of this package: the
	// numbers live in the declaration, the agent reads them there and passes them
	// here, and the tool call records which were applied.
	ShortLeastSigma  float64
	ValleyLeastSigma float64
	// ShortMostSigma bounds the search on the far side. Without it the enumeration
	// walks to the end of the chain, where the quotes are a penny wide and mean
	// nothing.
	ShortMostSigma float64
	// WorstCaseMost is the most this may lose, in dollars, at the valley. It sets
	// how many sets fit; a placement that cannot fit one is not returned.
	WorstCaseMost float64
	// Most is how many placements come back. The whole permitted space is several
	// hundred, and several hundred rows is not an answer.
	Most int
}

// Placement is one arrangement of the legs and what history says it is worth.
type Placement struct {
	ShortStrike float64 `json:"short_strike"`
	LongStrike  float64 `json:"long_strike"`
	ShortSymbol string  `json:"short_symbol"`
	LongSymbol  string  `json:"long_symbol"`
	// Credit is per set, at the executable sides of the book: what is paid for the
	// bought legs comes off the ask, what the sold leg brings comes off the bid.
	// Negative means the structure costs money to open.
	Credit float64 `json:"credit"`
	// Sets is how many fit under WorstCaseMost.
	Sets int `json:"sets"`
	// WorstCase is what the whole position loses at the valley, in dollars.
	WorstCase float64 `json:"worst_case"`
	// ShortSigma and ValleySigma say where the legs ended up, in sigmas. The
	// valley is the number that decides: it is where the worst case sits.
	ShortSigma  float64 `json:"short_sigma"`
	ValleySigma float64 `json:"valley_sigma"`
	// Expected, Median and Worst are dollars over the replayed windows.
	Expected float64 `json:"expected"`
	Median   float64 `json:"median"`
	Worst    float64 `json:"worst"`
	// LosingShare is the share of windows that end in the red, as a percent.
	LosingShare float64 `json:"losing_share_percent"`
	// TouchedShare is the share of windows in which the price reached the sold
	// strike at all - that is, in which this structure did ANYTHING beyond keeping
	// its credit.
	//
	// It is here because without it the ranking lies by omission. Past about two
	// and a half sigmas the history holds almost nothing, so every placement out
	// there scores the same - expectation, median and worst all equal to the
	// credit - and the order among them falls through to whichever fits the most
	// sets, which is the narrowest width and has nothing to do with being better.
	// A zero here says plainly: history has no opinion about this row, only about
	// the pennies it collects.
	TouchedShare float64 `json:"touched_share_percent"`
	// FromTopPercent is how much of Expected comes from the best one percent of
	// windows. This is the number no one finds by eye, and it is the difference
	// between a cheap bet and a lottery ticket: the structure sent on 28 August
	// drew two thirds of its expectation from one percent of history.
	//
	// It can exceed a hundred, and that is not a fault: where everything outside
	// the tail loses money, the tail carries more than the whole of what is left.
	// A row reading 255 is a structure that loses almost always and is redeemed
	// entirely by the rarest windows in the sample.
	//
	// Absent where the placement is not in the black: a share OF a negative
	// expectation is not a share of anything, and the reader has already been told
	// what matters about such a row by Expected itself.
	//
	// A pointer rather than a NaN, and that is not a matter of taste. NaN cannot
	// be written as JSON, so ONE such row failed the WHOLE answer with "json:
	// unsupported value: NaN" - and in a quiet market most placements are in the
	// red, which is to say the tool broke precisely when it was worth asking.
	FromTopPercent *float64 `json:"from_top_percent,omitempty"`
}

// Answer is what the caller gets back.
type Answer struct {
	Underlying  string  `json:"underlying"`
	Expiration  string  `json:"expiration"`
	Price       float64 `json:"underlying_price"`
	TradingDays int     `json:"trading_days_to_expiry"`
	// Volatility is the underlying's own realised volatility, annualised, and
	// Sigma is the move it implies by expiry, in points. Every distance below is
	// measured in it.
	Volatility float64 `json:"volatility_annual"`
	Sigma      float64 `json:"sigma_points"`
	// Windows is how many pieces of history the replay stood on, and Regime says
	// what made them comparable. A number without them is a number without a
	// sample.
	Windows int `json:"windows"`
	// Independent is roughly how many of those windows do not overlap. Windows
	// step one day at a time, so at five days to expiry each shares four fifths
	// of its path with the next: four hundred windows are not four hundred
	// observations, and the best one percent of them is likely ONE episode
	// counted five times. The honest figure to read a tail against is this one.
	Independent int    `json:"independent_windows"`
	Regime      string `json:"regime"`
	// Considered is the whole permitted space; Placements is the best of it.
	Considered int         `json:"placements_considered"`
	Placements []Placement `json:"placements"`
}

// Scorer holds what the tool needs between calls. Nothing about the declaration
// lives here.
type Scorer struct {
	Market Market
	// History is how many calendar days of closes to ask for.
	//
	// Three years rather than two. The regime filter throws away most of what it
	// is given - two years came back as 216 usable windows against the 466 the
	// same measurement stood on in research - and the tail is exactly where a
	// thin sample stops being able to tell one placement from another.
	History int
	// Now is the clock, injectable so a test is not at the mercy of the date.
	Now func() time.Time
	// Where is the exchange's own timezone, in which a trading day begins and
	// ends. Nil means UTC, which is right for a test and wrong for this team.
	Where *time.Location
}

// vol is the window realised volatility is measured over, in trading days. It
// is the usual twenty: short enough to say what the market is doing now, long
// enough not to jump on one loud day.
const volWindow = 20

// near is how far a past window's volatility may sit from today's and still
// count as the same weather: a quarter either way. Wider and a calm week is
// judged by a panic; narrower and there is no sample left.
const near = 0.25

// Score enumerates every placement the ask permits, prices each from the book,
// replays it against the underlying's own history and returns the best.
func (s Scorer) Score(ctx context.Context, ask Ask) (Answer, error) {
	if ask.Underlying == "" {
		return Answer{}, errors.New("no underlying to score")
	}
	if ask.Kind != "call" && ask.Kind != "put" {
		return Answer{}, fmt.Errorf("kind is %q: it is call or put", ask.Kind)
	}
	if ask.Bought < 2 {
		return Answer{}, errors.New("bought must be two or more: one for one is a vertical, and the screener already prices those")
	}
	if ask.WorstCaseMost <= 0 {
		return Answer{}, errors.New("no worst case ceiling given: without one there is nothing to size against")
	}

	prices, err := s.Market.LastTrades(ctx, []string{ask.Underlying})
	if err != nil {
		return Answer{}, fmt.Errorf("read the price: %w", err)
	}
	spot := prices[ask.Underlying]
	if spot <= 0 {
		return Answer{}, fmt.Errorf("the broker gave no price for %s", ask.Underlying)
	}

	closes, err := s.Market.DailyCloses(ctx, ask.Underlying, s.History)
	if err != nil {
		return Answer{}, fmt.Errorf("read the history: %w", err)
	}
	days := tradingDaysUntil(s.Now(), ask.Expiration, s.Where)
	if days < 1 {
		return Answer{}, errors.New("the expiration is today or past: there is no window left to replay")
	}

	moves, vol, err := windows(closes, days)
	if err != nil {
		return Answer{}, err
	}

	sigma := vol * math.Sqrt(float64(days)/252.0) * spot
	if sigma <= 0 {
		return Answer{}, errors.New("the history gives no volatility to measure distance in")
	}

	low, high := s.band(ask, spot, sigma)
	contracts, quotes, err := s.Market.ChainOn(ctx, ask.Underlying, low, high, ask.Expiration, ask.Kind, 500)
	if err != nil {
		return Answer{}, fmt.Errorf("read the chain: %w", err)
	}

	legs := usable(contracts, quotes, ask)
	found, considered := s.enumerate(ask, legs, spot, sigma, moves)

	sort.Slice(found, func(i, j int) bool { return found[i].Expected > found[j].Expected })
	if ask.Most > 0 && len(found) > ask.Most {
		found = found[:ask.Most]
	}

	return Answer{
		Underlying:  ask.Underlying,
		Expiration:  ask.Expiration.Format(time.DateOnly),
		Price:       spot,
		TradingDays: days,
		Volatility:  vol,
		Sigma:       sigma,
		Windows:     len(moves),
		Independent: len(moves) / days,
		Regime: fmt.Sprintf("windows whose own volatility sat within %.0f%% of today's %.1f%% annual",
			near*100, vol*100),
		Considered: considered,
		Placements: found,
	}, nil
}
